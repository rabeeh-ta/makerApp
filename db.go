package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // pure Go, so CGO_ENABLED=0 cross-compiles
)

func openDB(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// migrations run in order, once each. Only ever append to this list: the
// previous release may still be running against the new schema for a few
// seconds during a deploy, and rolling back doesn't undo a migration.
//
// Times are TEXT in UTC ("2026-01-31T09:30:00Z"); NULL means never.
var migrations = []string{
	`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		username TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE UNIQUE INDEX idx_users_username ON users(username) WHERE username IS NOT NULL`,
	`CREATE TABLE sessions (
		token_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at TEXT NOT NULL
	)`,
	// One-time tokens written into firmware flashed from the site.
	`CREATE TABLE claims (
		token TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at TEXT NOT NULL,
		device_id TEXT
	)`,
	`CREATE TABLE devices (
		id TEXT PRIMARY KEY,
		user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		name TEXT NOT NULL DEFAULT '',
		token_hash TEXT NOT NULL,
		pair_code TEXT NOT NULL DEFAULT '',
		fw_version TEXT NOT NULL DEFAULT '',
		board TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		rssi INTEGER NOT NULL DEFAULT 0,
		brightness INTEGER NOT NULL DEFAULT 100,
		flipped INTEGER NOT NULL DEFAULT 0 CHECK (flipped IN (0, 1)),
		offset_x INTEGER NOT NULL DEFAULT 0,
		offset_y INTEGER NOT NULL DEFAULT 0,
		last_seen TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE sources (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		kind TEXT NOT NULL CHECK (kind IN ('http', 'static', 'push')),
		method TEXT NOT NULL DEFAULT 'GET',
		url TEXT NOT NULL DEFAULT '',
		secret TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		interval_sec INTEGER NOT NULL DEFAULT 300,
		push_token TEXT NOT NULL DEFAULT '',
		last_data TEXT NOT NULL DEFAULT '',
		last_status INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT '',
		last_fetched_at TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX idx_sources_user ON sources(user_id)`,
	`CREATE UNIQUE INDEX idx_sources_push ON sources(push_token) WHERE push_token <> ''`,
	`CREATE TABLE apps (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		cycle_sec INTEGER NOT NULL DEFAULT 0,
		published_at TEXT,
		updated_at TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX idx_apps_user ON apps(user_id)`,
	// app_id has no foreign key: DeleteApp removes an app's screens itself.
	`CREATE TABLE screens (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		app_id TEXT,
		app_pos INTEGER NOT NULL DEFAULT 0,
		name TEXT NOT NULL,
		source_id TEXT REFERENCES sources(id) ON DELETE SET NULL,
		layout TEXT NOT NULL CHECK (layout IN ('big_number', 'list', 'text', 'spark', 'progress')),
		spec TEXT NOT NULL DEFAULT '{}',
		updated_at TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX idx_screens_source ON screens(source_id)`,
	`CREATE INDEX idx_screens_app ON screens(app_id)`,
	// app_id is a user app's ID or a built-in's ("clock"), so it has no foreign key.
	`CREATE TABLE device_apps (
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		app_id TEXT NOT NULL,
		pos INTEGER NOT NULL,
		PRIMARY KEY (device_id, app_id)
	)`,
	`CREATE INDEX idx_device_apps_app ON device_apps(app_id)`,
	// Every board that has ever installed an app, for its install count.
	`CREATE TABLE app_installs (
		app_id TEXT NOT NULL,
		device_id TEXT NOT NULL,
		at TEXT NOT NULL,
		PRIMARY KEY (app_id, device_id)
	)`,
	// Holds the generated SECRET_KEY when running locally without one.
	`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
