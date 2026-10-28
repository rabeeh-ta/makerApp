package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"makerapp/internal/store"
)

// TestRefreshTimes: a board showing {{ @ago }} gets a new state once the time
// it shows is out of date, and nothing while it isn't.
func TestRefreshTimes(t *testing.T) {
	ctx := context.Background()
	site := newTestSite(t)
	me := site.signUp("me@example.com")
	u, _ := site.st.UserByEmail(ctx, "me@example.com")
	const board, token = "AABBCCDDEEFF", "0123456789abcdef0123"
	if err := site.st.UpsertDeviceHello(ctx, &store.Device{ID: board, TokenHash: store.HashToken(token)}); err != nil {
		t.Fatal(err)
	}
	if err := site.st.ClaimDeviceFor(ctx, board, u.ID, "Desk"); err != nil {
		t.Fatal(err)
	}

	srcID := strings.TrimPrefix(site.post(me, "/api/sources", url.Values{"name": {"Usage"}, "kind": {"push"}}), "/sources/")
	appID := strings.TrimPrefix(site.post(me, "/api/apps", url.Values{"name": {"Usage"}}), "/apps/")
	if err := site.st.SaveScreen(ctx, &store.Screen{UserID: u.ID, Name: "Usage", SourceID: srcID, AppID: appID,
		Layout: "big_number", Spec: `{"title": "Usage", "value": "{{ pct }}%", "sub": "{{ @ago }}"}`}); err != nil {
		t.Fatal(err)
	}
	if err := site.st.SetSourceResult(ctx, srcID, `{"pct": 37}`, 200, ""); err != nil {
		t.Fatal(err)
	}
	site.post(me, "/api/apps/"+appID+"/install", url.Values{"device": {board}})

	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(site.url, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	hello, _ := json.Marshal(map[string]any{"type": "hello", "id": board, "token": token, "code": "123456", "fw": "3.1.0"})
	if err := ws.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	// A read that times out closes the connection, so one reader feeds a
	// channel and next waits on that: the board's next message, "" if none
	// comes in time.
	msgs := make(chan string, 8)
	go func() {
		for {
			_, b, err := ws.Read(ctx)
			if err != nil {
				close(msgs)
				return
			}
			msgs <- string(b)
		}
	}()
	next := func(wait time.Duration) string {
		select {
		case m := <-msgs:
			return m
		case <-time.After(wait):
			return ""
		}
	}
	if first := next(5 * time.Second); !strings.Contains(first, "just now") {
		t.Fatalf("first state: %s", first)
	}

	site.eng.RefreshTimes(ctx)
	if m := next(300 * time.Millisecond); m != "" {
		t.Fatalf("nothing changed, but the board got: %s", m)
	}
	if _, err := site.db.Exec(`UPDATE sources SET last_fetched_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format("2006-01-02T15:04:05Z"), srcID); err != nil {
		t.Fatal(err)
	}
	site.eng.RefreshTimes(ctx)
	if m := next(5 * time.Second); !strings.Contains(m, "2h ago") {
		t.Fatalf("after two hours the board should show 2h ago: %q", m)
	}
}
