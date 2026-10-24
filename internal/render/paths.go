package render

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// Path is one entry in the data explorer the screen editor shows next to a source.
type Path struct {
	Path    string
	Preview string
	Kind    string // value | array | object
}

// Paths flattens JSON into clickable gjson paths. Arrays show their first few
// elements plus a "#" entry for the whole column, which is what list and
// sparkline screens need.
func Paths(data string, limit int) []Path {
	var out []Path
	var walk func(prefix string, v gjson.Result, depth int)
	walk = func(prefix string, v gjson.Result, depth int) {
		if len(out) >= limit {
			return
		}
		switch {
		case v.IsArray():
			arr := v.Array()
			if prefix != "" {
				out = append(out, Path{Path: prefix, Preview: strconv.Itoa(len(arr)) + " items", Kind: "array"})
			}
			if depth > 6 {
				return
			}
			for i, e := range arr {
				if i >= 3 {
					break
				}
				walk(join(prefix, strconv.Itoa(i)), e, depth+1)
			}
			if len(arr) > 0 && !arr[0].IsObject() && !arr[0].IsArray() && prefix != "" {
				out = append(out, Path{Path: prefix + ".#", Preview: "all values", Kind: "array"})
			}
			if len(arr) > 0 && arr[0].IsObject() {
				arr[0].ForEach(func(k, e gjson.Result) bool {
					if !e.IsObject() && !e.IsArray() && len(out) < limit {
						out = append(out, Path{Path: join(prefix, "#."+escape(k.String())), Preview: "column", Kind: "array"})
					}
					return true
				})
			}
		case v.IsObject():
			if prefix != "" {
				out = append(out, Path{Path: prefix, Preview: "{...}", Kind: "object"})
			}
			if depth > 6 {
				return
			}
			v.ForEach(func(k, e gjson.Result) bool {
				walk(join(prefix, escape(k.String())), e, depth+1)
				return len(out) < limit
			})
		default:
			p := v.String()
			if len(p) > 60 {
				p = p[:60] + "..."
			}
			if v.Type == gjson.String {
				p = strconv.Quote(p)
			}
			out = append(out, Path{Path: prefix, Preview: p, Kind: "value"})
		}
	}
	walk("", gjson.Parse(orEmpty(data)), 0)
	return out
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func escape(k string) string {
	var b strings.Builder
	for _, r := range k {
		if strings.ContainsRune(`.*?#|@\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
