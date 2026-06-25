package postgres

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofurry/uptime"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	defaultHost        = "localhost"
	defaultPort        = 5432
	defaultDatabase    = "postgres"
	defaultSSLMode     = "disable"
	defaultSchema      = "public"
	defaultTablePrefix = "uptime_"
)

type Config struct {
	DSN string

	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string

	Schema      string
	TablePrefix string
	Tables      TableNames

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type TableNames struct {
	Services  string
	Instances string
	Samples   string
	Daily     string
}

type Store struct {
	config Config
	db     *sql.DB
	tables resolvedTables
}

type resolvedTables struct {
	schema string

	services  string
	instances string
	samples   string
	daily     string

	qServices  string
	qInstances string
	qSamples   string
	qDaily     string
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func New(config Config) *Store {
	return &Store{config: config}
}

func (s *Store) Name() string {
	return "postgres"
}

func (s *Store) Init(ctx context.Context) error {
	tables, err := resolveTables(s.config)
	if err != nil {
		return err
	}
	dsn, err := s.config.dsn()
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	if s.config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(s.config.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(5)
	}
	if s.config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(s.config.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(2)
	}
	if s.config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(s.config.ConnMaxLifetime)
	}

	s.db = db
	s.tables = tables

	if err := s.db.PingContext(ctx); err != nil {
		_ = s.db.Close()
		s.db = nil
		return fmt.Errorf("postgres uptime store: ping: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quoteIdent(tables.schema)); err != nil {
		_ = s.db.Close()
		s.db = nil
		return fmt.Errorf("postgres uptime store: create schema: %w", err)
	}
	for _, stmt := range tables.schemaStatements() {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			_ = s.db.Close()
			s.db = nil
			return fmt.Errorf("postgres uptime store: init schema: %w", err)
		}
	}
	return nil
}

func (s *Store) UpsertService(ctx context.Context, service uptime.Service) error {
	stmt := fmt.Sprintf(`
INSERT INTO %s (service_id, name, description, created_at, last_seen_at, sample_interval_nanos)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT(service_id) DO UPDATE SET
	name = EXCLUDED.name,
	description = EXCLUDED.description,
	last_seen_at = GREATEST(%s.last_seen_at, EXCLUDED.last_seen_at),
	sample_interval_nanos = EXCLUDED.sample_interval_nanos
`, s.tables.qServices, quoteIdent(s.tables.services))
	_, err := s.db.ExecContext(ctx, stmt, service.ID, service.Name, service.Description, unixNano(service.CreatedAt), unixNano(service.LastSeenAt), int64(service.SampleInterval))
	return err
}

func (s *Store) UpsertInstance(ctx context.Context, instance uptime.Instance) error {
	stmt := fmt.Sprintf(`
INSERT INTO %s (instance_id, service_id, hostname, pid, started_at, last_seen_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT(instance_id) DO UPDATE SET
	service_id = EXCLUDED.service_id,
	hostname = EXCLUDED.hostname,
	pid = EXCLUDED.pid,
	last_seen_at = GREATEST(%s.last_seen_at, EXCLUDED.last_seen_at)
`, s.tables.qInstances, quoteIdent(s.tables.instances))
	_, err := s.db.ExecContext(ctx, stmt, instance.ID, instance.ServiceID, instance.Hostname, instance.PID, unixNano(instance.StartedAt), unixNano(instance.LastSeenAt))
	return err
}

func (s *Store) WriteHeartbeat(ctx context.Context, heartbeat uptime.Heartbeat) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	seenAt := unixNano(heartbeat.SeenAt)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s
SET last_seen_at = GREATEST(last_seen_at, $1)
WHERE service_id = $2
`, s.tables.qServices), seenAt, heartbeat.ServiceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s
SET last_seen_at = GREATEST(last_seen_at, $1)
WHERE instance_id = $2
`, s.tables.qInstances), seenAt, heartbeat.InstanceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (service_id, instance_id, day, slot, seen_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT(service_id, instance_id, day, slot) DO UPDATE SET
	seen_at = GREATEST(%s.seen_at, EXCLUDED.seen_at)
`, s.tables.qSamples, quoteIdent(s.tables.samples)), heartbeat.ServiceID, heartbeat.InstanceID, heartbeat.Day, heartbeat.Slot, seenAt); err != nil {
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

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
SELECT service_id, day, COUNT(DISTINCT slot) AS up_slots
FROM %s
WHERE day < $1
GROUP BY service_id, day
`, s.tables.qSamples), options.BeforeDay)
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

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`
INSERT INTO %s (service_id, day, up_slots, expected_slots, uptime_rate, finalized)
VALUES ($1, $2, $3, $4, $5, TRUE)
ON CONFLICT(service_id, day) DO UPDATE SET
	up_slots = EXCLUDED.up_slots,
	expected_slots = EXCLUDED.expected_slots,
	uptime_rate = EXCLUDED.uptime_rate,
	finalized = EXCLUDED.finalized
`, s.tables.qDaily))
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
		if _, err := stmt.ExecContext(ctx, row.serviceID, row.day, row.upSlots, expected, rate(row.upSlots, expected)); err != nil {
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
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+s.tables.qDaily+` WHERE day < $1`, options.DailyBeforeDay); err != nil {
			return err
		}
	}
	if options.SamplesBeforeDay != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+s.tables.qSamples+` WHERE day < $1`, options.SamplesBeforeDay); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListServices(ctx context.Context) ([]uptime.Service, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT service_id, name, description, created_at, last_seen_at, sample_interval_nanos
FROM %s
ORDER BY service_id
`, s.tables.qServices))
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
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT service_id, day, up_slots, expected_slots, uptime_rate, finalized
FROM %s
WHERE day >= $1 AND day <= $2
ORDER BY service_id, day
`, s.tables.qDaily), options.FromDay, options.ToDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []uptime.DailyStatus
	for rows.Next() {
		var status uptime.DailyStatus
		if err := rows.Scan(&status.ServiceID, &status.Day, &status.UpSlots, &status.ExpectedSlots, &status.UptimeRate, &status.Finalized); err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}

func (s *Store) QueryTodaySamples(ctx context.Context, options uptime.QueryTodaySamplesOptions) ([]uptime.TodaySampleStatus, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT service_id, day, COUNT(DISTINCT slot) AS up_slots
FROM %s
WHERE day = $1
GROUP BY service_id, day
ORDER BY service_id
`, s.tables.qSamples), options.Day)
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

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (c Config) dsn() (string, error) {
	if c.DSN != "" {
		return c.DSN, nil
	}
	if c.Username == "" {
		return "", errors.New("postgres uptime store: username is required when DSN is empty")
	}
	host := c.Host
	if host == "" {
		host = defaultHost
	}
	port := c.Port
	if port == 0 {
		port = defaultPort
	}
	database := c.Database
	if database == "" {
		database = defaultDatabase
	}
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = defaultSSLMode
	}

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.Username, c.Password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + database,
	}
	q := u.Query()
	q.Set("sslmode", sslMode)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func resolveTables(config Config) (resolvedTables, error) {
	schema := config.Schema
	if schema == "" {
		schema = defaultSchema
	}
	if err := validateIdent("schema", schema); err != nil {
		return resolvedTables{}, err
	}

	prefix := config.TablePrefix
	if prefix == "" {
		prefix = defaultTablePrefix
	}
	names := TableNames{
		Services:  prefix + "services",
		Instances: prefix + "instances",
		Samples:   prefix + "samples",
		Daily:     prefix + "daily",
	}
	if config.Tables.Services != "" {
		names.Services = config.Tables.Services
	}
	if config.Tables.Instances != "" {
		names.Instances = config.Tables.Instances
	}
	if config.Tables.Samples != "" {
		names.Samples = config.Tables.Samples
	}
	if config.Tables.Daily != "" {
		names.Daily = config.Tables.Daily
	}

	for label, name := range map[string]string{
		"services table":  names.Services,
		"instances table": names.Instances,
		"samples table":   names.Samples,
		"daily table":     names.Daily,
	} {
		if err := validateIdent(label, name); err != nil {
			return resolvedTables{}, err
		}
	}

	return resolvedTables{
		schema:     schema,
		services:   names.Services,
		instances:  names.Instances,
		samples:    names.Samples,
		daily:      names.Daily,
		qServices:  quoteTable(schema, names.Services),
		qInstances: quoteTable(schema, names.Instances),
		qSamples:   quoteTable(schema, names.Samples),
		qDaily:     quoteTable(schema, names.Daily),
	}, nil
}

func (t resolvedTables) schemaStatements() []string {
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			service_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL,
			last_seen_at BIGINT NOT NULL,
			sample_interval_nanos BIGINT NOT NULL
		)`, t.qServices),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			instance_id BIGINT PRIMARY KEY,
			service_id TEXT NOT NULL,
			hostname TEXT NOT NULL DEFAULT '',
			pid INTEGER NOT NULL DEFAULT 0,
			started_at BIGINT NOT NULL,
			last_seen_at BIGINT NOT NULL
		)`, t.qInstances),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			service_id TEXT NOT NULL,
			instance_id BIGINT NOT NULL,
			day TEXT NOT NULL,
			slot BIGINT NOT NULL,
			seen_at BIGINT NOT NULL,
			PRIMARY KEY (service_id, instance_id, day, slot)
		)`, t.qSamples),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			service_id TEXT NOT NULL,
			day TEXT NOT NULL,
			up_slots INTEGER NOT NULL,
			expected_slots INTEGER NOT NULL,
			uptime_rate DOUBLE PRECISION NOT NULL,
			finalized BOOLEAN NOT NULL,
			PRIMARY KEY (service_id, day)
		)`, t.qDaily),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(service_id)`, quoteIdent(indexName(t.instances, "service")), t.qInstances),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(service_id, day)`, quoteIdent(indexName(t.samples, "service_day")), t.qSamples),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(day)`, quoteIdent(indexName(t.samples, "day")), t.qSamples),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(service_id, day)`, quoteIdent(indexName(t.daily, "service_day")), t.qDaily),
	}
}

func validateIdent(label, value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("postgres uptime store: invalid %s identifier %q", label, value)
	}
	return nil
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteTable(schema, table string) string {
	return quoteIdent(schema) + "." + quoteIdent(table)
}

func indexName(table, suffix string) string {
	base := "idx_" + table + "_" + suffix
	if len(base) <= 60 {
		return base
	}
	sum := sha1.Sum([]byte(base))
	return base[:45] + "_" + hex.EncodeToString(sum[:])[:12]
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
