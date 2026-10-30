// Package render turns a screen definition plus the latest JSON from its source
// into the resolved payload the device draws. Text fields are templates:
//
//	{{ results.0.count }}            value at a gjson path
//	{{ results.0.count | compact }}  with a formatter (num, compact, money, pct, pct100,
//	                                 fixed:N, upper, lower, date, time, ago,
//	                                 len, sum, avg, min, max, first, last)
//	{{ @ago }} / {{ @time }}         when the source was fetched / current time
package render

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"makerapp/internal/anim"
)

// Layouts a screen can use. The firmware also draws "clock" for the built-in Clock app.
var Layouts = []string{"big_number", "list", "text", "spark", "progress", "gif"}

type Spec struct {
	Title      string `json:"title"`
	Accent     string `json:"accent"`
	BG         string `json:"bg"`
	Value      string `json:"value"`
	Sub        string `json:"sub"`
	SubColor   string `json:"sub_color"`
	Body       string `json:"body"`
	ItemsPath  string `json:"items_path"`
	ItemLabel  string `json:"item_label"`
	ItemValue  string `json:"item_value"`
	MaxItems   int    `json:"max_items"`
	SeriesPath string `json:"series_path"`
	PctValue   string `json:"pct_value"`
	PctMax     string `json:"pct_max"`
	GIF        string `json:"gif,omitempty"` // the hash of an uploaded animation (internal/anim)
}

func DefaultSpec(layout string) Spec {
	return Spec{Accent: "#F54E00", MaxItems: 10}
}

func ParseSpec(layout, raw string) Spec {
	s := DefaultSpec(layout)
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s
}

// Screen is the resolved payload sent to the device (and drawn by the web preview).
type Screen struct {
	ID       string      `json:"id"`
	Name     string      `json:"-"`
	Layout   string      `json:"layout"`
	Title    string      `json:"title"`
	Accent   string      `json:"accent,omitempty"`
	BG       string      `json:"bg,omitempty"`
	Value    string      `json:"value,omitempty"`
	Sub      string      `json:"sub,omitempty"`
	SubColor string      `json:"subColor,omitempty"`
	Body     string      `json:"body,omitempty"`
	Items    [][2]string `json:"items,omitempty"`
	Series   []float64   `json:"series,omitempty"`
	Pct      float64     `json:"pct,omitempty"`
	Live     bool        `json:"live,omitempty"` // contains {$field} markers the device fills in
	Game     string      `json:"game,omitempty"` // layout "game": the Lua game to run
	Hash     string      `json:"hash,omitempty"` // and its version, which the board downloads
	GIF      string      `json:"gif,omitempty"`  // layout "gif": the animation the board downloads
	Error    string      `json:"-"`              // source error, shown in the web UI only
}

type Context struct {
	Preview   bool   // web preview: show sample device values instead of {$markers}
	Data      string // source JSON; "" when the screen has no source
	FetchedAt time.Time
	Now       time.Time
	Loc       *time.Location
}

const (
	maxItems  = 30
	maxSeries = 120
	maxBody   = 1500
)

func Render(id, name, layout string, spec Spec, c Context) Screen {
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	if c.Loc == nil {
		c.Loc = time.Local
	}
	root := gjson.Parse(orEmpty(c.Data))
	ev := func(tpl string) string { return Eval(tpl, root, c) }

	out := Screen{
		ID:     id,
		Name:   name,
		Layout: layout,
		Title:  ASCII(ev(spec.Title)),
		Accent: validColor(spec.Accent),
		BG:     validColor(spec.BG),
	}

	switch layout {
	case "list":
		out.Items = items(spec, root, c)
	case "text":
		out.Body = ASCII(truncate(ev(spec.Body), maxBody))
	case "spark":
		out.Value = ASCII(ev(spec.Value))
		out.Series = series(root.Get(spec.SeriesPath))
	case "progress":
		out.Value = ASCII(ev(spec.Value))
		out.Sub = ASCII(ev(spec.Sub))
		out.Pct = pct(ev(spec.PctValue), ev(spec.PctMax))
	case "gif":
		if anim.ValidHash(spec.GIF) {
			out.GIF = spec.GIF
		}
	default:
		out.Layout = "big_number"
		out.Value = ASCII(ev(spec.Value))
		out.Sub = ASCII(ev(spec.Sub))
		out.SubColor = subColor(spec.SubColor, out.Sub)
	}
	out.Live = strings.Contains(out.Title+out.Value+out.Sub+out.Body, "{$")
	for _, it := range out.Items {
		if strings.Contains(it[0]+it[1], "{$") {
			out.Live = true
		}
	}
	return out
}

// DeviceFields are values the board fills in itself ({{ device.<name> }}).
var DeviceFields = []struct{ Name, Desc string }{
	{"time", "14:05"}, {"time12", "2:05 PM"}, {"date", "Thu, Oct 1"}, {"day", "Thursday"},
	{"battery", "87%"}, {"battery_v", "3.98V"}, {"chip_temp", "41C"}, {"wifi", "-58 dBm"},
	{"uptime", "3h 12m"}, {"ip", "192.168.1.20"},
}

// DeviceSample is what the web preview shows for a device field.
func DeviceSample(field string, c Context) string {
	t := c.Now.In(c.Loc)
	switch field {
	case "time":
		return t.Format("15:04")
	case "time12":
		return t.Format("3:04 PM")
	case "date":
		return t.Format("Mon, Jan 2")
	case "day":
		return t.Format("Monday")
	}
	for _, f := range DeviceFields {
		if f.Name == field {
			return f.Desc
		}
	}
	return "?"
}

func orEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

func items(spec Spec, root gjson.Result, c Context) [][2]string {
	n := spec.MaxItems
	if n <= 0 || n > maxItems {
		n = maxItems
	}
	var out [][2]string
	list := root
	if spec.ItemsPath != "" {
		list = root.Get(spec.ItemsPath)
	}
	add := func(key, elem gjson.Result) bool {
		if len(out) >= n {
			return false
		}
		var label, value string
		if spec.ItemLabel != "" {
			label = Eval(spec.ItemLabel, elem, c)
		} else if key.Exists() {
			label = key.String()
		} else {
			label = scalar(elem)
		}
		if spec.ItemValue != "" {
			value = Eval(spec.ItemValue, elem, c)
		} else if key.Exists() {
			value = scalar(elem)
		}
		out = append(out, [2]string{ASCII(label), ASCII(value)})
		return true
	}
	if list.IsArray() {
		list.ForEach(func(_, v gjson.Result) bool { return add(gjson.Result{}, v) })
	} else if list.IsObject() {
		list.ForEach(add)
	}
	return out
}

func series(r gjson.Result) []float64 {
	var vals []float64
	r.ForEach(func(_, v gjson.Result) bool {
		if f, ok := number(v); ok {
			vals = append(vals, f)
		}
		return true
	})
	if len(vals) <= maxSeries {
		return vals
	}
	out := make([]float64, maxSeries)
	for i := range out {
		out[i] = vals[i*(len(vals)-1)/(maxSeries-1)]
	}
	return out
}

func pct(v, max string) float64 {
	val, ok := parseNum(v)
	if !ok {
		return 0
	}
	if m, ok := parseNum(max); ok && m != 0 {
		val = val / m
	} else if val > 1 {
		val = val / 100
	}
	return math.Max(0, math.Min(1, math.Round(val*1000)/1000))
}

func subColor(mode, sub string) string {
	if mode == "auto" {
		switch {
		case strings.HasPrefix(sub, "+"):
			return "#2BB673"
		case strings.HasPrefix(sub, "-"):
			return "#F04438"
		}
		return ""
	}
	return validColor(mode)
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validColor(c string) string {
	if colorRe.MatchString(c) {
		return strings.ToUpper(c)
	}
	return ""
}

// ---- template evaluation ----

var tplRe = regexp.MustCompile(`\{\{\s*(.*?)\s*\}\}`)

func Eval(tpl string, root gjson.Result, c Context) string {
	if !strings.Contains(tpl, "{{") {
		return tpl
	}
	return tplRe.ReplaceAllStringFunc(tpl, func(m string) string {
		expr := tplRe.FindStringSubmatch(m)[1]
		parts := strings.Split(expr, "|")
		path := strings.TrimSpace(parts[0])
		var v gjson.Result
		var s string
		switch path {
		case "@ago":
			if c.FetchedAt.IsZero() {
				return "never"
			}
			return ago(c.Now.Sub(c.FetchedAt))
		case "@time":
			s = c.Now.In(c.Loc).Format("15:04")
		case "@date":
			s = c.Now.In(c.Loc).Format("Jan 2")
		case "", ".", "@this":
			v = root
		default:
			if field, ok := strings.CutPrefix(path, "device."); ok {
				if c.Preview {
					return DeviceSample(field, c)
				}
				return "{$" + field + "}" // filled in on the device itself
			}
			v = root.Get(path)
		}
		if s == "" {
			s = scalar(v)
		}
		for _, f := range parts[1:] {
			s, v = apply(strings.TrimSpace(f), s, v, c)
		}
		return s
	})
}

func scalar(v gjson.Result) string {
	switch v.Type {
	case gjson.Null:
		return ""
	case gjson.Number:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case gjson.JSON:
		return v.Raw
	default:
		return v.String()
	}
}

func apply(f, s string, v gjson.Result, c Context) (string, gjson.Result) {
	name, arg, _ := strings.Cut(f, ":")
	switch name {
	case "len":
		if v.IsArray() {
			return strconv.Itoa(len(v.Array())), gjson.Result{}
		}
		return strconv.Itoa(len(s)), gjson.Result{}
	case "sum", "avg", "min", "max":
		return aggregate(name, v), gjson.Result{}
	case "first", "last":
		if a := v.Array(); len(a) > 0 {
			e := a[0]
			if name == "last" {
				e = a[len(a)-1]
			}
			return scalar(e), e
		}
		return "", gjson.Result{}
	case "upper":
		return strings.ToUpper(s), v
	case "lower":
		return strings.ToLower(s), v
	case "date", "time", "ago":
		t, ok := parseTime(s)
		if !ok {
			return s, v
		}
		switch name {
		case "date":
			return t.In(c.Loc).Format("Jan 2"), v
		case "time":
			return t.In(c.Loc).Format("15:04"), v
		}
		return ago(c.Now.Sub(t)), v
	}
	n, ok := parseNum(s)
	if !ok {
		return s, v
	}
	switch name {
	case "num":
		return thousands(n, 2), v
	case "fixed":
		d, _ := strconv.Atoi(arg)
		return strconv.FormatFloat(n, 'f', d, 64), v
	case "compact":
		return compact(n), v
	case "money":
		sym := arg
		if sym == "" {
			sym = "$"
		}
		if math.Abs(n) >= 10000 {
			return sym + compact(n), v
		}
		return sym + thousands(n, 2), v
	case "pct":
		return trimFloat(n, 1) + "%", v
	case "pct100":
		return trimFloat(n*100, 1) + "%", v
	case "signed":
		if n > 0 {
			return "+" + s, v
		}
	}
	return s, v
}

func aggregate(op string, v gjson.Result) string {
	var vals []float64
	v.ForEach(func(_, e gjson.Result) bool {
		if f, ok := number(e); ok {
			vals = append(vals, f)
		}
		return true
	})
	if len(vals) == 0 {
		return "0"
	}
	r := vals[0]
	sum := 0.0
	for _, f := range vals {
		sum += f
		if op == "min" {
			r = math.Min(r, f)
		} else if op == "max" {
			r = math.Max(r, f)
		}
	}
	switch op {
	case "sum":
		r = sum
	case "avg":
		r = sum / float64(len(vals))
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

func number(v gjson.Result) (float64, bool) {
	if v.Type == gjson.Number {
		return v.Float(), true
	}
	if v.Type == gjson.String {
		return parseNum(v.String())
	}
	return 0, false
}

func parseNum(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "$"), "%")
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

func parseTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1e12 {
			return time.UnixMilli(n), true
		}
		return time.Unix(n, 0), true
	}
	return time.Time{}, false
}

func trimFloat(f float64, decimals int) string {
	s := strconv.FormatFloat(f, 'f', decimals, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func thousands(f float64, decimals int) string {
	neg := f < 0
	s := trimFloat(math.Abs(f), decimals)
	intPart, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

func compact(f float64) string {
	a := math.Abs(f)
	switch {
	case a >= 1e9:
		return trimFloat(f/1e9, 1) + "B"
	case a >= 1e6:
		return trimFloat(f/1e6, 1) + "M"
	case a >= 1e3:
		return trimFloat(f/1e3, 1) + "k"
	}
	return trimFloat(f, 1)
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	}
	return strconv.Itoa(int(d.Hours()/24)) + "d ago"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

var asciiReplacements = strings.NewReplacer(
	"‘", "'", "’", "'", "“", `"`, "”", `"`,
	"–", "-", "—", "-", "…", "...", "•", "*",
	"€", "EUR ", "£", "GBP ", "₹", "Rs ", "°", "", "·", "-",
	" ", " ", "\t", "  ", "\r", "",
)

// ASCII keeps text within what the device fonts can draw (printable ASCII + newlines).
func ASCII(s string) string {
	s = asciiReplacements.Replace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || (r >= 0x20 && r < 0x7F):
			b.WriteRune(r)
		case r >= 0x80:
			b.WriteByte('?')
		}
	}
	return b.String()
}
