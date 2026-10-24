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
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/games"
	"makerapp/internal/secret"
	"makerapp/internal/store"
	"makerapp/internal/web"
)

// TestDeviceFlow walks the whole path a real board takes: sign up, flash
// (claim token in the config image), connect with no apps, install apps,
// and live updates from a push source. The website's side goes through the
// JSON API.
func TestDeviceFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := store.New(db)
	box, _ := secret.New("test")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(st, box, fetch.New(true), time.UTC, log)
	site := web.New(web.Config{AllowSignup: true}, st, eng, log)
	srv := httptest.NewServer(site.Handler())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	// call sends a form to the API (GET when form is nil) and decodes the JSON reply.
	call := func(path string, form url.Values, out any) int {
		t.Helper()
		var resp *http.Response
		var err error
		if form == nil {
			resp, err = client.Get(srv.URL + path)
		} else {
			resp, err = client.PostForm(srv.URL+path, form)
		}
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") && out != nil {
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("%s: %v: %s", path, err, b)
			}
		}
		return resp.StatusCode
	}
	// post expects a form post that worked, and returns the page it sends the website to.
	post := func(path string, form url.Values) string {
		t.Helper()
		var r struct{ Redirect, Error string }
		if code := call(path, form, &r); code != 200 || r.Error != "" {
			t.Fatalf("POST %s: %d %q", path, code, r.Error)
		}
		return r.Redirect
	}
	raw := func(path string) string {
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

	if code := call("/api/devices", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d, want 401", code)
	}
	if to := post("/api/signup", url.Values{"email": {"me@example.com"}, "password": {"password123"}}); to != "/flash" {
		t.Fatalf("after signup: %q", to)
	}
	var me struct {
		User        struct{ Email string }
		AllowSignup bool
	}
	if call("/api/me", nil, &me); me.User.Email != "me@example.com" || !me.AllowSignup {
		t.Fatalf("me: %+v", me)
	}

	// The flash page makes a claim; the config image carries it plus the server URL.
	var flash struct{ Boards []struct{ ID, Name string } }
	if call("/api/flash", nil, &flash); len(flash.Boards) == 0 || flash.Boards[0].Name != "ESP32-S3 Rev TFT Feather" {
		t.Fatalf("board picker: %+v", flash)
	}
	var claim struct{ Claim, ManifestURL string }
	if call("/api/flash/claims", url.Values{}, &claim); len(claim.Claim) != 32 || !strings.Contains(claim.ManifestURL, claim.Claim) {
		t.Fatalf("claim: %+v", claim)
	}
	cfg := raw("/flash/config.bin?claim=" + claim.Claim)
	if !strings.HasPrefix(cfg, "HOGCFG1\x00") {
		t.Fatal("bad config image header")
	}
	n := binary.LittleEndian.Uint16([]byte(cfg[8:10]))
	var cfgJSON struct{ Server, Claim string }
	if err := json.Unmarshal([]byte(cfg[10:10+n]), &cfgJSON); err != nil || cfgJSON.Claim != claim.Claim {
		t.Fatalf("config json: %v %+v", err, cfgJSON)
	}
	if !strings.HasSuffix(cfgJSON.Server, "/ws") {
		t.Fatalf("server url %q", cfgJSON.Server)
	}

	// A device connects with the claim and is linked to the account at once.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	// Arduino's WebSocket client sends a placeholder Origin; it must be accepted.
	dev, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"file://"}},
		Subprotocols: []string{"makerdevice"}})
	if err != nil {
		t.Fatal(err)
	}
	defer dev.CloseNow()
	if dev.Subprotocol() != "makerdevice" {
		t.Fatalf("subprotocol %q", dev.Subprotocol())
	}
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
		"fw": "2.0.0", "claim": claim.Claim})
	state := recv("state")
	if state["name"] != "makerDevice EEFF" {
		t.Fatalf("state name %v", state["name"])
	}
	if apps := state["apps"].([]any); len(apps) != 0 {
		t.Fatalf("a new board should have no apps, got %v", apps)
	}
	var status struct{ Device *struct{ ID, Name string } }
	if call("/api/flash/status?claim="+claim.Claim, nil, &status); status.Device == nil || status.Device.Name != "makerDevice EEFF" {
		t.Fatalf("flash status did not report the device: %+v", status)
	}
	appsIn := func(state map[string]any) []map[string]any {
		var out []map[string]any
		for _, a := range state["apps"].([]any) {
			out = append(out, a.(map[string]any))
		}
		return out
	}

	// The API never sends secrets: no password hash, device secret or pair code.
	for _, path := range []string{"/api/devices", "/api/devices/AABBCCDDEEFF", "/api/me"} {
		if body := raw(path); strings.Contains(body, "TokenHash") || strings.Contains(body, "PairCode") ||
			strings.Contains(body, "PasswordHash") || strings.Contains(body, "123456") {
			t.Fatalf("%s leaks a secret: %s", path, body)
		}
	}

	// A push source, and an app made from the device page (installed there at once).
	post("/api/sources", url.Values{"name": {"Deploys"}, "kind": {"push"}, "interval_sec": {"300"}})
	srcs, _ := st.UserSources(ctx, mustUser(t, st).ID)
	src := srcs[0]
	post("/api/apps", url.Values{"name": {"Ops"}, "device": {"AABBCCDDEEFF"}})
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
	to := post("/api/screens", url.Values{
		"app":  {appID},
		"name": {"Deploys"}, "source_id": {src.ID}, "layout": {"big_number"},
		"title": {"Deploys"}, "value": {"{{ count | num }}"}, "sub": {"{{ delta | signed }} today"}, "sub_color": {"auto"},
	})
	if !strings.HasPrefix(to, "/screens/") {
		t.Fatalf("after saving a screen: %q", to)
	}
	var appPage struct{ Screens []struct{ S struct{ Name string } } }
	call("/api/apps/"+appID, nil, &appPage)
	if len(appPage.Screens) != 1 || appPage.Screens[0].S.Name != "Deploys" {
		t.Fatalf("screen not in the app: %+v", appPage)
	}
	state = recv("state")
	if got := appsIn(state); len(got) != 1 || got[0]["screens"].([]any)[0].(map[string]any)["title"] != "Deploys" {
		t.Fatalf("app screens %v", got)
	}

	// The built-in Clock installs from the device page and joins the end of the drawer.
	type devicePage struct {
		DataKB    string
		Choices   []struct{ ID, Name string }
		Installed []struct{ ID, NeedsFW string }
	}
	var dp devicePage
	call("/api/devices/AABBCCDDEEFF", nil, &dp)
	hasClock := false
	for _, c := range dp.Choices {
		hasClock = hasClock || c.Name == "Clock"
	}
	if !hasClock || dp.DataKB == "" {
		t.Fatalf("device page should offer the Clock and show the apps data size: %+v", dp)
	}
	post("/api/apps/clock/install", url.Values{"device": {"AABBCCDDEEFF"}})
	state = recv("state")
	got := appsIn(state)
	if len(got) != 2 || got[1]["id"] != "clock" || got[1]["screens"].([]any)[0].(map[string]any)["layout"] != "clock" {
		t.Fatalf("clock not installed: %v", got)
	}

	// Games need newer firmware: on this 2.0 board they wait for the update.
	post("/api/apps/paddle/install", url.Values{"device": {"AABBCCDDEEFF"}})
	state = recv("state")
	if got := appsIn(state); len(got) != 2 {
		t.Fatalf("paddle should be held back on firmware 2.0: %v", got)
	}
	dp = devicePage{}
	call("/api/devices/AABBCCDDEEFF", nil, &dp)
	if last := dp.Installed[len(dp.Installed)-1]; last.ID != "paddle" || last.NeedsFW != "3.0" {
		t.Fatalf("device page doesn't explain the held-back game: %+v", dp.Installed)
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
	if to := post("/api/apps/"+appID+"/uninstall", url.Values{"device": {"AABBCCDDEEFF"}, "back": {"/apps"}}); to != "/apps" {
		t.Fatalf("uninstall goes back to %q", to)
	}
	state = recv("state")
	if got := appsIn(state); len(got) != 1 || got[0]["id"] != "clock" {
		t.Fatalf("after uninstall: %v", got)
	}
	type appsPage struct {
		Apps   []struct{ Name string }
		Public []struct {
			ID, Name, Desc, By string
			Installs           int
		}
	}
	var ap appsPage
	call("/api/apps", nil, &ap)
	hasGame := false
	for _, l := range ap.Public {
		hasGame = hasGame || l.Name == "Hog Invaders"
	}
	if len(ap.Apps) != 1 || ap.Apps[0].Name != "Ops" || !hasGame {
		t.Fatalf("library should list the uninstalled app and the built-in games: %+v", ap)
	}

	// Publishing: pick a public name once, then the app is in everyone's list
	// with its install count (the board that had it installed counts).
	var pubErr struct{ Error string }
	if code := call("/api/apps/"+appID+"/publish", url.Values{"username": {"x"}, "description": {"Deploys today"}}, &pubErr); code != 400 || pubErr.Error == "" {
		t.Fatalf("a too-short public name should be refused: %d %+v", code, pubErr)
	}
	post("/api/apps/"+appID+"/publish", url.Values{"username": {"Tester"}, "description": {"Deploys today"}})
	ap = appsPage{}
	call("/api/apps", nil, &ap)
	published := false
	for _, l := range ap.Public {
		published = published || (l.ID == appID && l.By == "tester" && l.Desc == "Deploys today" && l.Installs == 1)
	}
	if !published {
		t.Fatalf("published app missing from All apps, or without its publisher and installs: %+v", ap.Public)
	}
	if u := mustUser(t, st); u.Username != "tester" {
		t.Fatalf("username %q", u.Username)
	}

	// Boards download games by ID and version; a stale version is gone.
	if script := raw("/games/snake/" + games.Hash("snake") + ".lua"); !strings.Contains(script, "function update") {
		t.Fatalf("game script: %.80s", script)
	}
	if r, _ := client.Get(srv.URL + "/games/snake/000000000000.lua"); r.StatusCode != 404 {
		t.Fatalf("old game version: %d", r.StatusCode)
	}

	// Cross-site posts are blocked; unknown API paths answer 404 in JSON.
	req, _ := http.NewRequest("POST", srv.URL+"/api/apps", strings.NewReader("name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	if r, _ := client.Do(req); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", r.StatusCode)
	}
	var missing struct{ Error string }
	if code := call("/api/nope", nil, &missing); code != 404 || missing.Error == "" {
		t.Fatalf("unknown API path: %d %+v", code, missing)
	}

	// Boards that connect without a claim show a 6-digit code: one pairs from
	// the link on its screen, another by typing the code.
	unpaired := func(id, code string) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{"esphog"}})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]any{"type": "hello", "id": id, "token": "fedcba9876543210fedc", "code": code, "fw": "3.1.0"})
		_ = c.Write(ctx, websocket.MessageText, b)
		_, reply, _ := c.Read(ctx)
		if !strings.Contains(string(reply), `"type":"pair"`) {
			t.Fatalf("unclaimed board should be asked to pair: %s", reply)
		}
		return c
	}
	linked := unpaired("112233445566", "654321")
	defer linked.CloseNow()
	var try struct {
		Redirect, Error string
		Waiting         bool
	}
	if call("/api/pair/try", url.Values{"device": {"112233445566"}, "code": {"000000"}}, &try); try.Error == "" {
		t.Fatalf("a wrong code should be refused: %+v", try)
	}
	if call("/api/pair/try", url.Values{"device": {"778899AABBCC"}, "code": {"654321"}}, &try); !try.Waiting {
		t.Fatalf("a board that hasn't connected yet should be waited for: %+v", try)
	}
	if call("/api/pair/try", url.Values{"device": {"112233445566"}, "code": {"654321"}}, &try); try.Redirect != "/devices/112233445566?paired=1" {
		t.Fatalf("pairing from the link: %+v", try)
	}
	typed := unpaired("665544332211", "111222")
	defer typed.CloseNow()
	var bad struct{ Error string }
	if code := call("/api/pair", url.Values{"code": {"12"}}, &bad); code != 400 || bad.Error == "" {
		t.Fatalf("a short code should be refused: %d %+v", code, bad)
	}
	if to := post("/api/pair", url.Values{"code": {"111 222"}}); to != "/devices/665544332211?paired=1" {
		t.Fatalf("pairing by code: %q", to)
	}
	if devices, _ := st.UserDevices(ctx, mustUser(t, st).ID); len(devices) != 3 {
		t.Fatalf("want 3 boards on the account, got %d", len(devices))
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
