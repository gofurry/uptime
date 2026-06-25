package sqlite

var pragmaStatements = []string{
	`PRAGMA journal_mode = WAL`,
	`PRAGMA synchronous = NORMAL`,
	`PRAGMA busy_timeout = 5000`,
}

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS services (
		service_id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		created_at BIGINT NOT NULL,
		last_seen_at BIGINT NOT NULL,
		sample_interval_nanos BIGINT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS instances (
		instance_id BIGINT PRIMARY KEY,
		service_id TEXT NOT NULL,
		hostname TEXT NOT NULL DEFAULT '',
		pid INTEGER NOT NULL DEFAULT 0,
		started_at BIGINT NOT NULL,
		last_seen_at BIGINT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS uptime_samples (
		service_id TEXT NOT NULL,
		instance_id BIGINT NOT NULL,
		day TEXT NOT NULL,
		slot BIGINT NOT NULL,
		seen_at BIGINT NOT NULL,
		PRIMARY KEY (service_id, instance_id, day, slot)
	)`,
	`CREATE TABLE IF NOT EXISTS uptime_daily (
		service_id TEXT NOT NULL,
		day TEXT NOT NULL,
		up_slots INTEGER NOT NULL,
		expected_slots INTEGER NOT NULL,
		uptime_rate REAL NOT NULL,
		finalized INTEGER NOT NULL,
		PRIMARY KEY (service_id, day)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_instances_service
		ON instances(service_id)`,
	`CREATE INDEX IF NOT EXISTS idx_uptime_samples_service_day
		ON uptime_samples(service_id, day)`,
	`CREATE INDEX IF NOT EXISTS idx_uptime_samples_day
		ON uptime_samples(day)`,
	`CREATE INDEX IF NOT EXISTS idx_uptime_daily_service_day
		ON uptime_daily(service_id, day)`,
}
