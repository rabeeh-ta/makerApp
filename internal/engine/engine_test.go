package engine

import (
	"testing"

	"makerapp/internal/games"
	"makerapp/internal/render"
)

func TestLegacyFirmware(t *testing.T) {
	for v, want := range map[string]bool{"1.0.0": true, "1.3.0": true, "2.0.0": false, "10.1": false, "dev": false, "": false} {
		if got := legacyFirmware(v); got != want {
			t.Errorf("legacyFirmware(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestFirmwareAtLeast(t *testing.T) {
	for _, c := range []struct {
		have, want string
		ok         bool
	}{{"3.1.1", "3.1.1", true}, {"3.1.0", "3.1.1", false}, {"3.1.2", "3.1.1", true}, {"3.2", "3.1.1", true},
		{"3.1", "3.1.0", true}, {"2.10", "2.9", true}, {"dev", "3.1.1", true}, {"", "3.1.1", true}} {
		if got := firmwareAtLeast(c.have, c.want); got != c.ok {
			t.Errorf("firmwareAtLeast(%q, %q) = %v, want %v", c.have, c.want, got, c.ok)
		}
	}
}

func TestCatalogRunsOn(t *testing.T) {
	flappy, _ := CatalogEntry("flappy")
	runner, _ := CatalogEntry("runner")
	pomodoro, _ := CatalogEntry("pomodoro")
	clock, _ := CatalogEntry("clock")
	for v, want := range map[string]bool{"2.0.1": false, "2.1.0": true, "2.10": true, "3.0.0": true, "dev": true} {
		if got := flappy.RunsOn(v); got != want {
			t.Errorf("flappy.RunsOn(%q) = %v, want %v", v, got, want)
		}
	}
	if runner.RunsOn("2.2.0") || !runner.RunsOn("2.3.0") || !runner.RunsOn("3.0.0") {
		t.Error("runner: built in from 2.3, downloaded from 3.0")
	}
	if pomodoro.RunsOn("3.0.1") || !pomodoro.RunsOn("3.1.0") || pomodoro.NeedsFirmware() != "3.1" {
		t.Error("pomodoro needs the 3.1 app API (save, saved, now)")
	}
	if !clock.RunsOn("1.3.0") {
		t.Error("clock should run on any firmware")
	}
	for _, a := range Catalog {
		if a.Publisher == "" || (a.Native == "" && a.Lua == "") {
			t.Errorf("%s needs a publisher and a firmware that runs it", a.ID)
		}
	}
}

func TestLuaAppScreens(t *testing.T) {
	for _, a := range Catalog {
		if a.Lua == "" {
			continue
		}
		if games.Hash(a.ID) == "" {
			t.Errorf("app %s has no script in internal/games", a.ID)
		}
		lua := a.deviceScreen(a.Lua + ".0")
		if lua.Layout != "game" || lua.Game != a.ID || lua.Hash != games.Hash(a.ID) {
			t.Errorf("%s on %s: %+v", a.ID, a.Lua, lua)
		}
		if a.Native != "" {
			if old := a.deviceScreen(a.Native + ".0"); old.Layout != a.ID || old.Hash != "" {
				t.Errorf("%s on %s should use the built-in layout: %+v", a.ID, a.Native, old)
			}
		}
	}
}

func TestLegacyFlowShowsEveryScreen(t *testing.T) {
	state := map[string]any{}
	addLegacyFlow(state, []AppView{
		{ID: "clock", Screens: []render.Screen{{ID: "clock", Layout: "clock"}}},
		{ID: "a1", Screens: []render.Screen{{ID: "s1", Layout: "text"}, {ID: "s2", Layout: "list"}}},
	})
	nodes := state["nodes"].([]map[string]any)
	if len(nodes) != 3 || nodes[0]["kind"] != "clock" || nodes[2]["group"] != "all" || state["home"] != "clock.0" {
		t.Fatalf("nodes = %v, home = %v", nodes, state["home"])
	}
	if screens := state["screens"].([]render.Screen); len(screens) != 2 {
		t.Fatalf("legacy screens = %v", screens)
	}
}
