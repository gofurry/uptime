package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/gofurry/uptime/internal/timeutil"
	"github.com/gofurry/uptime/store/sqlite"
	_ "modernc.org/sqlite"
)

type demoService struct {
	id          string
	name        string
	description string
	instances   []demoInstance
	profile     []float64
}

type demoInstance struct {
	id       int64
	hostname string
	pid      int
}

func main() {
	var (
		path     = flag.String("path", "uptime.db", "SQLite database path")
		days     = flag.Int("days", 90, "number of demo days")
		reset    = flag.Bool("reset", true, "reset uptime tables before seeding")
		interval = flag.Duration("interval", 3*time.Second, "sample interval used by demo data")
	)
	flag.Parse()

	if *days < 3 {
		log.Fatal("days must be at least 3")
	}

	ctx := context.Background()
	store := sqlite.New(sqlite.Config{Path: *path})
	if err := store.Init(ctx); err != nil {
		log.Fatal(err)
	}
	if err := store.Close(); err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("sqlite", *path)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if *reset {
		for _, table := range []string{"uptime_samples", "uptime_daily", "instances", "services"} {
			if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				log.Fatal(err)
			}
		}
	}

	loc := time.Local
	now := time.Now()
	today := timeutil.DayOf(now, loc)
	services := demoServices()

	if err := seedServices(ctx, db, services, now, *interval, *days); err != nil {
		log.Fatal(err)
	}
	if err := seedDaily(ctx, db, services, today, *days, *interval, loc); err != nil {
		log.Fatal(err)
	}
	if err := seedTodaySamples(ctx, db, services, today, now, *interval, loc); err != nil {
		log.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("seeded %s with %d services across %d days\n", *path, len(services), *days)
}

func demoServices() []demoService {
	return []demoService{
		{
			id:          "demo-api",
			name:        "Demo API",
			description: "Public HTTP API",
			instances: []demoInstance{
				{id: 1001, hostname: "server-a", pid: 4101},
				{id: 1002, hostname: "server-b", pid: 4201},
			},
			profile: []float64{.9995, .9992, .9978, .9820, .9610, .9998, .9940, .9050, .9990},
		},
		{
			id:          "gofurry-worker",
			name:        "GoFurry Worker",
			description: "Background jobs",
			instances: []demoInstance{
				{id: 2001, hostname: "server-a", pid: 5101},
			},
			profile: []float64{.9988, .9970, .9920, .9890, .9400, .9991, .9965},
		},
		{
			id:          "gofurry-collector",
			name:        "GoFurry Collector",
			description: "Internal collection service",
			instances: []demoInstance{
				{id: 3001, hostname: "server-c", pid: 6101},
				{id: 3002, hostname: "server-d", pid: 6201},
			},
			profile: []float64{.9999, .9999, .9989, .9976, .9930, .9860, .9994, .9991},
		},
		{
			id:          "billing-webhook",
			name:        "Billing Webhook",
			description: "Webhook receiver",
			instances: []demoInstance{
				{id: 4001, hostname: "server-b", pid: 7101},
			},
			profile: []float64{.9700, .9520, .9120, .9880, .9960, .9990, .9340},
		},
		{
			id:          "nav-backend",
			name:        "GoFurry Nav Backend",
			description: "Navigation backend",
			instances: []demoInstance{
				{id: 5001, hostname: "server-a", pid: 8101},
				{id: 5002, hostname: "server-c", pid: 8201},
			},
			profile: []float64{.9996, .9984, .9960, .9900, .9730, .9992, .9480},
		},
		{
			id:          "search-api",
			name:        "Search API",
			description: "Search endpoint",
			instances: []demoInstance{
				{id: 6001, hostname: "server-d", pid: 9101},
			},
			profile: []float64{.9910, .9820, .9650, .9340, .9980, .9990, .9880},
		},
		{
			id:          "image-proxy",
			name:        "Image Proxy",
			description: "Image resize proxy",
			instances: []demoInstance{
				{id: 7001, hostname: "server-e", pid: 10101},
				{id: 7002, hostname: "server-f", pid: 10201},
			},
			profile: []float64{.9991, .9995, .9972, .9930, .9860, .9600, .9990},
		},
		{
			id:          "auth-service",
			name:        "Auth Service",
			description: "Session and token service",
			instances: []demoInstance{
				{id: 8001, hostname: "server-a", pid: 11101},
			},
			profile: []float64{.9998, .9992, .9987, .9975, .9890, .9510, .9993},
		},
		{
			id:          "report-worker",
			name:        "Report Worker",
			description: "Scheduled reports",
			instances: []demoInstance{
				{id: 9001, hostname: "server-b", pid: 12101},
			},
			profile: []float64{.9800, .9720, .9610, .9460, .9930, .9980, .9850},
		},
		{
			id:          "mail-sender",
			name:        "Mail Sender",
			description: "Outbound notifications",
			instances: []demoInstance{
				{id: 10001, hostname: "server-c", pid: 13101},
			},
			profile: []float64{.9950, .9910, .9760, .9560, .9985, .9991, .9820},
		},
		{
			id:          "audit-log",
			name:        "Audit Log",
			description: "Append-only audit events",
			instances: []demoInstance{
				{id: 11001, hostname: "server-d", pid: 14101},
			},
			profile: []float64{.9999, .9998, .9995, .9988, .9970, .9940, .9996},
		},
		{
			id:          "preview-empty",
			name:        "Preview Empty",
			description: "Service registered without samples",
			instances: []demoInstance{
				{id: 12001, hostname: "server-z", pid: 15101},
			},
			profile: nil,
		},
	}
}

func seedServices(ctx context.Context, db *sql.DB, services []demoService, now time.Time, interval time.Duration, days int) error {
	created := now.AddDate(0, 0, -days)
	for _, service := range services {
		if _, err := db.ExecContext(ctx, `
INSERT INTO services (service_id, name, description, created_at, last_seen_at, sample_interval_nanos)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(service_id) DO UPDATE SET
	name = excluded.name,
	description = excluded.description,
	last_seen_at = excluded.last_seen_at,
	sample_interval_nanos = excluded.sample_interval_nanos
`, service.id, service.name, service.description, unixNano(created), unixNano(now), int64(interval)); err != nil {
			return err
		}
		for _, instance := range service.instances {
			if _, err := db.ExecContext(ctx, `
INSERT INTO instances (instance_id, service_id, hostname, pid, started_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(instance_id) DO UPDATE SET
	service_id = excluded.service_id,
	hostname = excluded.hostname,
	pid = excluded.pid,
	last_seen_at = excluded.last_seen_at
`, instance.id, service.id, instance.hostname, instance.pid, unixNano(created), unixNano(now)); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedDaily(ctx context.Context, db *sql.DB, services []demoService, today string, days int, interval time.Duration, loc *time.Location) error {
	stmt, err := db.PrepareContext(ctx, `
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

	for offset := days - 1; offset >= 1; offset-- {
		day := timeutil.AddDays(today, -offset, loc)
		expected := timeutil.ExpectedSlotsForDay(day, interval, loc)
		for serviceIndex, service := range services {
			if len(service.profile) == 0 {
				continue
			}
			rate := service.profile[(days-offset+serviceIndex)%len(service.profile)]
			upSlots := int(math.Round(float64(expected) * rate))
			if upSlots > expected {
				upSlots = expected
			}
			if _, err := stmt.ExecContext(ctx, service.id, day, upSlots, expected, float64(upSlots)/float64(expected)); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedTodaySamples(ctx context.Context, db *sql.DB, services []demoService, today string, now time.Time, interval time.Duration, loc *time.Location) error {
	expected := timeutil.ExpectedSlotsSoFar(now, interval, loc)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO uptime_samples (service_id, instance_id, day, slot, seen_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(service_id, instance_id, day, slot) DO UPDATE SET
	seen_at = excluded.seen_at
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for serviceIndex, service := range services {
		if len(service.profile) == 0 {
			continue
		}
		rate := service.profile[(len(service.profile)-1+serviceIndex)%len(service.profile)]
		upSlots := int(math.Round(float64(expected) * rate))
		for slot := 0; slot < upSlots; slot++ {
			instance := service.instances[slot%len(service.instances)]
			seenAt := timeutil.StartOfDay(now, loc).Add(time.Duration(slot) * interval)
			if _, err := stmt.ExecContext(ctx, service.id, instance.id, today, slot, unixNano(seenAt)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func unixNano(t time.Time) int64 {
	return t.UTC().UnixNano()
}
