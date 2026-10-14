package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"esphog/internal/engine"
	"esphog/internal/fetch"
	"esphog/internal/games"
	"esphog/internal/secret"
	"esphog/internal/store"
	"esphog/internal/web"
)

// TestDeviceFlow walks the whole path a real board takes: sign up, flash
// (claim token in the config image), connect with no apps, install apps,
// and live updates from a push source.
func TestDeviceFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	box, _ := secret.New("test")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(st, box, fetch.New(true), time.UTC, log)
	site, err := web.New(web.Config{AllowSignup: true}, st, eng, log)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(site.Handler())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	post := func(path string, form url.Values) *http.Response {
		t.Helper()
		resp, err := client.PostForm(srv.URL+path, form)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	get := func(path string) string {
		t.Helper()
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: %d %s", path, resp.StatusCode, b)
		}
		return string(b)
	}

	post("/signup", url.Values{"email": {"me@example.com"}, "password": {"password123"}})

	// Flash page mints a claim; config image carries it plus the server URL.
	if !strings.Contains(get("/flash"), "ESP32-S3 Rev TFT Feather") {
		t.Fatal("board picker missing")
	}
	page := get("/flash?board=feather-s3-rtft")
	claim := regexp.MustCompile(`claim=([0-9a-f]{32})`).FindStringSubmatch(page)
	if claim == nil {
		t.Fatal("no claim on flash page")
	}
	cfg := get("/flash/config.bin?claim=" + claim[1])
	if !strings.HasPrefix(cfg, "HOGCFG1\x00") {
		t.Fatal("bad config image header")
	}
	n := binary.LittleEndian.Uint16([]byte(cfg[8:10]))
	var cfgJSON struct{ Server, Claim string }
	if err := json.Unmarshal([]byte(cfg[10:10+n]), &cfgJSON); err != nil || cfgJSON.Claim != claim[1] {
		t.Fatalf("config json: %v %+v", err, cfgJSON)
	}
	if !strings.HasSuffix(cfgJSON.Server, "/ws") {
		t.Fatalf("server url %q", cfgJSON.Server)
	}

	// A device connects with the claim and is linked to the account at once.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	// Arduino's WebSocket client sends a placeholder Origin; it must be accepted.
	dev, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"file://"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer dev.CloseNow()
	send := func(v any) {
		b, _ := json.Marshal(v)
		if err := dev.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
	recv := func(wantType string) map[string]any {
		t.Helper()
		for {
			rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
			_, b, err := dev.Read(rctx)
			rcancel()
			if err != nil {
				t.Fatalf("waiting for %s: %v", wantType, err)
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			if m["type"] == wantType {
				return m
			}
		}
	}
	send(map[string]any{"type": "hello", "id": "AABBCCDDEEFF", "token": "0123456789abcdef0123", "code": "123456",
		"fw": "2.0.0", "claim": claim[1]})
	state := recv("state")
	if state["name"] != "makerDevice EEFF" {
		t.Fatalf("state name %v", state["name"])
	}
	if apps := state["apps"].([]any); len(apps) != 0 {
		t.Fatalf("a new board should have no apps, got %v", apps)
	}
	if !strings.Contains(get("/flash/status?claim="+claim[1]), "is connected") {
		t.Fatal("flash status did not report the device")
	}
	appsIn := func(state map[string]any) []map[string]any {
		var out []map[string]any
		for _, a := range state["apps"].([]any) {
			out = append(out, a.(map[string]any))
		}
		return out
	}

	// A push source, and an app made from the device page (installed there at once).
	post("/sources/new", url.Values{"name": {"Deploys"}, "kind": {"push"}, "interval_sec": {"300"}})
	srcs, _ := st.UserSources(ctx, mustUser(t, st).ID)
	src := srcs[0]
	post("/apps/new", url.Values{"name": {"Ops"}, "device": {"AABBCCDDEEFF"}})
	apps, _ := st.UserApps(ctx, mustUser(t, st).ID)
	if len(apps) != 1 || apps[0].Name != "Ops" {
		t.Fatalf("app not created: %+v", apps)
	}
	appID := apps[0].ID
	state = recv("state")
	if got := appsIn(state); len(got) != 1 || got[0]["name"] != "Ops" || len(got[0]["screens"].([]any)) != 1 {
		t.Fatalf("empty app should arrive with a placeholder screen: %v", got)
	}

	// A screen in the app, bound to the source.
	resp := post("/screens/new", url.Values{
		"app":  {appID},
		"name": {"Deploys"}, "source_id": {src.ID}, "layout": {"big_number"},
		"title": {"Deploys"}, "value": {"{{ count | num }}"}, "sub": {"{{ delta | signed }} today"}, "sub_color": {"auto"},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("save screen: %d", resp.StatusCode)
	}
	screens, _ := st.UserScreens(ctx, mustUser(t, st).ID)
	if len(screens) != 1 || screens[0].AppID != appID || !strings.Contains(get("/apps/"+appID), "Deploys") {
		t.Fatalf("screen not in the app: %+v", screens)
	}
	state = recv("state")
	if got := appsIn(state); len(got) != 1 || got[0]["screens"].([]any)[0].(map[string]any)["title"] != "Deploys" {
		t.Fatalf("app screens %v", got)
	}

	// The built-in Clock installs from the device page and joins the end of the drawer.
	if page := get("/devices/AABBCCDDEEFF"); !strings.Contains(page, "Clock") || !strings.Contains(page, "Apps data sent to the board") {
		t.Fatal("device page should offer the Clock and show the apps data size")
	}
	post("/apps/clock/install", url.Values{"device": {"AABBCCDDEEFF"}})
	state = recv("state")
	got := appsIn(state)
	if len(got) != 2 || got[1]["id"] != "clock" || got[1]["screens"].([]any)[0].(map[string]any)["layout"] != "clock" {
		t.Fatalf("clock not installed: %v", got)
	}

	// Games need newer firmware: on this 2.0 board they wait for the update.
	post("/apps/paddle/install", url.Values{"device": {"AABBCCDDEEFF"}})
	state = recv("state")
	if got := appsIn(state); len(got) != 2 {
		t.Fatalf("paddle should be held back on firmware 2.0: %v", got)
	}
	if !strings.Contains(get("/devices/AABBCCDDEEFF"), "firmware update to 3.0") {
		t.Fatal("device page doesn't explain the held-back game")
	}

	// Pushing data updates the device right away.
	pr, err := http.Post(srv.URL+"/push/"+src.PushToken, "application/json", strings.NewReader(`{"count": 12345, "delta": 3}`))
	if err != nil || pr.StatusCode != 200 {
		t.Fatalf("push: %v %v", err, pr.StatusCode)
	}
	state = recv("state")
	s0 := appsIn(state)[0]["screens"].([]any)[0].(map[string]any)
	if s0["value"] != "12,345" || s0["sub"] != "+3 today" || s0["subColor"] != "#2BB673" {
		t.Fatalf("rendered screen %v", s0)
	}

	// Uninstalling takes it off the board; the app itself stays in the library.
	post("/apps/"+appID+"/uninstall", url.Values{"device": {"AABBCCDDEEFF"}})
	state = recv("state")
	if got := appsIn(state); len(got) != 1 || got[0]["id"] != "clock" {
		t.Fatalf("after uninstall: %v", got)
	}
	if page := get("/apps"); !strings.Contains(page, "Ops") || !strings.Contains(page, "Hog Invaders") {
		t.Fatal("library should list the uninstalled app and the built-in games")
	}

	// Publishing: pick a public name once, then the app is in everyone's list
	// with its install count (the board that had it installed counts).
	post("/apps/"+appID+"/publish", url.Values{"username": {"Tester"}, "description": {"Deploys today"}})
	if page := get("/apps"); !strings.Contains(page, "@tester") || !strings.Contains(page, "Deploys today") ||
		!strings.Contains(page, "1 install") {
		t.Fatal("published app missing from All apps, or without its publisher and installs")
	}
	if u := mustUser(t, st); u.Username != "tester" {
		t.Fatalf("username %q", u.Username)
	}

	// Boards download games by ID and version; a stale version is gone.
	if script := get("/games/snake/" + games.Hash("snake") + ".lua"); !strings.Contains(script, "function update") {
		t.Fatalf("game script: %.80s", script)
	}
	if r, _ := client.Get(srv.URL + "/games/snake/000000000000.lua"); r.StatusCode != 404 {
		t.Fatalf("old game version: %d", r.StatusCode)
	}

	// Another device with the same ID but a different secret is refused.
	evil, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"type": "hello", "id": "AABBCCDDEEFF", "token": "ffffffffffffffffffff", "code": "999999"})
	_ = evil.Write(ctx, websocket.MessageText, b)
	_, reply, _ := evil.Read(ctx)
	if !strings.Contains(string(reply), `"error"`) {
		t.Fatalf("impostor not rejected: %s", reply)
	}
	evil.CloseNow()
}

func mustUser(t *testing.T, st *store.Store) *store.User {
	u, err := st.UserByEmail(context.Background(), "me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return u
}
