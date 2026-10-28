// Package store holds everything makerApp persists, in plain SQLite SQL. The
// schema is the migrations list in db.go.
//
// Times are stored as UTC text ("2026-01-31T09:30:00Z"), with NULL for never.
// In Go they're Unix seconds, with 0 for never: reads convert with unixepoch()
// and writes with iso().
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

// New wraps a database opened and migrated by openDB in db.go.
func New(db *sql.DB) *Store { return &Store{db: db} }

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

// iso formats Unix seconds for storage; 0 (never) is stored as NULL.
func iso(unix int64) any {
	if unix == 0 {
		return nil
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02T15:04:05Z")
}

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
	PasswordHash string `json:"-"`
	CreatedAt    int64
	Username     string // public name for published apps; "" until chosen
}

var ErrTaken = errors.New("taken")

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	u := &User{ID: NewID(), Email: email, PasswordHash: passwordHash, CreatedAt: now()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, iso(u.CreatedAt))
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := s.db.QueryRowContext(ctx, `SELECT id, email, password_hash, unixepoch(created_at), COALESCE(username, '') FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.Username)
	return u, notFound(err)
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	token := RandomToken(32)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		HashToken(token), userID, iso(time.Now().Add(ttl).Unix()))
	return token, err
}

func (s *Store) UserBySession(ctx context.Context, token string) (*User, error) {
	u := &User{}
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.email, u.password_hash, unixepoch(u.created_at), COALESCE(u.username, '')
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, HashToken(token), iso(now())).
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

// SetPassword changes the password and signs out every other session, keeping
// the one that made the change.
func (s *Store) SetPassword(ctx context.Context, userID, passwordHash, keepSession string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, HashToken(keepSession)); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- claims: one-time tokens written into firmware flashed from the site ----

func (s *Store) CreateClaim(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	token := RandomToken(16)
	_, err := s.db.ExecContext(ctx, `INSERT INTO claims (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, iso(time.Now().Add(ttl).Unix()))
	return token, err
}

// UseClaim binds an unused, unexpired claim to deviceID and returns its owner.
// Re-using a claim for the same device succeeds, so reconnects are harmless.
func (s *Store) UseClaim(ctx context.Context, token, deviceID string) (string, error) {
	var userID string
	var bound sql.NullString
	var exp int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id, device_id, unixepoch(expires_at) FROM claims WHERE token = ?`, token).
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
	TokenHash  string `json:"-"`
	PairCode   string `json:"-"`
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

const deviceCols = `id, COALESCE(user_id, ''), name, token_hash, pair_code, fw_version, board, ip, rssi, brightness, COALESCE(unixepoch(last_seen), 0), unixepoch(created_at), flipped, offset_x, offset_y`

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
		WHERE user_id IS NULL AND pair_code = ? AND last_seen >= ? ORDER BY last_seen DESC LIMIT 1`, code, iso(seenSince.Unix())))
}

// UpsertDeviceHello records a device's hello. It creates the row on first
// contact and refreshes the connection details after that.
func (s *Store) UpsertDeviceHello(ctx context.Context, d *Device) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices (id, name, token_hash, pair_code, fw_version, board, ip, rssi, last_seen, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET token_hash = excluded.token_hash, pair_code = excluded.pair_code,
			fw_version = excluded.fw_version, board = excluded.board, ip = excluded.ip, rssi = excluded.rssi,
			last_seen = excluded.last_seen`,
		d.ID, d.Name, d.TokenHash, d.PairCode, d.FWVersion, d.Board, d.IP, d.RSSI, iso(now()), iso(now()))
	return err
}

func (s *Store) TouchDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET last_seen = ? WHERE id = ?`, iso(now()), id)
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
	for _, q := range []string{`DELETE FROM device_apps WHERE device_id = ?`, `DELETE FROM screensavers WHERE device_id = ?`} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET user_id = NULL, name = '', brightness = 100
		WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- screensavers ----

// Screensaver is what a board plays when it sits on its Apps drawer: screens
// of its installed apps, in order, each shown for IntervalSec.
type Screensaver struct {
	IntervalSec int
	Items       []SaverItem
}

// SaverItem is one screen in a screensaver: an app's screen, by their IDs
// (the built-in Clock is app "clock", screen "clock").
type SaverItem struct {
	App    string `json:"app"`
	Screen string `json:"screen"`
}

// Screensaver is the board's screensaver: none, with a 10-second interval,
// until one is saved.
func (s *Store) Screensaver(ctx context.Context, deviceID string) (*Screensaver, error) {
	sv := &Screensaver{IntervalSec: 10}
	var items string
	err := s.db.QueryRowContext(ctx, `SELECT interval_sec, items FROM screensavers WHERE device_id = ?`, deviceID).Scan(&sv.IntervalSec, &items)
	if errors.Is(err, sql.ErrNoRows) {
		return sv, nil
	}
	if err != nil {
		return nil, err
	}
	return sv, json.Unmarshal([]byte(items), &sv.Items)
}

func (s *Store) SaveScreensaver(ctx context.Context, deviceID string, sv *Screensaver) error {
	if sv.Items == nil {
		sv.Items = []SaverItem{}
	}
	items, err := json.Marshal(sv.Items)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO screensavers (device_id, interval_sec, items) VALUES (?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET interval_sec = excluded.interval_sec, items = excluded.items`,
		deviceID, sv.IntervalSec, string(items))
	return err
}

// ---- sources ----

type Source struct {
	ID            string
	UserID        string
	Name          string
	Kind          string // http | static | push
	Method        string
	URL           string
	Secret        string `json:"-"` // encrypted JSON {"auth": "...", "headers": "..."}
	Body          string
	IntervalSec   int
	PushToken     string
	LastData      string
	LastStatus    int
	LastError     string
	LastFetchedAt int64
	CreatedAt     int64
	PerUser       bool   // personal: everyone who installs an app using it sends their own data, to a copy of it
	OriginID      string // set on such a copy: the source it copies
	Example       string // personal: JSON website previews show people who haven't sent their own
}

const sourceCols = `id, user_id, name, kind, method, url, secret, body, interval_sec, push_token, last_data, last_status, last_error, last_fetched_at, created_at, per_user, origin_id, example`

const sourceSelect = `id, user_id, name, kind, method, url, secret, body, interval_sec, push_token, last_data, last_status, last_error,
	COALESCE(unixepoch(last_fetched_at), 0), unixepoch(created_at), per_user, COALESCE(origin_id, ''), example`

func scanSource(row interface{ Scan(...any) error }) (*Source, error) {
	x := &Source{}
	err := row.Scan(&x.ID, &x.UserID, &x.Name, &x.Kind, &x.Method, &x.URL, &x.Secret, &x.Body, &x.IntervalSec,
		&x.PushToken, &x.LastData, &x.LastStatus, &x.LastError, &x.LastFetchedAt, &x.CreatedAt, &x.PerUser, &x.OriginID, &x.Example)
	return x, notFound(err)
}

// orNull stores "" as NULL, for optional references.
func orNull(s string) any {
	if s == "" {
		return nil
	}
	return s
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
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceSelect+` FROM sources WHERE id = ?`, id))
}

// UserSource is one of the sources the user made. Copies of personal sources
// aren't among them: those belong to the apps they were installed with.
func (s *Store) UserSource(ctx context.Context, userID, id string) (*Source, error) {
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceSelect+` FROM sources WHERE id = ? AND user_id = ? AND origin_id IS NULL`, id, userID))
}

func (s *Store) SourceByPushToken(ctx context.Context, token string) (*Source, error) {
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceSelect+` FROM sources WHERE push_token = ? AND kind = 'push'`, token))
}

// UserSources lists the sources the user made (not copies, see UserSource).
func (s *Store) UserSources(ctx context.Context, userID string) ([]*Source, error) {
	return s.querySources(ctx, `SELECT `+sourceSelect+` FROM sources WHERE user_id = ? AND origin_id IS NULL ORDER BY name`, userID)
}

// SourceCopy is the user's own copy of a personal source.
func (s *Store) SourceCopy(ctx context.Context, originID, userID string) (*Source, error) {
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceSelect+` FROM sources WHERE origin_id = ? AND user_id = ?`, originID, userID))
}

// EnsureSourceCopy gives the user their own copy of a personal source, with
// its own push link, unless they have one already.
func (s *Store) EnsureSourceCopy(ctx context.Context, origin *Source, userID string) (*Source, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO sources (id, user_id, name, kind, push_token, origin_id, created_at)
		VALUES (?, ?, ?, 'push', ?, ?, ?)`, NewID(), userID, origin.Name, RandomToken(16), origin.ID, iso(now())); err != nil {
		return nil, err
	}
	return s.SourceCopy(ctx, origin.ID, userID)
}

// PersonalSources lists the personal sources an app's screens use.
func (s *Store) PersonalSources(ctx context.Context, appID string) ([]*Source, error) {
	return s.querySources(ctx, `SELECT `+sourceSelect+` FROM sources
		WHERE per_user = 1 AND id IN (SELECT source_id FROM screens WHERE app_id = ?) ORDER BY name`, appID)
}

// DueSources lists HTTP sources whose interval has elapsed.
func (s *Store) DueSources(ctx context.Context) ([]*Source, error) {
	return s.querySources(ctx, `SELECT `+sourceSelect+` FROM sources
		WHERE kind = 'http' AND url <> '' AND (last_fetched_at IS NULL OR unixepoch(last_fetched_at) + interval_sec <= ?)`, now())
}

func (s *Store) SaveSource(ctx context.Context, x *Source) error {
	if x.ID == "" {
		x.ID = NewID()
		x.CreatedAt = now()
		_, err := s.db.ExecContext(ctx, `INSERT INTO sources (`+sourceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			x.ID, x.UserID, x.Name, x.Kind, x.Method, x.URL, x.Secret, x.Body, x.IntervalSec, x.PushToken,
			x.LastData, x.LastStatus, x.LastError, iso(x.LastFetchedAt), iso(x.CreatedAt), x.PerUser, orNull(x.OriginID), x.Example)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sources SET name = ?, kind = ?, method = ?, url = ?, secret = ?, body = ?,
		interval_sec = ?, push_token = ?, last_data = ?, last_status = ?, last_error = ?, last_fetched_at = ?, per_user = ?, example = ?
		WHERE id = ? AND user_id = ?`,
		x.Name, x.Kind, x.Method, x.URL, x.Secret, x.Body, x.IntervalSec, x.PushToken, x.LastData, x.LastStatus,
		x.LastError, iso(x.LastFetchedAt), x.PerUser, x.Example, x.ID, x.UserID)
	return err
}

// SetSourceResult stores a fetch outcome. data is left untouched when empty
// so the last good data keeps showing while a source is failing.
func (s *Store) SetSourceResult(ctx context.Context, id, data string, status int, errText string) error {
	if data == "" {
		_, err := s.db.ExecContext(ctx, `UPDATE sources SET last_status = ?, last_error = ?, last_fetched_at = ? WHERE id = ?`,
			status, errText, iso(now()), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sources SET last_data = ?, last_status = ?, last_error = ?, last_fetched_at = ? WHERE id = ?`,
		data, status, errText, iso(now()), id)
	return err
}

func (s *Store) DeleteSource(ctx context.Context, userID, id string) error {
	// Its screens keep working without data: screens.source_id is ON DELETE SET NULL.
	_, err := s.db.ExecContext(ctx, `DELETE FROM sources WHERE id = ? AND user_id = ?`, id, userID)
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

const screenCols = `id, user_id, name, COALESCE(source_id, ''), layout, spec, unixepoch(updated_at), unixepoch(created_at), COALESCE(app_id, '')`

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
			x.ID, x.UserID, x.Name, src, x.Layout, x.Spec, iso(x.UpdatedAt), iso(x.CreatedAt), app, x.AppID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE screens SET name = ?, source_id = ?, layout = ?, spec = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, x.Name, src, x.Layout, x.Spec, iso(x.UpdatedAt), x.ID, x.UserID)
	return err
}

func (s *Store) DeleteScreen(ctx context.Context, userID, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM screens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// SetScreenOrder puts an app's screens in the order of ids.
func (s *Store) SetScreenOrder(ctx context.Context, appID string, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE screens SET app_pos = ? WHERE id = ? AND app_id = ?`, i, id, appID); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	Description string // shown in the App Store
	PublishedAt int64  // 0 unless anyone can install it
	MadeBy      string // the AI client that made it over MCP ("Claude"); "" if made on the website
	Setup       string // how to set it up, shown on its App Store page
}

const appCols = `id, user_id, name, cycle_sec, updated_at, created_at, description, published_at, made_by, setup`

const appSelect = `a.id, a.user_id, a.name, a.cycle_sec, unixepoch(a.updated_at), unixepoch(a.created_at), a.description,
	COALESCE(unixepoch(a.published_at), 0), a.made_by, a.setup`

// appDest is where a row selected with appSelect scans to.
func appDest(a *App) []any {
	return []any{&a.ID, &a.UserID, &a.Name, &a.CycleSec, &a.UpdatedAt, &a.CreatedAt, &a.Description, &a.PublishedAt, &a.MadeBy, &a.Setup}
}

func scanApp(row interface{ Scan(...any) error }) (*App, error) {
	a := &App{}
	err := row.Scan(appDest(a)...)
	return a, notFound(err)
}

func (s *Store) UserApp(ctx context.Context, userID, id string) (*App, error) {
	return scanApp(s.db.QueryRowContext(ctx, `SELECT `+appSelect+` FROM apps a WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) UserApps(ctx context.Context, userID string) ([]*App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appSelect+` FROM apps a WHERE user_id = ? ORDER BY created_at`, userID)
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
		_, err := s.db.ExecContext(ctx, `INSERT INTO apps (`+appCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.UserID, a.Name, a.CycleSec, iso(a.UpdatedAt), iso(a.CreatedAt), a.Description, iso(a.PublishedAt), a.MadeBy, a.Setup)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET name = ?, cycle_sec = ?, updated_at = ?, description = ?, published_at = ?, setup = ?
		WHERE id = ? AND user_id = ?`, a.Name, a.CycleSec, iso(a.UpdatedAt), a.Description, iso(a.PublishedAt), a.Setup, a.ID, a.UserID)
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
	rows, err := s.db.QueryContext(ctx, `SELECT `+appSelect+`, COALESCE(u.username, '') FROM apps a JOIN users u ON u.id = a.user_id WHERE a.published_at IS NOT NULL
		ORDER BY a.published_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishedApp
	for rows.Next() {
		p := PublishedApp{App: &App{}}
		if err := rows.Scan(append(appDest(p.App), &p.Publisher)...); err != nil {
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
	err = s.db.QueryRowContext(ctx, `SELECT `+appSelect+`, CASE WHEN a.user_id = ? THEN '' ELSE COALESCE(u.username, '') END
		FROM apps a JOIN users u ON u.id = a.user_id WHERE a.id = ? AND (a.user_id = ? OR a.published_at IS NOT NULL)`, userID, id, userID).
		Scan(append(appDest(a), &publisher)...)
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
	if _, err := tx.ExecContext(ctx, `UPDATE apps SET published_at = NULL WHERE id = ? AND user_id = ?`, id, userID); err != nil {
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
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO app_installs (app_id, device_id, at) VALUES (?, ?, ?)`, appID, deviceID, iso(now()))
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
	return s.devicesForScreens(ctx, "", screenIDs)
}

// UserDevicesForScreens is DevicesForScreens for one user's devices.
func (s *Store) UserDevicesForScreens(ctx context.Context, userID string, screenIDs []string) ([]string, error) {
	return s.devicesForScreens(ctx, userID, screenIDs)
}

// devicesForScreens is DevicesForScreens, for every user's devices when userID is "".
func (s *Store) devicesForScreens(ctx context.Context, userID string, screenIDs []string) ([]string, error) {
	if len(screenIDs) == 0 {
		return nil, nil
	}
	args := []any{userID, userID}
	for _, id := range screenIDs {
		args = append(args, id)
	}
	return s.queryIDs(ctx, `SELECT DISTINCT da.device_id FROM device_apps da JOIN screens s ON s.app_id = da.app_id
		JOIN devices d ON d.id = da.device_id WHERE (? = '' OR d.user_id = ?)
		AND s.id IN (?`+strings.Repeat(",?", len(screenIDs)-1)+`)`, args...)
}
