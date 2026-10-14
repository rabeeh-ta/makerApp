package render

import (
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

func TestEval(t *testing.T) {
	data := gjson.Parse(`{"results":[[1234567.5],[3]],"rows":[{"amount":"100"},{"amount":250.5}],"name":"Café — ok","pct":0.723,
		"ts":"2026-10-01T10:00:00Z"}`)
	c := Context{Now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Loc: time.UTC}
	cases := map[string]string{
		"{{ results.0.0 }}":              "1234567.5",
		"{{ results.0.0 | num }}":        "1,234,567.5",
		"{{ results.0.0 | compact }}":    "1.2M",
		"{{results.0.0|money}}":          "$1.2M",
		"{{ pct | pct100 }}":             "72.3%",
		"{{ rows.#.amount | sum }}":      "350.5",
		"{{ rows | len }}":               "2",
		"{{ rows.#.amount | max | num}}": "250.5",
		"{{ ts | ago }}":                 "2h ago",
		"Total: {{ results.1.0 }} items": "Total: 3 items",
		"{{ missing }}":                  "",
		"{{ name | upper }}":             "CAFÉ — OK",
	}
	for tpl, want := range cases {
		if got := Eval(tpl, data, c); got != want {
			t.Errorf("Eval(%q) = %q, want %q", tpl, got, want)
		}
	}
}

func TestRenderList(t *testing.T) {
	spec := DefaultSpec("list")
	spec.Title = "Deals"
	spec.ItemsPath = "results"
	spec.ItemLabel = "{{ properties.dealname }}"
	spec.ItemValue = "{{ properties.amount | money }}"
	s := Render("x", "x", "list", spec, Context{Data: `{"results":[{"properties":{"dealname":"Acme – big","amount":"52000"}}]}`})
	if len(s.Items) != 1 || s.Items[0][0] != "Acme - big" || s.Items[0][1] != "$52k" {
		t.Fatalf("items = %v", s.Items)
	}
}

func TestRenderSparkAndProgress(t *testing.T) {
	spec := DefaultSpec("spark")
	spec.SeriesPath = "hourly.temp"
	s := Render("x", "x", "spark", spec, Context{Data: `{"hourly":{"temp":[20.5,21,"22"]}}`})
	if len(s.Series) != 3 || s.Series[2] != 22 {
		t.Fatalf("series = %v", s.Series)
	}
	p := DefaultSpec("progress")
	p.PctValue = "{{ done }}"
	p.PctMax = "{{ goal }}"
	r := Render("x", "x", "progress", p, Context{Data: `{"done":30,"goal":120}`})
	if r.Pct != 0.25 {
		t.Fatalf("pct = %v", r.Pct)
	}
}

func TestPaths(t *testing.T) {
	ps := Paths(`{"results":[{"a.b":1,"n":"x"}],"total":2}`, 100)
	want := map[string]bool{"results": false, "results.0.a\\.b": false, "results.#.n": false, "total": false}
	for _, p := range ps {
		if _, ok := want[p.Path]; ok {
			want[p.Path] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing path %s in %+v", k, ps)
		}
	}
}

func TestDeviceFields(t *testing.T) {
	spec := DefaultSpec("big_number")
	spec.Value = "{{ device.time }}"
	spec.Sub = "Battery {{ device.battery }}"
	dev := Render("x", "x", "big_number", spec, Context{})
	if dev.Value != "{$time}" || dev.Sub != "Battery {$battery}" || !dev.Live {
		t.Fatalf("device render = %+v", dev)
	}
	now := time.Date(2026, 10, 1, 14, 5, 0, 0, time.UTC)
	prev := Render("x", "x", "big_number", spec, Context{Preview: true, Now: now, Loc: time.UTC})
	if prev.Value != "14:05" || prev.Live {
		t.Fatalf("preview render = %+v", prev)
	}
}
