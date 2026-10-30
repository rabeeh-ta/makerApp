package render

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// Check looks at a rendered screen the way the board will draw it and returns
// what's wrong, in words a person (or an AI) can act on: text that gets cut,
// values that come out empty, paths that aren't in the data. data is the
// source's latest JSON ("" without a source); screens is how many screens the
// app has (the header shows a dot for each).
func Check(s Screen, spec Spec, data string, screens int) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	if s.Error != "" {
		add("The data source failed (%s), so values from it are empty.", s.Error)
	}
	out = append(out, checkPaths(s.Layout, spec, data)...)

	dots := 0
	if screens > 1 {
		dots = screens * 9
	}
	if s.Layout == "gif" {
		if s.GIF == "" {
			add("No GIF yet: upload one on this screen's page in makerApp.")
		}
		return out
	}
	if cut, n := fits(bold9, s.Title, screenW-20-dots); !cut {
		add("Title %q is cut to %q on the board; keep it to about %d characters.", s.Title, bold9.ellipsize(s.Title, screenW-20-dots), n)
	}

	switch s.Layout {
	case "list":
		if len(s.Items) == 0 {
			add("The list is empty: items_path %q gives no rows.", spec.ItemsPath)
		}
		cut := 0
		for _, it := range s.Items {
			valueW := 0
			if it[1] != "" {
				valueW = bold9.width(it[1]) + 10
			}
			if ok, _ := fits(sans9, it[0], screenW-20-valueW); !ok {
				cut++
			}
		}
		if cut > 0 {
			add("%d of %d row labels are cut short; shorter labels or values fit better.", cut, len(s.Items))
		}
		if len(s.Items) > rowsPerPage {
			add("%d rows: the board shows %d a page and pages through them with the buttons.", len(s.Items), rowsPerPage)
		}
	case "text":
		if strings.TrimSpace(s.Body) == "" {
			add("The text is empty.")
		} else if n := len(sans9.wrap(s.Body, screenW-16)); n > rowsPerPage {
			add("The text wraps to %d lines: %d pages of %d lines on the board.", n, (n+rowsPerPage-1)/rowsPerPage, rowsPerPage)
		}
	case "spark":
		if len(s.Series) < 2 {
			add("The sparkline needs at least 2 numbers; series_path %q gives %d.", spec.SeriesPath, len(s.Series))
		}
		out = append(out, checkValue(s.Value, 1, false)...)
	case "progress":
		out = append(out, checkValue(s.Value, 0, false)...)
		out = append(out, checkSub(s.Sub)...)
	case "big_number":
		out = append(out, checkValue(s.Value, 0, true)...)
		out = append(out, checkSub(s.Sub)...)
	}
	return out
}

// fits reports whether s fits maxW in f, and about how many characters do.
func fits(f font, s string, maxW int) (bool, int) {
	if f.width(s) <= maxW {
		return true, len(s)
	}
	return false, len(f.ellipsize(s, maxW)) - 3
}

// checkValue: big values shrink through 24, 18, 12 and 9 pt until they fit.
func checkValue(v string, first int, required bool) []string {
	if v == "" {
		if required {
			return []string{"The main value is empty (the board shows \"-\")."}
		}
		return nil
	}
	maxW := screenW - 16
	f := fitFont(v, maxW, first)
	switch {
	case f.width(v) > maxW:
		return []string{fmt.Sprintf("The main value %q doesn't fit even at the smallest size and is cut off; shorten it (| compact helps with big numbers).", v)}
	case f.name == bold9.name || f.name == bold12.name:
		_, n := fits(bold18, v, maxW) // 18 pt is the smallest size that still reads as big
		return []string{fmt.Sprintf("The main value %q only fits in a small font; it reads best at about %d characters or fewer.", v, n)}
	}
	return nil
}

func checkSub(sub string) []string {
	if ok, n := fits(sans9, sub, screenW-16); !ok {
		return []string{fmt.Sprintf("Subtitle %q is cut to %q; keep it to about %d characters.", sub, sans9.ellipsize(sub, screenW-16), n)}
	}
	return nil
}

// checkPaths finds template paths that aren't in the data, with close matches.
func checkPaths(layout string, spec Spec, data string) []string {
	var out []string
	root := gjson.Parse(orEmpty(data))
	missing := func(field, path string) {
		msg := fmt.Sprintf("%s: %q isn't in the data.", field, path)
		if near := nearPaths(data, path); len(near) > 0 {
			msg += " Close matches: " + strings.Join(near, ", ")
		}
		out = append(out, msg)
	}
	tpls := map[string]string{"title": spec.Title}
	switch layout {
	case "list":
		items := root.Get(spec.ItemsPath)
		switch {
		case spec.ItemsPath == "":
			out = append(out, "items_path is empty; set it to the path of a list in the data.")
		case !items.Exists():
			missing("items_path", spec.ItemsPath)
		case !items.IsArray() && !items.IsObject():
			out = append(out, fmt.Sprintf("items_path %q is a single value, not a list.", spec.ItemsPath))
		default:
			// Row templates are relative to each item; check them on the first.
			var first gjson.Result
			items.ForEach(func(_, v gjson.Result) bool { first = v; return false })
			for field, tpl := range map[string]string{"item_label": spec.ItemLabel, "item_value": spec.ItemValue} {
				for _, p := range tplPaths(tpl) {
					if !first.Get(p).Exists() {
						out = append(out, fmt.Sprintf("%s: %q isn't in the list's items (paths are relative to each item).", field, p))
					}
				}
			}
		}
	case "text":
		tpls["body"] = spec.Body
	case "spark":
		tpls["value"] = spec.Value
		if spec.SeriesPath != "" && !root.Get(spec.SeriesPath).Exists() {
			missing("series_path", spec.SeriesPath)
		}
	case "progress":
		tpls["value"], tpls["sub"], tpls["pct_value"], tpls["pct_max"] = spec.Value, spec.Sub, spec.PctValue, spec.PctMax
	default:
		tpls["value"], tpls["sub"] = spec.Value, spec.Sub
	}
	for _, field := range []string{"title", "value", "sub", "body", "pct_value", "pct_max"} {
		for _, p := range tplPaths(tpls[field]) {
			if data == "" {
				out = append(out, fmt.Sprintf("%s uses %q but the screen has no data source.", field, p))
			} else if !root.Get(p).Exists() {
				missing(field, p)
			}
		}
	}
	return out
}

// tplPaths are the data paths a template reads, without the board's own
// fields (device.*) and the specials (@ago, @time, @date).
func tplPaths(tpl string) []string {
	var out []string
	for _, m := range tplRe.FindAllStringSubmatch(tpl, -1) {
		p := strings.TrimSpace(strings.Split(m[1], "|")[0])
		if p == "" || p == "." || strings.HasPrefix(p, "@") || strings.HasPrefix(p, "device.") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// nearPaths are up to three paths in the data that look like what was meant:
// the same last name, a name containing it, or a sibling in the same object.
func nearPaths(data, path string) []string {
	i := strings.LastIndex(path, ".")
	parent, last := path[:max(i, 0)], strings.ToLower(path[i+1:])
	var exact, partial, sibling []string
	for _, p := range Paths(data, 300) {
		if p.Path == path || p.Kind != "value" {
			continue
		}
		j := strings.LastIndex(p.Path, ".")
		pParent, pLast := p.Path[:max(j, 0)], strings.ToLower(p.Path[j+1:])
		switch {
		case pLast == last:
			exact = append(exact, p.Path)
		case last != "" && (strings.Contains(pLast, last) || strings.Contains(last, pLast)):
			partial = append(partial, p.Path)
		case parent != "" && pParent == parent:
			sibling = append(sibling, p.Path)
		}
	}
	out := append(append(exact, partial...), sibling...)
	return out[:min(len(out), 3)]
}
