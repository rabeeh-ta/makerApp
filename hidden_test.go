package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/library"
	"makerapp/internal/secret"
	"makerapp/internal/store"
	"makerapp/internal/web"
)

func TestHiddenScreensStayOffBoards(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	st := store.New(db)
	box, _ := secret.New("test")
	eng := engine.New(st, box, fetch.New(true), time.UTC, slog.New(slog.NewTextHandler(io.Discard, nil)))

	u, err := st.CreateUser(ctx, "me@example.com", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDeviceHello(ctx, &store.Device{ID: "B1", Name: "board"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimDeviceFor(ctx, "B1", u.ID, "board"); err != nil {
		t.Fatal(err)
	}
	a := &store.App{UserID: u.ID, Name: "App"}
	if err := st.SaveApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, name := range []string{"One", "Two"} {
		sc := &store.Screen{UserID: u.ID, AppID: a.ID, Name: name, Layout: "text", Spec: `{"title":"` + name + `"}`}
		if err := st.SaveScreen(ctx, sc); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sc.ID)
	}
	if err := st.InstallApp(ctx, "B1", a.ID); err != nil {
		t.Fatal(err)
	}
	shown := func() []string {
		dev, _ := st.Device(ctx, "B1")
		var names []string
		for _, v := range eng.DeviceApps(ctx, dev, false) {
			for _, sc := range v.Screens {
				names = append(names, sc.Name)
			}
		}
		return names
	}
	if got := shown(); len(got) != 2 {
		t.Fatalf("before hiding: %v", got)
	}
	// The Hide button on the app page.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(web.New(web.Config{}, st, eng, library.New(st, eng), log).Handler())
	defer srv.Close()
	token, _ := st.CreateSession(ctx, u.ID, time.Hour)
	call := func(method, path string, form url.Values) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "makerapp_session", Value: token})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	hide := func(screenID, v string) int {
		resp := call("POST", "/api/apps/"+a.ID+"/screens/"+screenID+"/hide", url.Values{"hidden": {v}})
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := hide("nope", "1"); code != http.StatusNotFound {
		t.Fatalf("hiding a screen that isn't there: %d", code)
	}
	if code := hide(ids[0], "1"); code != http.StatusOK {
		t.Fatalf("hide: %d", code)
	}
	resp := call("GET", "/api/apps/"+a.ID, nil)
	var page struct{ Screens []struct{ Hidden bool } }
	json.NewDecoder(resp.Body).Decode(&page)
	resp.Body.Close()
	if len(page.Screens) != 2 || !page.Screens[0].Hidden || page.Screens[1].Hidden {
		t.Fatalf("app page: %+v", page.Screens)
	}
	if got := shown(); len(got) != 1 || got[0] != "Two" {
		t.Fatalf("after hiding One: %v", got)
	}
	if screens, _ := st.AppScreens(ctx, a.ID); len(screens) != 2 || !screens[0].Hidden {
		t.Fatal("the hidden screen should stay in the app")
	}
	if err := st.SetScreenHidden(ctx, u.ID, ids[0], false); err != nil {
		t.Fatal(err)
	}
	if got := shown(); len(got) != 2 {
		t.Fatalf("after showing again: %v", got)
	}
}
