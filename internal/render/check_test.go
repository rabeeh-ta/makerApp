package render

import (
	"strings"
	"testing"
)

func TestWidth(t *testing.T) {
	if w := bold9.width(""); w != 0 {
		t.Fatalf("empty: %d", w)
	}
	if w := bold9.width("   "); w != 0 {
		t.Fatalf("spaces draw nothing: %d", w)
	}
	// Wider text in the same font, and the same text in a bigger font, measure wider.
	if !(bold9.width("WWW") > bold9.width("iii")) || !(bold24.width("42") > bold9.width("42")) {
		t.Fatal("widths don't grow")
	}
	long := strings.Repeat("Temperature ", 5)
	if got := bold9.ellipsize(long, 200); bold9.width(got) > 200 || !strings.HasSuffix(got, "...") {
		t.Fatalf("ellipsize: %q is %d px", got, bold9.width(got))
	}
	if lines := sans9.wrap("one two three four five six seven eight nine ten eleven twelve", 100); len(lines) < 3 {
		t.Fatalf("wrap: %q", lines)
	}
	if f := fitFont("12,345", screenW-16, 0); f.name != bold24.name {
		t.Fatalf("a short number fits at 24 pt, got %s", f.name)
	}
}

func TestCheck(t *testing.T) {
	data := `{"current":{"temperature_2m":23.5},"hourly":{"temperature_2m":[1,2,3]},"deals":[{"name":"Acme","amount":5}]}`
	check := func(layout string, spec Spec) []string {
		t.Helper()
		return Check(Render("s", "", layout, spec, Context{Data: data, Preview: true}), spec, data, 1)
	}
	has := func(got []string, want string) bool {
		for _, g := range got {
			if strings.Contains(g, want) {
				return true
			}
		}
		return false
	}

	if got := check("big_number", Spec{Title: "Bengaluru", Value: "{{ current.temperature_2m }} C", Sub: "updated {{ @ago }}"}); len(got) != 0 {
		t.Fatalf("a good screen has no findings: %q", got)
	}
	got := check("big_number", Spec{Title: "A title that is far too long to fit on this board", Value: "{{ current.temp }}"})
	if !has(got, "Title") || !has(got, `"current.temp" isn't in the data`) || !has(got, "current.temperature_2m") || !has(got, "main value is empty") {
		t.Fatalf("findings: %q", got)
	}
	if got := check("big_number", Spec{Value: "1234567890123"}); !has(got, "small font") && !has(got, "doesn't fit") {
		t.Fatalf("a long value should be flagged: %q", got)
	}
	if got := check("list", Spec{ItemsPath: "deals", ItemLabel: "{{ name }}", ItemValue: "{{ amount }}"}); len(got) != 0 {
		t.Fatalf("row templates are relative to each item: %q", got)
	}
	if got := check("list", Spec{ItemsPath: "deals", ItemLabel: "{{ deals.0.name }}"}); !has(got, "relative to each item") {
		t.Fatalf("an absolute row path should be flagged: %q", got)
	}
	if got := check("spark", Spec{SeriesPath: "hourly.temperature_2m"}); len(got) != 0 {
		t.Fatalf("spark: %q", got)
	}
	if got := Check(Render("s", "", "big_number", Spec{Value: "{{ x }}"}, Context{Preview: true}), Spec{Value: "{{ x }}"}, "", 1); !has(got, "no data source") {
		t.Fatalf("no source: %q", got)
	}
}
