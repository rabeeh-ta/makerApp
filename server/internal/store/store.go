// Package store holds everything espHog persists. It speaks plain SQLite SQL so
// the same code runs on a local file (modernc.org/sqlite, no CGO) or on Turso
// (libsql:// URLs).
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

// Open accepts a file path ("data/esphog.db") or a Turso URL
// ("libsql://name-org.turso.io?authToken=...").
func Open(dsn string) (*Store, error) {
	var db *sql.DB
	var err error
	if strings.HasPrefix(dsn, "libsql://") || strings.HasPrefix(dsn, "https://") {
		db, err = sql.Open("libsql", dsn)
	} else {
		db, err = sql.Open("sqlite", "file:"+dsn+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
		if err == nil {
			db.SetMaxOpenConns(1) // SQLite has one writer; this avoids SQLITE_BUSY churn.
		}
	}
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS sessions (
		token_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS claims (
		token TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at INTEGER NOT NULL,
		device_id TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS devices (
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
		rotate_sec INTEGER NOT NULL DEFAULT 0,
		last_seen INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS sources (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		method TEXT NOT NULL DEFAULT 'GET',
		url TEXT NOT NULL DEFAULT '',
		secret TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		interval_sec INTEGER NOT NULL DEFAULT 300,
		push_token TEXT NOT NULL DEFAULT '',
		last_data TEXT NOT NULL DEFAULT '',
		last_status INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT '',
		last_fetched_at INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS screens (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		source_id TEXT REFERENCES sources(id) ON DELETE SET NULL,
		layout TEXT NOT NULL,
		spec TEXT NOT NULL DEFAULT '{}',
		updated_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS device_screens (
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		screen_id TEXT NOT NULL REFERENCES screens(id) ON DELETE CASCADE,
		pos INTEGER NOT NULL,
		PRIMARY KEY (device_id, screen_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_sources_user ON sources(user_id)`,
	`CREATE INDEX IF NOT EXISTS idx_screens_source ON screens(source_id)`,
	`CREATE INDEX IF NOT EXISTS idx_device_screens_screen ON device_screens(screen_id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_push ON sources(push_token) WHERE push_token <> ''`,
	`CREATE TABLE IF NOT EXISTS flows (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		spec TEXT NOT NULL DEFAULT '{}',
		updated_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS apps (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		cycle_sec INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	// app_id is a user app's ID or a built-in's ("clock"), so it has no foreign key.
	`CREATE TABLE IF NOT EXISTS device_apps (
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		app_id TEXT NOT NULL,
		pos INTEGER NOT NULL,
		PRIMARY KEY (device_id, app_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_apps_user ON apps(user_id)`,
	`CREATE INDEX IF NOT EXISTS idx_device_apps_app ON device_apps(app_id)`,
	// Every board that has ever installed an app, for its install count.
	`CREATE TABLE IF NOT EXISTS app_installs (
		app_id TEXT NOT NULL,
		device_id TEXT NOT NULL,
		at INTEGER NOT NULL,
		PRIMARY KEY (app_id, device_id)
	)`,
}

// columnMigrations add columns to existing tables (SQLite has no ADD COLUMN IF NOT EXISTS).
var columnMigrations = []struct{ table, column, ddl string }{
	{"devices", "flow_id", `ALTER TABLE devices ADD COLUMN flow_id TEXT`},
	{"devices", "flipped", `ALTER TABLE devices ADD COLUMN flipped INTEGER NOT NULL DEFAULT 0`},
	{"devices", "offset_x", `ALTER TABLE devices ADD COLUMN offset_x INTEGER NOT NULL DEFAULT 0`},
	{"devices", "offset_y", `ALTER TABLE devices ADD COLUMN offset_y INTEGER NOT NULL DEFAULT 0`},
	{"screens", "device_id", `ALTER TABLE screens ADD COLUMN device_id TEXT`},
	{"flows", "device_id", `ALTER TABLE flows ADD COLUMN device_id TEXT`},
	{"screens", "app_id", `ALTER TABLE screens ADD COLUMN app_id TEXT`},
	{"screens", "app_pos", `ALTER TABLE screens ADD COLUMN app_pos INTEGER NOT NULL DEFAULT 0`},
	{"users", "username", `ALTER TABLE users ADD COLUMN username TEXT`},
	{"apps", "description", `ALTER TABLE apps ADD COLUMN description TEXT NOT NULL DEFAULT ''`},
	{"apps", "published_at", `ALTER TABLE apps ADD COLUMN published_at INTEGER NOT NULL DEFAULT 0`},
}

func (s *Store) migrate(ctx context.Context) error {
	for _, m := range migrations {
		if _, err := s.db.ExecContext(ctx, m); err != nil {
			return err
		}
	}
	for _, c := range columnMigrations {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.column).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := s.db.ExecContext(ctx, c.ddl); err != nil {
				return err
			}
		}
	}
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS idx_screens_app ON screens(app_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username) WHERE username IS NOT NULL`,
		// Installs from before the counter count too.
		`INSERT OR IGNORE INTO app_installs (app_id, device_id, at) SELECT app_id, device_id, 0 FROM device_apps`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return s.migrateToApps(ctx)
}

// NewID returns a short random identifier.
func NewID() string { return RandomToken(8) }

func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func HashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func now() int64 { return time.Now().Unix() }

// queryIDs runs a query that selects a single text column.
func (s *Store) queryIDs(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// moved returns ids with id shifted one place earlier (delta -1) or later (+1),
// and false when it can't move.
func moved(ids []string, id string, delta int) ([]string, bool) {
	i := slices.Index(ids, id)
	j := i + delta
	if i < 0 || j < 0 || j >= len(ids) {
		return ids, false
	}
	ids[i], ids[j] = ids[j], ids[i]
	return ids, true
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ---- meta ----

func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v, notFound(err)
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---- users & sessions ----

type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    int64
	Username     string // public name for published apps; "" until chosen
}

var ErrTaken = errors.New("taken")

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	u := &User{ID: NewID(), Email: email, PasswordHash: passwordHash, CreatedAt: now()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := s.db.QueryRowContext(ctx, `SELECT id, email, password_hash, created_at, COALESCE(username, '') FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.Username)
	return u, notFound(err)
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	token := RandomToken(32)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		HashToken(token), userID, time.Now().Add(ttl).Unix())
	return token, err
}

func (s *Store) UserBySession(ctx context.Context, token string) (*User, error) {
	u := &User{}
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.email, u.password_hash, u.created_at, COALESCE(u.username, '')
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, HashToken(token), now()).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.Username)
	return u, notFound(err)
}

// SetUsername gives the user a public name, once. ErrTaken if someone has it.
func (s *Store) SetUsername(ctx context.Context, userID, name string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, name).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrTaken
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET username = ? WHERE id = ? AND username IS NULL`, name, userID)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, HashToken(token))
	return err
}

// ---- claims: one-time tokens written into firmware flashed from the site ----

func (s *Store) CreateClaim(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	token := RandomToken(16)
	_, err := s.db.ExecContext(ctx, `INSERT INTO claims (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, time.Now().Add(ttl).Unix())
	return token, err
}

// UseClaim binds an unused, unexpired claim to deviceID and returns its owner.
// Re-using a claim for the same device succeeds, so reconnects are harmless.
func (s *Store) UseClaim(ctx context.Context, token, deviceID string) (string, error) {
	var userID string
	var bound sql.NullString
	var exp int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id, device_id, expires_at FROM claims WHERE token = ?`, token).
		Scan(&userID, &bound, &exp)
	if err != nil {
		return "", notFound(err)
	}
	if bound.Valid && bound.String != "" {
		if bound.String == deviceID {
			return userID, nil
		}
		return "", ErrNotFound
	}
	if exp < now() {
		return "", ErrNotFound
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE claims SET device_id = ? WHERE token = ?`, deviceID, token); err != nil {
		return "", err
	}
	return userID, nil
}

// RecentClaimDevice returns the device a claim was used by, if any.
func (s *Store) ClaimDevice(ctx context.Context, token, userID string) (string, error) {
	var d sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT device_id FROM claims WHERE token = ? AND user_id = ?`, token, userID).Scan(&d)
	if err != nil {
		return "", notFound(err)
	}
	return d.String, nil
}

// ---- devices ----

type Device struct {
	ID         string
	UserID     string
	Name       string
	TokenHash  string
	PairCode   string
	FWVersion  string
	Board      string
	IP         string
	RSSI       int
	Brightness int
	LastSeen   int64
	CreatedAt  int64
	Flipped    bool // display turned 180°
	OffsetX    int  // picture nudge in pixels
	OffsetY    int
}

const deviceCols = `id, COALESCE(user_id, ''), name, token_hash, pair_code, fw_version, board, ip, rssi, brightness, last_seen, created_at, flipped, offset_x, offset_y`

func scanDevice(row interface{ Scan(...any) error }) (*Device, error) {
	d := &Device{}
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.TokenHash, &d.PairCode, &d.FWVersion, &d.Board, &d.IP, &d.RSSI,
		&d.Brightness, &d.LastSeen, &d.CreatedAt, &d.Flipped, &d.OffsetX, &d.OffsetY)
	return d, notFound(err)
}

func (s *Store) Device(ctx context.Context, id string) (*Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE id = ?`, id))
}

func (s *Store) UserDevice(ctx context.Context, userID, id string) (*Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) UserDevices(ctx context.Context, userID string) ([]*Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UnclaimedDeviceByCode finds a device waiting to be paired that was seen recently.
func (s *Store) UnclaimedDeviceByCode(ctx context.Context, code string, seenSince time.Time) (*Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices
		WHERE user_id IS NULL AND pair_code = ? AND last_seen >= ? ORDER BY last_seen DESC LIMIT 1`, code, seenSince.Unix()))
}

// UpsertDeviceHello records a device's hello. It creates the row on first
// contact and refreshes the connection details after that.
func (s *Store) UpsertDeviceHello(ctx context.Context, d *Device) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices (id, name, token_hash, pair_code, fw_version, board, ip, rssi, last_seen, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET token_hash = excluded.token_hash, pair_code = excluded.pair_code,
			fw_version = excluded.fw_version, board = excluded.board, ip = excluded.ip, rssi = excluded.rssi,
			last_seen = excluded.last_seen`,
		d.ID, d.Name, d.TokenHash, d.PairCode, d.FWVersion, d.Board, d.IP, d.RSSI, now(), now())
	return err
}

func (s *Store) TouchDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET last_seen = ? WHERE id = ?`, now(), id)
	return err
}

func (s *Store) ClaimDeviceFor(ctx context.Context, id, userID, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET user_id = ?, name = CASE WHEN name = '' THEN ? ELSE name END WHERE id = ?`,
		userID, name, id)
	return err
}

type DeviceSettings struct {
	Name             string
	Brightness       int
	Flipped          bool
	OffsetX, OffsetY int
}

func (s *Store) UpdateDeviceSettings(ctx context.Context, userID, id string, d DeviceSettings) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET name = ?, brightness = ?, flipped = ?, offset_x = ?, offset_y = ?
		WHERE id = ? AND user_id = ?`, d.Name, d.Brightness, d.Flipped, d.OffsetX, d.OffsetY, id, userID)
	return err
}

// ForgetDevice releases the device so it can be paired again.
func (s *Store) ForgetDevice(ctx context.Context, userID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_apps WHERE device_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET user_id = NULL, name = '', brightness = 100
		WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- sources ----

type Source struct {
	ID            string
	UserID        string
	Name          string
	Kind          string // http | static | push
	Method        string
	URL           string
	Secret        string // encrypted JSON {"auth": "...", "headers": "..."}
	Body          string
	IntervalSec   int
	PushToken     string
	LastData      string
	LastStatus    int
	LastError     string
	LastFetchedAt int64
	CreatedAt     int64
}

const sourceCols = `id, user_id, name, kind, method, url, secret, body, interval_sec, push_token, last_data, last_status, last_error, last_fetched_at, created_at`

func scanSource(row interface{ Scan(...any) error }) (*Source, error) {
	x := &Source{}
	err := row.Scan(&x.ID, &x.UserID, &x.Name, &x.Kind, &x.Method, &x.URL, &x.Secret, &x.Body, &x.IntervalSec,
		&x.PushToken, &x.LastData, &x.LastStatus, &x.LastError, &x.LastFetchedAt, &x.CreatedAt)
	return x, notFound(err)
}

func (s *Store) querySources(ctx context.Context, q string, args ...any) ([]*Source, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Source
	for rows.Next() {
		x, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) Source(ctx context.Context, id string) (*Source, error) {
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE id = ?`, id))
}

func (s *Store) UserSource(ctx context.Context, userID, id string) (*Source, error) {
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) SourceByPushToken(ctx context.Context, token string) (*Source, error) {
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE push_token = ? AND kind = 'push'`, token))
}

func (s *Store) UserSources(ctx context.Context, userID string) ([]*Source, error) {
	return s.querySources(ctx, `SELECT `+sourceCols+` FROM sources WHERE user_id = ? ORDER BY name`, userID)
}

// DueSources lists HTTP sources whose interval has elapsed.
func (s *Store) DueSources(ctx context.Context) ([]*Source, error) {
	return s.querySources(ctx, `SELECT `+sourceCols+` FROM sources
		WHERE kind = 'http' AND url <> '' AND last_fetched_at + interval_sec <= ?`, now())
}

func (s *Store) SaveSource(ctx context.Context, x *Source) error {
	if x.ID == "" {
		x.ID = NewID()
		x.CreatedAt = now()
		_, err := s.db.ExecContext(ctx, `INSERT INTO sources (`+sourceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			x.ID, x.UserID, x.Name, x.Kind, x.Method, x.URL, x.Secret, x.Body, x.IntervalSec, x.PushToken,
			x.LastData, x.LastStatus, x.LastError, x.LastFetchedAt, x.CreatedAt)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sources SET name = ?, kind = ?, method = ?, url = ?, secret = ?, body = ?,
		interval_sec = ?, push_token = ?, last_data = ?, last_status = ?, last_error = ?, last_fetched_at = ?
		WHERE id = ? AND user_id = ?`,
		x.Name, x.Kind, x.Method, x.URL, x.Secret, x.Body, x.IntervalSec, x.PushToken, x.LastData, x.LastStatus,
		x.LastError, x.LastFetchedAt, x.ID, x.UserID)
	return err
}

// SetSourceResult stores a fetch outcome. data is left untouched when empty
// so the last good data keeps showing while a source is failing.
func (s *Store) SetSourceResult(ctx context.Context, id, data string, status int, errText string) error {
	if data == "" {
		_, err := s.db.ExecContext(ctx, `UPDATE sources SET last_status = ?, last_error = ?, last_fetched_at = ? WHERE id = ?`,
			status, errText, now(), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sources SET last_data = ?, last_status = ?, last_error = ?, last_fetched_at = ? WHERE id = ?`,
		data, status, errText, now(), id)
	return err
}

func (s *Store) DeleteSource(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sources WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		// Explicit because Turso connections don't enforce foreign keys.
		_, err = s.db.ExecContext(ctx, `UPDATE screens SET source_id = NULL WHERE source_id = ?`, id)
	}
	return err
}

// ---- screens ----

type Screen struct {
	ID        string
	UserID    string
	Name      string
	SourceID  string
	Layout    string
	Spec      string // JSON, see render.Spec
	UpdatedAt int64
	CreatedAt int64
	AppID     string // the app this screen belongs to
}

const screenCols = `id, user_id, name, COALESCE(source_id, ''), layout, spec, updated_at, created_at, COALESCE(app_id, '')`

func scanScreen(row interface{ Scan(...any) error }) (*Screen, error) {
	x := &Screen{}
	err := row.Scan(&x.ID, &x.UserID, &x.Name, &x.SourceID, &x.Layout, &x.Spec, &x.UpdatedAt, &x.CreatedAt, &x.AppID)
	return x, notFound(err)
}

func (s *Store) queryScreens(ctx context.Context, q string, args ...any) ([]*Screen, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Screen
	for rows.Next() {
		x, err := scanScreen(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) UserScreen(ctx context.Context, userID, id string) (*Screen, error) {
	return scanScreen(s.db.QueryRowContext(ctx, `SELECT `+screenCols+` FROM screens WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) UserScreens(ctx context.Context, userID string) ([]*Screen, error) {
	return s.queryScreens(ctx, `SELECT `+screenCols+` FROM screens WHERE user_id = ? ORDER BY name`, userID)
}

// AppScreens lists an app's screens in order.
func (s *Store) AppScreens(ctx context.Context, appID string) ([]*Screen, error) {
	return s.queryScreens(ctx, `SELECT `+screenCols+` FROM screens WHERE app_id = ? ORDER BY app_pos, created_at`, appID)
}

func (s *Store) ScreenIDsForSource(ctx context.Context, sourceID string) ([]string, error) {
	return s.queryIDs(ctx, `SELECT id FROM screens WHERE source_id = ?`, sourceID)
}

func (s *Store) SaveScreen(ctx context.Context, x *Screen) error {
	var src any
	if x.SourceID != "" {
		src = x.SourceID
	}
	x.UpdatedAt = now()
	if x.ID == "" {
		x.ID = NewID()
		x.CreatedAt = x.UpdatedAt
		var app any
		if x.AppID != "" {
			app = x.AppID
		}
		// New screens join the end of their app.
		_, err := s.db.ExecContext(ctx, `INSERT INTO screens (id, user_id, name, source_id, layout, spec, updated_at, created_at, app_id, app_pos)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(app_pos), -1) + 1 FROM screens WHERE app_id = ?))`,
			x.ID, x.UserID, x.Name, src, x.Layout, x.Spec, x.UpdatedAt, x.CreatedAt, app, x.AppID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE screens SET name = ?, source_id = ?, layout = ?, spec = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, x.Name, src, x.Layout, x.Spec, x.UpdatedAt, x.ID, x.UserID)
	return err
}

func (s *Store) DeleteScreen(ctx context.Context, userID, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM screens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// MoveAppScreen shifts a screen one place earlier (-1) or later (+1) in its app.
func (s *Store) MoveAppScreen(ctx context.Context, appID, screenID string, delta int) error {
	screens, err := s.AppScreens(ctx, appID)
	if err != nil {
		return err
	}
	ids := make([]string, len(screens))
	for i, sc := range screens {
		ids[i] = sc.ID
	}
	ids, ok := moved(ids, screenID, delta)
	if !ok {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE screens SET app_pos = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- apps ----

// App is what a board lists in its Apps drawer: a name and an ordered set of
// screens. Catalog apps (engine.Catalog: the clock, games, tools) aren't stored
// here; devices install them by ID.
type App struct {
	ID          string
	UserID      string
	Name        string
	CycleSec    int // move to the next screen after this long without a press; 0 = off
	UpdatedAt   int64
	CreatedAt   int64
	Description string // shown in the public list
	PublishedAt int64  // 0 unless anyone can install it
}

const appCols = `id, user_id, name, cycle_sec, updated_at, created_at, description, published_at`

func scanApp(row interface{ Scan(...any) error }) (*App, error) {
	a := &App{}
	err := row.Scan(&a.ID, &a.UserID, &a.Name, &a.CycleSec, &a.UpdatedAt, &a.CreatedAt, &a.Description, &a.PublishedAt)
	return a, notFound(err)
}

func (s *Store) UserApp(ctx context.Context, userID, id string) (*App, error) {
	return scanApp(s.db.QueryRowContext(ctx, `SELECT `+appCols+` FROM apps WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) UserApps(ctx context.Context, userID string) ([]*App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appCols+` FROM apps WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SaveApp(ctx context.Context, a *App) error {
	a.UpdatedAt = now()
	if a.ID == "" {
		a.ID = NewID()
		a.CreatedAt = a.UpdatedAt
		_, err := s.db.ExecContext(ctx, `INSERT INTO apps (`+appCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.UserID, a.Name, a.CycleSec, a.UpdatedAt, a.CreatedAt, a.Description, a.PublishedAt)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET name = ?, cycle_sec = ?, updated_at = ?, description = ?, published_at = ?
		WHERE id = ? AND user_id = ?`, a.Name, a.CycleSec, a.UpdatedAt, a.Description, a.PublishedAt, a.ID, a.UserID)
	return err
}

// DeleteApp removes an app with its screens, and uninstalls it everywhere.
func (s *Store) DeleteApp(ctx context.Context, userID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM apps WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM screens WHERE app_id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	for _, q := range []string{`DELETE FROM device_apps WHERE app_id = ?`, `DELETE FROM app_installs WHERE app_id = ?`} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PublishedApp is an app anyone can install, with its publisher's name.
type PublishedApp struct {
	*App
	Publisher string
}

func (s *Store) PublishedApps(ctx context.Context) ([]PublishedApp, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.user_id, a.name, a.cycle_sec, a.updated_at, a.created_at, a.description,
		a.published_at, COALESCE(u.username, '') FROM apps a JOIN users u ON u.id = a.user_id WHERE a.published_at > 0
		ORDER BY a.published_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishedApp
	for rows.Next() {
		p := PublishedApp{App: &App{}}
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.CycleSec, &p.UpdatedAt, &p.CreatedAt, &p.Description,
			&p.PublishedAt, &p.Publisher); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// InstallableApp is an app the user may install: their own, or a published one.
// publisher is the owner's public name ("" for the user's own apps).
func (s *Store) InstallableApp(ctx context.Context, userID, id string) (a *App, publisher string, err error) {
	a = &App{}
	err = s.db.QueryRowContext(ctx, `SELECT a.id, a.user_id, a.name, a.cycle_sec, a.updated_at, a.created_at, a.description,
		a.published_at, CASE WHEN a.user_id = ? THEN '' ELSE COALESCE(u.username, '') END
		FROM apps a JOIN users u ON u.id = a.user_id WHERE a.id = ? AND (a.user_id = ? OR a.published_at > 0)`, userID, id, userID).
		Scan(&a.ID, &a.UserID, &a.Name, &a.CycleSec, &a.UpdatedAt, &a.CreatedAt, &a.Description, &a.PublishedAt, &publisher)
	return a, publisher, notFound(err)
}

// Unpublish takes the app out of the public list and off other people's
// boards, returning those boards so they can be updated.
func (s *Store) Unpublish(ctx context.Context, userID, id string) ([]string, error) {
	devices, err := s.queryIDs(ctx, `SELECT da.device_id FROM device_apps da JOIN apps a ON a.id = da.app_id
		WHERE da.app_id = ? AND a.user_id = ? AND da.device_id NOT IN (SELECT id FROM devices WHERE user_id = ?)`, id, userID, userID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE apps SET published_at = 0 WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return nil, err
	}
	for _, d := range devices {
		if _, err := tx.ExecContext(ctx, `DELETE FROM device_apps WHERE app_id = ? AND device_id = ?`, id, d); err != nil {
			return nil, err
		}
	}
	return devices, tx.Commit()
}

// InstallCounts is how many boards have ever installed each app.
func (s *Store) InstallCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT app_id, COUNT(*) FROM app_installs GROUP BY app_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ---- installed apps ----

// DeviceAppIDs lists the apps installed on a device, in drawer order.
func (s *Store) DeviceAppIDs(ctx context.Context, deviceID string) ([]string, error) {
	return s.queryIDs(ctx, `SELECT app_id FROM device_apps WHERE device_id = ? ORDER BY pos`, deviceID)
}

// InstallApp adds an app to the end of a device's drawer and counts the
// install. Installing it again does nothing.
func (s *Store) InstallApp(ctx context.Context, deviceID, appID string) error {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO device_apps (device_id, app_id, pos)
		VALUES (?, ?, (SELECT COALESCE(MAX(pos), -1) + 1 FROM device_apps WHERE device_id = ?))`, deviceID, appID, deviceID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO app_installs (app_id, device_id, at) VALUES (?, ?, ?)`, appID, deviceID, now())
	return err
}

func (s *Store) UninstallApp(ctx context.Context, deviceID, appID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM device_apps WHERE device_id = ? AND app_id = ?`, deviceID, appID)
	return err
}

// MoveDeviceApp shifts an installed app one place earlier (-1) or later (+1) in the drawer.
func (s *Store) MoveDeviceApp(ctx context.Context, deviceID, appID string, delta int) error {
	ids, err := s.DeviceAppIDs(ctx, deviceID)
	if err != nil {
		return err
	}
	ids, ok := moved(ids, appID, delta)
	if !ok {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE device_apps SET pos = ? WHERE device_id = ? AND app_id = ?`, i, deviceID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DevicesWithApp lists the devices an app is installed on.
func (s *Store) DevicesWithApp(ctx context.Context, appID string) ([]string, error) {
	return s.queryIDs(ctx, `SELECT device_id FROM device_apps WHERE app_id = ?`, appID)
}

// DevicesForScreens lists devices with an app installed that contains any of the given screens.
func (s *Store) DevicesForScreens(ctx context.Context, screenIDs []string) ([]string, error) {
	if len(screenIDs) == 0 {
		return nil, nil
	}
	args := make([]any, len(screenIDs))
	for i, id := range screenIDs {
		args[i] = id
	}
	return s.queryIDs(ctx, `SELECT DISTINCT da.device_id FROM device_apps da JOIN screens s ON s.app_id = da.app_id
		WHERE s.id IN (?`+strings.Repeat(",?", len(screenIDs)-1)+`)`, args...)
}

// ---- migration from device screens and flows ----

// migrateToApps moves the older layout onto apps, once. Before apps, screens
// belonged to a device and an optional flow put a clock and a menu in front of
// them. Now each menu entry becomes an app, a device's other screens become a
// "Screens" app, and a clock home becomes the built-in Clock.
func (s *Store) migrateToApps(ctx context.Context) error {
	if done, _ := s.Meta(ctx, "apps_migrated"); done != "" {
		return nil
	}
	type oldDevice struct{ id, userID, flowSpec string }
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.user_id, COALESCE(f.spec, '') FROM devices d
		LEFT JOIN flows f ON f.id = d.flow_id WHERE d.user_id IS NOT NULL ORDER BY d.created_at`)
	if err != nil {
		return err
	}
	var devices []oldDevice
	for rows.Next() {
		var d oldDevice
		if err := rows.Scan(&d.id, &d.userID, &d.flowSpec); err != nil {
			rows.Close()
			return err
		}
		devices = append(devices, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, d := range devices {
		var spec struct {
			Template *struct {
				Home string `json:"home"`
				Apps []struct {
					Name    string   `json:"name"`
					Screens []string `json:"screens"`
				} `json:"apps"`
			} `json:"template"`
		}
		_ = json.Unmarshal([]byte(d.flowSpec), &spec)
		if t := spec.Template; t != nil {
			if t.Home == "" || t.Home == "clock" {
				if err := s.InstallApp(ctx, d.id, "clock"); err != nil {
					return err
				}
			}
			for _, a := range t.Apps {
				if err := s.migrateApp(ctx, d.id, d.userID, a.Name, a.Screens); err != nil {
					return err
				}
			}
		}
		// Screens designed for this device that no menu entry took, in its old order.
		rest, err := s.queryIDs(ctx, `SELECT s.id FROM screens s
			LEFT JOIN device_screens ds ON ds.screen_id = s.id AND ds.device_id = s.device_id
			WHERE s.device_id = ? AND s.app_id IS NULL ORDER BY COALESCE(ds.pos, 1000000), s.created_at`, d.id)
		if err != nil {
			return err
		}
		if err := s.migrateApp(ctx, d.id, d.userID, "Screens", rest); err != nil {
			return err
		}
	}

	// Screens that were never on a device go into an app per user, not installed.
	users, err := s.queryIDs(ctx, `SELECT DISTINCT user_id FROM screens WHERE app_id IS NULL`)
	if err != nil {
		return err
	}
	for _, u := range users {
		ids, err := s.queryIDs(ctx, `SELECT id FROM screens WHERE user_id = ? AND app_id IS NULL ORDER BY created_at`, u)
		if err != nil {
			return err
		}
		if err := s.migrateApp(ctx, "", u, "Screens", ids); err != nil {
			return err
		}
	}
	return s.SetMeta(ctx, "apps_migrated", "1")
}

// migrateApp makes an app from the user's screens that aren't in an app yet,
// installing it on deviceID when given. Nothing is made if no screens qualify.
func (s *Store) migrateApp(ctx context.Context, deviceID, userID, name string, screenIDs []string) error {
	var ids []string
	for _, id := range screenIDs {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM screens WHERE id = ? AND user_id = ? AND app_id IS NULL`,
			id, userID).Scan(&n); err != nil {
			return err
		}
		if n > 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if name == "" {
		name = "Screens"
	}
	a := &App{UserID: userID, Name: name}
	if err := s.SaveApp(ctx, a); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE screens SET app_id = ?, app_pos = ? WHERE id = ?`, a.ID, i, id); err != nil {
			return err
		}
	}
	if deviceID == "" {
		return nil
	}
	return s.InstallApp(ctx, deviceID, a.ID)
}
