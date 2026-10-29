package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"

	"makerapp/internal/engine"
	"makerapp/internal/store"
)

// The App Store lists the apps anyone can install: makerApp's own
// (engine.Catalog, games included) and the ones people publish. Every board
// that installs one counts as an install. Each app has a page with its screens
// and how to set it up; an app whose sources are personal shows each viewer
// their own push links there.

// listing is an app in the App Store.
type listing struct {
	ID, Name, Desc, By string
	Category           string // Games or Tools (makerApp's own), or Community (published by people)
	Installs           int
	Preview            *webScreen
	On                 []*store.Device // the user's devices it's installed on
}

// featured are the apps the App Store shows first, in order. Any app's ID
// works, a published one's too.
var featured = []string{"flappy", "pomodoro", "runner", "clock"}

// listings is the App Store, most installed first. Previews show the
// viewer's own data for personal sources, never the publisher's.
func (s *Server) listings(r *http.Request, on map[string][]*store.Device) []listing {
	counts, _ := s.st.InstallCounts(r.Context())
	var out []listing
	for _, a := range engine.Catalog {
		category := "Tools"
		if a.Game {
			category = "Games"
		}
		out = append(out, listing{ID: a.ID, Name: a.Name, Desc: a.Desc, By: a.Publisher, Category: category, Installs: counts[a.ID],
			Preview: toWeb(s.eng.CatalogPreview(a.ID)), On: on[a.ID]})
	}
	published, _ := s.st.PublishedApps(r.Context())
	for _, p := range published {
		l := listing{ID: p.ID, Name: p.Name, Desc: p.Description, By: p.Publisher, Category: "Community", Installs: counts[p.ID], On: on[p.ID]}
		if screens, _ := s.st.AppScreens(r.Context(), p.ID); len(screens) > 0 {
			l.Preview = toWeb(s.eng.PreviewScreensFor(r.Context(), screens[:1], currentUser(r).ID)[0])
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Installs > out[j].Installs })
	return out
}

func (s *Server) storeList(w http.ResponseWriter, r *http.Request) {
	devices, _ := s.st.UserDevices(r.Context(), currentUser(r).ID)
	writeJSON(w, http.StatusOK, map[string]any{"Apps": s.listings(r, s.installedOn(r, devices)), "Featured": featured})
}

// storeDevice is one of the viewer's boards on an app's page.
type storeDevice struct {
	*store.Device
	Installed bool
	NeedsFW   string // catalog apps: the firmware the board needs first
}

// personalLink is where the viewer sends their own data for a personal source.
type personalLink struct {
	Name, PushURL string
	LastFetchedAt int64
	Data          string // the latest data, pretty and trimmed
}

// pushURLMarker in an app's README becomes the viewer's push link.
const pushURLMarker = "{{push_url}}"

// storeApp is an app's App Store page. Viewers who installed the app (and its
// publisher) get their own push links for its personal sources; others see
// which data the app will ask them for.
func (s *Server) storeApp(w http.ResponseWriter, r *http.Request) {
	ctx, u, id := r.Context(), currentUser(r), r.PathValue("id")
	devices, _ := s.st.UserDevices(ctx, u.ID)
	on, _ := s.st.DevicesWithApp(ctx, id)
	counts, _ := s.st.InstallCounts(ctx)
	page := map[string]any{"ID": id, "Installs": counts[id]}
	cat, isCatalog := engine.CatalogEntry(id)
	if isCatalog {
		page["Name"], page["Desc"], page["By"] = cat.Name, cat.Desc, cat.Publisher
		page["Screens"] = []*webScreen{toWeb(s.eng.CatalogPreview(id))}
	} else {
		a, by, err := s.st.InstallableApp(ctx, u.ID, id)
		if notFound(w, err) {
			return
		}
		mine := a.UserID == u.ID
		if mine {
			by = u.Username
		}
		screens, _ := s.st.AppScreens(ctx, a.ID)
		var shots []*webScreen
		for _, sc := range s.eng.PreviewScreensFor(ctx, screens, u.ID) {
			shots = append(shots, toWeb(sc))
		}
		page["Name"], page["Desc"], page["By"], page["Mine"], page["Public"] = a.Name, a.Description, by, mine, a.PublishedAt > 0
		page["Screens"] = shots

		installed := false
		for _, d := range devices {
			installed = installed || slices.Contains(on, d.ID)
		}
		marker := "YOUR_PUSH_LINK" // until they install, when they get one
		if installed || mine {
			sources, err := s.lib.PersonalLinks(ctx, u.ID, a.ID)
			if notFound(w, err) {
				return
			}
			links := make([]personalLink, len(sources))
			for i, src := range sources {
				links[i] = personalLink{Name: src.Name, PushURL: s.publicBase(r) + "/push/" + src.PushToken,
					LastFetchedAt: src.LastFetchedAt, Data: prettyData(src.LastData)}
			}
			if len(links) == 1 {
				marker = links[0].PushURL
			}
			page["Personal"] = links
		} else {
			personal, _ := s.st.PersonalSources(ctx, a.ID)
			var names []string
			for _, src := range personal {
				names = append(names, src.Name)
			}
			page["NeedsData"] = names
		}
		page["Setup"] = strings.ReplaceAll(a.Setup, pushURLMarker, marker)
	}
	boards := make([]storeDevice, len(devices))
	for i, d := range devices {
		boards[i] = storeDevice{Device: d, Installed: slices.Contains(on, d.ID)}
		if isCatalog && !cat.RunsOn(d.FWVersion) {
			boards[i].NeedsFW = cat.NeedsFirmware()
		}
	}
	page["Devices"] = boards
	writeJSON(w, http.StatusOK, page)
}

// prettyData indents JSON for reading, trimmed to a screenful.
func prettyData(data string) string {
	var b bytes.Buffer
	if json.Indent(&b, []byte(data), "", "  ") == nil {
		data = b.String()
	}
	if len(data) > 2000 {
		data = data[:2000] + "\n… (trimmed)"
	}
	return data
}
