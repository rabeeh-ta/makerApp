package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

// TestMigrateToApps turns the older layout (screens owned by a device, a flow
// with a clock home and a menu) into installed apps.
func TestMigrateToApps(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES ('u1', 'a@b.c', 'x', 1)`,
		`INSERT INTO devices (id, user_id, name, token_hash, created_at, flow_id) VALUES ('D1', 'u1', 'Desk', 'h', 1, 'f1')`,
		`INSERT INTO screens (id, user_id, name, layout, updated_at, created_at, device_id) VALUES
			('s1', 'u1', 'Deals', 'list', 1, 1, 'D1'), ('s2', 'u1', 'MRR', 'big_number', 1, 2, 'D1'),
			('s3', 'u1', 'Status', 'text', 1, 3, 'D1'), ('s4', 'u1', 'Loose', 'text', 1, 4, NULL)`,
		`INSERT INTO device_screens (device_id, screen_id, pos) VALUES ('D1', 's3', 0), ('D1', 's1', 1), ('D1', 's2', 2)`,
		`INSERT INTO flows (id, user_id, name, spec, updated_at, created_at, device_id) VALUES ('f1', 'u1', 'Flow',
			'{"template":{"home":"clock","apps":[{"name":"Sales","screens":["s2","s1","gone"]}]}}', 1, 1, 'D1')`,
		`DELETE FROM meta WHERE key = 'apps_migrated'`,
	} {
		if _, err := st.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	st.Close()

	st, err = Open(path) // runs the migration
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	installed, _ := st.DeviceAppIDs(ctx, "D1")
	if len(installed) != 3 || installed[0] != "clock" {
		t.Fatalf("installed = %v, want clock, Sales, Screens", installed)
	}
	wantApp := func(id, name string, screens ...string) {
		t.Helper()
		a, err := st.UserApp(ctx, "u1", id)
		if err != nil || a.Name != name {
			t.Fatalf("app %s = %+v, %v; want %q", id, a, err, name)
		}
		list, _ := st.AppScreens(ctx, id)
		var ids []string
		for _, sc := range list {
			ids = append(ids, sc.ID)
		}
		if !slices.Equal(ids, screens) {
			t.Fatalf("%s screens = %v, want %v", name, ids, screens)
		}
	}
	wantApp(installed[1], "Sales", "s2", "s1")
	wantApp(installed[2], "Screens", "s3")

	// The screen that was never on a device lands in its own app, not installed.
	apps, _ := st.UserApps(ctx, "u1")
	if len(apps) != 3 {
		t.Fatalf("apps = %d, want 3", len(apps))
	}
	for _, a := range apps {
		if !slices.Contains(installed, a.ID) {
			wantApp(a.ID, "Screens", "s4")
		}
	}

	// Running again changes nothing.
	st.Close()
	st, _ = Open(path)
	if again, _ := st.UserApps(ctx, "u1"); len(again) != 3 {
		t.Fatalf("migration ran twice: %d apps", len(again))
	}
}

// TestPublishing: someone else can install an app once it's published; the
// install counter remembers every board; unpublishing takes it off other
// people's boards but not the publisher's.
func TestPublishing(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "pub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES ('pub', 'p@x', 'x', 1), ('fan', 'f@x', 'x', 1)`,
		`INSERT INTO devices (id, user_id, name, token_hash, created_at) VALUES ('D1', 'pub', 'Mine', 'h', 1), ('D2', 'fan', 'Theirs', 'h', 1)`,
	} {
		if _, err := st.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetUsername(ctx, "pub", "maker"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUsername(ctx, "fan", "maker"); err != ErrTaken {
		t.Fatalf("duplicate username: %v", err)
	}

	a := &App{UserID: "pub", Name: "Weather"}
	if err := st.SaveApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.InstallableApp(ctx, "fan", a.ID); err != ErrNotFound {
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
