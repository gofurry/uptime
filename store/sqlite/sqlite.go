package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofurry/uptime"
	_ "modernc.org/sqlite"
)

type Config struct {
	Path string
}

type Store struct {
	config Config
	db     *sql.DB
}

func New(config Config) *Store {
	return &Store{config: config}
}

func (s *Store) Name() string {
	return "sqlite"
}

func (s *Store) Init(ctx context.Context) error {
	if s.config.Path == "" {
		return errors.New("sqlite uptime store: path is required")
	}
	if err := ensureDir(s.config.Path); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", s.config.Path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)

	s.db = db
	for _, stmt := range pragmaStatements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			_ = s.db.Close()
			s.db = nil
			return fmt.Errorf("sqlite uptime store: %s: %w", stmt, err)
		}
	}
	for _, stmt := range schemaStatements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			_ = s.db.Close()
			s.db = nil
			return fmt.Errorf("sqlite uptime store: init schema: %w", err)
		}
	}
	return nil
}

func (s *Store) UpsertService(ctx context.Context, service uptime.Service) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO services (service_id, name, description, created_at, last_seen_at, sample_interval_nanos)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(service_id) DO UPDATE SET
	name = excluded.name,
	description = excluded.description,
	last_seen_at = max(services.last_seen_at, excluded.last_seen_at),
	sample_interval_nanos = excluded.sample_interval_nanos
`, service.ID, service.Name, service.Description, unixNano(service.CreatedAt), unixNano(service.LastSeenAt), int64(service.SampleInterval))
	return err
}

func (s *Store) UpsertInstance(ctx context.Context, instance uptime.Instance) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO instances (instance_id, service_id, hostname, pid, started_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(instance_id) DO UPDATE SET
	service_id = excluded.service_id,
	hostname = excluded.hostname,
	pid = excluded.pid,
	last_seen_at = max(instances.last_seen_at, excluded.last_seen_at)
`, instance.ID, instance.ServiceID, instance.Hostname, instance.PID, unixNano(instance.StartedAt), unixNano(instance.LastSeenAt))
	return err
}

func (s *Store) WriteHeartbeat(ctx context.Context, heartbeat uptime.Heartbeat) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	seenAt := unixNano(heartbeat.SeenAt)
	if _, err := tx.ExecContext(ctx, `
UPDATE services
SET last_seen_at = max(last_seen_at, ?)
WHERE service_id = ?
`, seenAt, heartbeat.ServiceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE instances
SET last_seen_at = max(last_seen_at, ?)
WHERE instance_id = ?
`, seenAt, heartbeat.InstanceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO uptime_samples (service_id, instance_id, day, slot, seen_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(service_id, instance_id, day, slot) DO UPDATE SET
	seen_at = max(uptime_samples.seen_at, excluded.seen_at)
`, heartbeat.ServiceID, heartbeat.InstanceID, heartbeat.Day, heartbeat.Slot, seenAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RollupDaily(ctx context.Context, options uptime.RollupOptions) error {
	if options.BeforeDay == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	rows, err := tx.QueryContext(ctx, `
SELECT service_id, day, COUNT(DISTINCT slot) AS up_slots
FROM uptime_samples
WHERE day < ?
GROUP BY service_id, day
`, options.BeforeDay)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rollupRow struct {
		serviceID string
		day       string
		upSlots   int
	}
	var rowsToWrite []rollupRow
	for rows.Next() {
		var row rollupRow
		if err := rows.Scan(&row.serviceID, &row.day, &row.upSlots); err != nil {
			return err
		}
		rowsToWrite = append(rowsToWrite, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO uptime_daily (service_id, day, up_slots, expected_slots, uptime_rate, finalized)
VALUES (?, ?, ?, ?, ?, 1)
ON CONFLICT(service_id, day) DO UPDATE SET
	up_slots = excluded.up_slots,
	expected_slots = excluded.expected_slots,
	uptime_rate = excluded.uptime_rate,
	finalized = excluded.finalized
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, row := range rowsToWrite {
		expected := 0
		if options.ExpectedSlotsForServiceDay != nil {
			expected = options.ExpectedSlotsForServiceDay(row.serviceID, row.day)
		} else if options.ExpectedSlotsForDay != nil {
			expected = options.ExpectedSlotsForDay(row.day)
		}
		rate := rate(row.upSlots, expected)
		if _, err := stmt.ExecContext(ctx, row.serviceID, row.day, row.upSlots, expected, rate); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) Cleanup(ctx context.Context, options uptime.CleanupOptions) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	if options.DailyBeforeDay != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM uptime_daily WHERE day < ?`, options.DailyBeforeDay); err != nil {
			return err
		}
	}
	if options.SamplesBeforeDay != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM uptime_samples WHERE day < ?`, options.SamplesBeforeDay); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListServices(ctx context.Context) ([]uptime.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT service_id, name, description, created_at, last_seen_at, sample_interval_nanos
FROM services
ORDER BY service_id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []uptime.Service
	for rows.Next() {
		var service uptime.Service
		var createdAt, lastSeenAt, intervalNanos int64
		if err := rows.Scan(&service.ID, &service.Name, &service.Description, &createdAt, &lastSeenAt, &intervalNanos); err != nil {
			return nil, err
		}
		service.CreatedAt = fromUnixNano(createdAt)
		service.LastSeenAt = fromUnixNano(lastSeenAt)
		service.SampleInterval = time.Duration(intervalNanos)
		services = append(services, service)
	}
	return services, rows.Err()
}

func (s *Store) QueryDaily(ctx context.Context, options uptime.QueryDailyOptions) ([]uptime.DailyStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT service_id, day, up_slots, expected_slots, uptime_rate, finalized
FROM uptime_daily
WHERE day >= ? AND day <= ?
ORDER BY service_id, day
`, options.FromDay, options.ToDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []uptime.DailyStatus
	for rows.Next() {
		var status uptime.DailyStatus
		var finalized int
		if err := rows.Scan(&status.ServiceID, &status.Day, &status.UpSlots, &status.ExpectedSlots, &status.UptimeRate, &finalized); err != nil {
			return nil, err
		}
		status.Finalized = finalized != 0
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}

func (s *Store) QueryTodaySamples(ctx context.Context, options uptime.QueryTodaySamplesOptions) ([]uptime.TodaySampleStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT service_id, day, COUNT(DISTINCT slot) AS up_slots
FROM uptime_samples
WHERE day = ?
GROUP BY service_id, day
ORDER BY service_id
`, options.Day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []uptime.TodaySampleStatus
	for rows.Next() {
		var status uptime.TodaySampleStatus
		if err := rows.Scan(&status.ServiceID, &status.Day, &status.UpSlots); err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}

func (s *Store) ClaimAlertEvent(ctx context.Context, state uptime.AlertState) (uptime.AlertDecision, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return uptime.AlertDecision{}, err
	}
	defer rollback(tx)

	var previous string
	err = tx.QueryRowContext(ctx, `
SELECT status
FROM uptime_alert_state
WHERE service_id = ?
`, state.ServiceID).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO uptime_alert_state (service_id, status, last_seen_at, updated_at)
VALUES (?, ?, ?, ?)
`, state.ServiceID, state.Status, unixNano(state.LastSeenAt), unixNano(state.CheckedAt)); err != nil {
			return uptime.AlertDecision{}, err
		}
		if err := tx.Commit(); err != nil {
			return uptime.AlertDecision{}, err
		}
		return uptime.AlertDecision{
			Notify: state.NotifyOnFirstDown && state.Status == uptime.AlertStatusDown,
		}, nil
	}
	if err != nil {
		return uptime.AlertDecision{}, err
	}

	if previous == state.Status {
		if _, err := tx.ExecContext(ctx, `
UPDATE uptime_alert_state
SET last_seen_at = ?, updated_at = ?
WHERE service_id = ?
`, unixNano(state.LastSeenAt), unixNano(state.CheckedAt), state.ServiceID); err != nil {
			return uptime.AlertDecision{}, err
		}
		return uptime.AlertDecision{}, tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE uptime_alert_state
SET status = ?, last_seen_at = ?, updated_at = ?
WHERE service_id = ?
`, state.Status, unixNano(state.LastSeenAt), unixNano(state.CheckedAt), state.ServiceID); err != nil {
		return uptime.AlertDecision{}, err
	}
	if err := tx.Commit(); err != nil {
		return uptime.AlertDecision{}, err
	}
	return uptime.AlertDecision{
		Notify:         true,
		PreviousStatus: previous,
	}, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func ensureDir(path string) error {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func unixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func fromUnixNano(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v).UTC()
}

func rate(upSlots, expectedSlots int) float64 {
	if expectedSlots <= 0 {
		return 0
	}
	value := float64(upSlots) / float64(expectedSlots)
	if value > 1 {
		return 1
	}
	if value < 0 || math.IsNaN(value) {
		return 0
	}
	return value
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
