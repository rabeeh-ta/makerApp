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

	// The AI client that made an app over MCP ("Claude"); "" when made on the website.
	`ALTER TABLE apps ADD COLUMN made_by TEXT NOT NULL DEFAULT ''`,
	// MCP sign-in (OAuth 2.1). A client is an AI app: registered dynamically
	// (random ID) or identified by its metadata document's URL.
	`CREATE TABLE oauth_clients (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		redirect_uris TEXT NOT NULL,            -- JSON array
		created_at TEXT NOT NULL,
		fetched_at TEXT                         -- metadata documents: when the cached copy was fetched
	)`,
	// A grant is one user's permission for one client; disconnecting deletes it,
	// and its codes and tokens with it.
	`CREATE TABLE oauth_grants (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		client_id TEXT NOT NULL,
		client_name TEXT NOT NULL,
		created_at TEXT NOT NULL,
		last_used_at TEXT,
		UNIQUE (user_id, client_id)
	)`,
	`CREATE TABLE oauth_codes (
		code_hash TEXT PRIMARY KEY,
		grant_id TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
		redirect_uri TEXT NOT NULL,
		challenge TEXT NOT NULL,                -- PKCE S256
		resource TEXT NOT NULL,
		expires_at TEXT NOT NULL
	)`,
	`CREATE TABLE oauth_tokens (
		token_hash TEXT PRIMARY KEY,
		grant_id TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
		kind TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
		expires_at TEXT NOT NULL,
		used_at TEXT                            -- refresh tokens: when exchanged; a second use revokes the grant
	)`,
	`CREATE INDEX idx_oauth_tokens_grant ON oauth_tokens(grant_id)`,

	// A personal source: everyone who installs an app using it sends their own
	// data, to their own copy of it (origin_id is the source it copies). The
	// owner's data never reaches anyone else's board.
	`ALTER TABLE sources ADD COLUMN per_user INTEGER NOT NULL DEFAULT 0 CHECK (per_user IN (0, 1))`,
	`ALTER TABLE sources ADD COLUMN origin_id TEXT REFERENCES sources(id) ON DELETE CASCADE`,
	`CREATE UNIQUE INDEX idx_sources_origin ON sources(origin_id, user_id) WHERE origin_id IS NOT NULL`,
	// How to set an app up, shown on its App Store page.
	`ALTER TABLE apps ADD COLUMN setup TEXT NOT NULL DEFAULT ''`,
	// A personal source's example data: what previews on the website show
	// someone who hasn't sent their own yet. Boards only ever show real data.
	`ALTER TABLE sources ADD COLUMN example TEXT NOT NULL DEFAULT ''`,
	// What a board plays when it sits on its Apps drawer: screens of its
	// installed apps (a JSON list of {"app", "screen"}), each for interval_sec.
	`CREATE TABLE screensavers (
		device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
		interval_sec INTEGER NOT NULL DEFAULT 10,
		items TEXT NOT NULL DEFAULT '[]'
	)`,
	// Converted GIFs, about 60 KB each at most. Named by their content's hash, and
	// never changed or removed while a board or a published app may still ask for one.
	`CREATE TABLE animations (
		hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		data BLOB NOT NULL,
		created_at TEXT NOT NULL
	)`,
	// A CHECK can't be altered, so screens is rebuilt to allow the gif layout. Same
	// columns, so the previous release keeps working, and no table references screens.
	`CREATE TABLE screens_new (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		app_id TEXT,
		app_pos INTEGER NOT NULL DEFAULT 0,
		name TEXT NOT NULL,
		source_id TEXT REFERENCES sources(id) ON DELETE SET NULL,
		layout TEXT NOT NULL CHECK (layout IN ('big_number', 'list', 'text', 'spark', 'progress', 'gif')),
		spec TEXT NOT NULL DEFAULT '{}',
		updated_at TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	INSERT INTO screens_new (id, user_id, app_id, app_pos, name, source_id, layout, spec, updated_at, created_at)
		SELECT id, user_id, app_id, app_pos, name, source_id, layout, spec, updated_at, created_at FROM screens;
	DROP TABLE screens;
	ALTER TABLE screens_new RENAME TO screens;
	CREATE INDEX idx_screens_source ON screens(source_id);
	CREATE INDEX idx_screens_app ON screens(app_id);`,
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
