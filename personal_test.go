package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"makerapp/internal/store"
)

// TestPersonalData: an app published with a personal source shows each
// installer their own data, sent to their own push link, and never the
// publisher's: not on their board, not in the App Store previews.
func TestPersonalData(t *testing.T) {
	ctx := context.Background()
	site := newTestSite(t)
	// Push links name the machine's LAN address, for boards; the test server
	// listens on loopback, so links are compared and used by path.
	path := func(link string) string {
		u, _ := url.Parse(link)
		return u.Path
	}
	push := func(link, body string) {
		t.Helper()
		resp, err := http.Post(site.url+path(link), "application/json", strings.NewReader(body))
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("push to %s: %v %v", link, resp, err)
		}
		resp.Body.Close()
	}

	// The publisher's app: one screen showing a personal push source.
	pub := site.signUp("pub@example.com")
	srcID := strings.TrimPrefix(site.post(pub, "/api/sources", url.Values{"name": {"Usage"}, "kind": {"push"}, "per_user": {"1"},
		"example": {`{"pct": 7}`}}), "/sources/")
	origin, err := site.st.Source(ctx, srcID)
	if err != nil || !origin.PerUser {
		t.Fatalf("personal source: %+v %v", origin, err)
	}
	appID := strings.TrimPrefix(site.post(pub, "/api/apps", url.Values{"name": {"Usage"}}), "/apps/")
	if err := site.st.SaveScreen(ctx, &store.Screen{UserID: origin.UserID, Name: "Usage", SourceID: srcID, AppID: appID,
		Layout: "big_number", Spec: `{"title": "Usage", "value": "{{ pct }}%"}`}); err != nil {
		t.Fatal(err)
	}
	originLink := "/push/" + origin.PushToken
	push(originLink, `{"pct": 11}`)
	site.post(pub, "/api/apps/"+appID+"/publish", url.Values{"username": {"pub"}, "description": {"Your usage"},
		"setup": {"Send it: curl -d @usage.json {{push_url}}"}})

	// Someone else, with a board.
	inst := site.signUp("inst@example.com")
	u, _ := site.st.UserByEmail(ctx, "inst@example.com")
	const board = "AABBCCDDEEFF"
	if err := site.st.UpsertDeviceHello(ctx, &store.Device{ID: board, TokenHash: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := site.st.ClaimDeviceFor(ctx, board, u.ID, "Desk"); err != nil {
		t.Fatal(err)
	}

	type shot struct {
		Value string `json:"value"`
	}
	type page struct {
		Screens   []shot
		Setup     string
		NeedsData []string
		Personal  []struct{ PushURL string }
	}
	var before page
	site.call(inst, "/api/store/"+appID, nil, &before)
	if len(before.Screens) != 1 || before.Screens[0].Value != "7%" {
		t.Fatalf("store page should show the example data, never the publisher's: %+v", before.Screens)
	}
	if len(before.NeedsData) != 1 || len(before.Personal) != 0 || !strings.Contains(before.Setup, "YOUR_PUSH_LINK") {
		t.Fatalf("before installing: %+v", before)
	}
	var list struct {
		Apps []struct {
			ID      string
			Preview *shot
		}
	}
	site.call(inst, "/api/store", nil, &list)
	for _, l := range list.Apps {
		if l.ID == appID && (l.Preview == nil || strings.Contains(l.Preview.Value, "11")) {
			t.Fatalf("store list shows the publisher's data: %+v", l.Preview)
		}
	}

	// Installing gives them their own push link, in the setup instructions too.
	site.post(inst, "/api/apps/"+appID+"/install", url.Values{"device": {board}})
	var after page
	site.call(inst, "/api/store/"+appID, nil, &after)
	if len(after.Personal) != 1 || path(after.Personal[0].PushURL) == originLink || !strings.Contains(after.Setup, after.Personal[0].PushURL) {
		t.Fatalf("after installing: %+v", after)
	}
	state := func() string {
		s, err := site.eng.BuildState(ctx, board)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(s)
		return string(b)
	}
	if b := state(); strings.Contains(b, "11%") || strings.Contains(b, "7%") {
		t.Fatalf("board shows the publisher's or the example data: %s", b)
	}
	push(after.Personal[0].PushURL, `{"pct": 42}`)
	if !strings.Contains(state(), "42%") {
		t.Fatalf("board doesn't show its owner's data: %s", state())
	}

	// The publisher still sees their own, on their own link.
	var own struct {
		Screens []struct{ S shot }
	}
	site.call(pub, "/api/apps/"+appID, nil, &own)
	if len(own.Screens) != 1 || own.Screens[0].S.Value != "11%" {
		t.Fatalf("publisher's preview: %+v", own.Screens)
	}
	var mine page
	site.call(pub, "/api/store/"+appID, nil, &mine)
	if len(mine.Personal) != 1 || path(mine.Personal[0].PushURL) != originLink {
		t.Fatalf("publisher's store page: %+v", mine.Personal)
	}

	// The copy belongs to the installed app, not to their data sources.
	var sources struct{ Sources []struct{ ID string } }
	site.call(inst, "/api/sources", nil, &sources)
	if len(sources.Sources) != 0 {
		t.Fatalf("installer's data sources: %+v", sources.Sources)
	}
}
