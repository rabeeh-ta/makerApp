package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"makerapp/internal/store"
)

// testStore opens a fresh database with the real migrations.
func testStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return store.New(db), db
}

func TestMigrationsRunOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for range 2 {
		db, err := openDB(path)
		if err != nil {
			t.Fatal(err)
		}
		var v int
		if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != len(migrations) {
			t.Fatalf("user_version = %d, %v; want %d", v, err, len(migrations))
		}
		db.Close()
	}
}

// TestTimes covers the queries that compare stored times: they're text in the
// database and Unix seconds (0 = never) in Go.
func TestTimes(t *testing.T) {
	ctx := context.Background()
	st, db := testStore(t)
	start := time.Now().Unix()

	u, err := st.CreateUser(ctx, "me@example.com", "x")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.UserByEmail(ctx, u.Email); err != nil || got.CreatedAt < start {
		t.Fatalf("user created_at = %d, %v", got.CreatedAt, err)
	}

	live, _ := st.CreateSession(ctx, u.ID, time.Hour)
	expired, _ := st.CreateSession(ctx, u.ID, -time.Second)
	if _, err := st.UserBySession(ctx, live); err != nil {
		t.Fatalf("live session: %v", err)
	}
	if _, err := st.UserBySession(ctx, expired); err != store.ErrNotFound {
		t.Fatalf("expired session: %v", err)
	}

	goodClaim, _ := st.CreateClaim(ctx, u.ID, time.Hour)
	oldClaim, _ := st.CreateClaim(ctx, u.ID, -time.Second)
	if owner, err := st.UseClaim(ctx, goodClaim, "AABBCCDDEEFF"); err != nil || owner != u.ID {
		t.Fatalf("claim: %q %v", owner, err)
	}
	if _, err := st.UseClaim(ctx, oldClaim, "AABBCCDDEEFF"); err != store.ErrNotFound {
		t.Fatalf("expired claim: %v", err)
	}

	if err := st.UpsertDeviceHello(ctx, &store.Device{ID: "AABBCCDDEEFF", TokenHash: "h", PairCode: "123456"}); err != nil {
		t.Fatal(err)
	}
	d, err := st.UnclaimedDeviceByCode(ctx, "123456", time.Now().Add(-15*time.Minute))
	if err != nil || d.LastSeen < start || d.CreatedAt < start {
		t.Fatalf("pairing within the window: %+v %v", d, err)
	}
	if _, err := st.UnclaimedDeviceByCode(ctx, "123456", time.Now().Add(time.Minute)); err != store.ErrNotFound {
		t.Fatalf("pairing after the window: %v", err)
	}

	src := &store.Source{UserID: u.ID, Name: "s", Kind: "http", Method: "GET", URL: "https://example.com", IntervalSec: 60}
	if err := st.SaveSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	due := func() bool {
		t.Helper()
		list, err := st.DueSources(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(list) == 1 && list[0].ID == src.ID
	}
	if !due() {
		t.Fatal("a never-fetched source should be due")
	}
	if err := st.SetSourceResult(ctx, src.ID, `{"a":1}`, 200, ""); err != nil {
		t.Fatal(err)
	}
	if due() {
		t.Fatal("a source fetched just now isn't due")
	}
	if got, _ := st.Source(ctx, src.ID); got.LastFetchedAt < start {
		t.Fatalf("last_fetched_at = %d", got.LastFetchedAt)
	}
	if _, err := db.Exec(`UPDATE sources SET last_fetched_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Minute).UTC().Format("2006-01-02T15:04:05Z"), src.ID); err != nil {
		t.Fatal(err)
	}
	if !due() {
		t.Fatal("a source fetched two minutes ago with a 60 s interval is due")
	}

	a := &store.App{UserID: u.ID, Name: "Weather"}
	if err := st.SaveApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.UserApp(ctx, u.ID, a.ID); got.PublishedAt != 0 || got.CreatedAt < start {
		t.Fatalf("new app: %+v", got)
	}
	a.PublishedAt = 1767225600 // 2026-01-01T00:00:00Z
	if err := st.SaveApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT published_at FROM apps WHERE id = ?`, a.ID).Scan(&stored); err != nil || stored != "2026-01-01T00:00:00Z" {
		t.Fatalf("published_at stored as %q, %v", stored, err)
	}
	if got, _ := st.UserApp(ctx, u.ID, a.ID); got.PublishedAt != a.PublishedAt {
		t.Fatalf("published_at read back as %d", got.PublishedAt)
	}
}

func TestPublishing(t *testing.T) {
	ctx := context.Background()
	st, db := testStore(t)
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES ('pub', 'p@x', 'x', '2026-01-01T00:00:00Z'), ('fan', 'f@x', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO devices (id, user_id, name, token_hash, created_at) VALUES ('D1', 'pub', 'Mine', 'h', '2026-01-01T00:00:00Z'), ('D2', 'fan', 'Theirs', 'h', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetUsername(ctx, "pub", "maker"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUsername(ctx, "fan", "maker"); err != store.ErrTaken {
		t.Fatalf("duplicate username: %v", err)
	}

	a := &store.App{UserID: "pub", Name: "Weather"}
	if err := st.SaveApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.InstallableApp(ctx, "fan", a.ID); err != store.ErrNotFound {
		t.Fatalf("unpublished app installable by others: %v", err)
	}
	a.PublishedAt, a.Description = 1, "Rain or shine"
	if err := st.SaveApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, by, err := st.InstallableApp(ctx, "fan", a.ID); err != nil || by != "maker" {
		t.Fatalf("published app for others: %q %v", by, err)
	}
	if _, by, _ := st.InstallableApp(ctx, "pub", a.ID); by != "" {
		t.Fatalf("own app shows a publisher: %q", by)
	}
	if list, _ := st.PublishedApps(ctx); len(list) != 1 || list[0].Publisher != "maker" || list[0].Description != "Rain or shine" {
		t.Fatalf("published list: %+v", list)
	}

	for _, d := range []string{"D1", "D2", "D2"} {
		if err := st.InstallApp(ctx, d, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if counts, _ := st.InstallCounts(ctx); counts[a.ID] != 2 {
		t.Fatalf("installs = %d, want 2 boards", counts[a.ID])
	}

	gone, err := st.Unpublish(ctx, "pub", a.ID)
	if err != nil || len(gone) != 1 || gone[0] != "D2" {
		t.Fatalf("unpublish touched %v, %v", gone, err)
	}
	if _, _, err := st.InstallableApp(ctx, "fan", a.ID); err != store.ErrNotFound {
		t.Fatalf("unpublished app still installable by others: %v", err)
	}
	if ids, _ := st.DeviceAppIDs(ctx, "D1"); len(ids) != 1 {
		t.Fatal("the publisher's own board lost the app")
	}
	if ids, _ := st.DeviceAppIDs(ctx, "D2"); len(ids) != 0 {
		t.Fatal("unpublished app still on someone else's board")
	}
	if counts, _ := st.InstallCounts(ctx); counts[a.ID] != 2 {
		t.Fatal("the install count should remember past installs")
	}
}
