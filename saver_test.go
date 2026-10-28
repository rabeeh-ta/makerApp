package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"makerapp/internal/fw"
	"makerapp/internal/store"
)

// TestScreensaver: a board's screensaver keeps the screens it can play, in
// the order chosen (never a game), and drops one when its app is uninstalled.
func TestScreensaver(t *testing.T) {
	ctx := context.Background()
	site := newTestSite(t)
	me := site.signUp("me@example.com")
	u, _ := site.st.UserByEmail(ctx, "me@example.com")
	const board = "AABBCCDDEEFF"
	if err := site.st.UpsertDeviceHello(ctx, &store.Device{ID: board, TokenHash: "x", FWVersion: fw.Version()}); err != nil {
		t.Fatal(err)
	}
	if err := site.st.ClaimDeviceFor(ctx, board, u.ID, "Desk"); err != nil {
		t.Fatal(err)
	}
	appID := strings.TrimPrefix(site.post(me, "/api/apps", url.Values{"name": {"Usage"}}), "/apps/")
	for _, name := range []string{"One", "Two"} {
		if err := site.st.SaveScreen(ctx, &store.Screen{UserID: u.ID, Name: name, AppID: appID, Layout: "text",
			Spec: `{"title": "` + name + `", "body": "hi"}`}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{appID, "clock", "flappy"} {
		site.post(me, "/api/apps/"+id+"/install", url.Values{"device": {board}})
	}

	var page struct {
		Runs    bool
		Choices []struct{ App, Screen string }
	}
	site.call(me, "/api/devices/"+board+"/screensaver", nil, &page)
	if !page.Runs || len(page.Choices) != 3 { // two screens and the clock; no game
		t.Fatalf("choices: %+v", page)
	}
	two := page.Choices[1]

	// Chosen: the second screen, a game, something unknown, then the clock.
	site.post(me, "/api/devices/"+board+"/screensaver", url.Values{"interval_sec": {"15"},
		"item": {appID + "/" + two.Screen, "flappy/flappy", "nope/nope", "clock/clock"}})
	saver := func() string {
		state, err := site.eng.BuildState(ctx, board)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(state["saver"])
		return string(b)
	}
	want := `{"after":30,"interval":15,"items":[{"app":"` + appID + `","screen":"` + two.Screen + `"},{"app":"clock","screen":"clock"}]}`
	if got := saver(); got != want {
		t.Fatalf("saver state:\n got %s\nwant %s", got, want)
	}

	site.post(me, "/api/apps/clock/uninstall", url.Values{"device": {board}})
	if got := saver(); strings.Contains(got, "clock") {
		t.Fatalf("an uninstalled app's screen stays: %s", got)
	}
}
