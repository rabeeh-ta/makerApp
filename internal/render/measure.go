package render

import "strings"

//go:generate go run ./fontgen -fonts "../../firmware/.pio/libdeps/feather_s3_rtft/Adafruit GFX Library/Fonts" -out fonts.go

// Measuring text the way the board does (firmware/src/display.cpp), so the
// server can tell when something won't fit the 240x135 screen.

const (
	screenW     = 240
	rowsPerPage = 5 // list rows or text lines on one page
)

type glyph struct{ width, advance, offset int }

type font struct {
	name   string
	glyphs [95]glyph
}

// bigFonts are tried largest first for big values, as fitFont does on the board.
var bigFonts = []font{bold24, bold18, bold12, bold9}

// width is the board's textWidth: the right edge of the last visible glyph
// (Adafruit GFX getTextBounds). Characters it can't draw are skipped.
func (f font) width(s string) int {
	x, right := 0, -1
	for _, c := range s {
		if c < 0x20 || c > 0x7E {
			continue
		}
		g := f.glyphs[c-0x20]
		if g.width > 0 {
			right = max(right, x+g.offset+g.width-1)
		}
		x += g.advance
	}
	return right + 1
}

// ellipsize cuts s to fit maxW with "...", as the board does.
func (f font) ellipsize(s string, maxW int) string {
	if f.width(s) <= maxW {
		return s
	}
	t := s
	for len(t) > 1 && f.width(t+"...") > maxW {
		t = t[:len(t)-1]
	}
	return t + "..."
}

// wrap breaks s into lines at spaces and newlines, as the board does.
func (f font) wrap(s string, maxW int) []string {
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Split(para, " ") {
			trial := word
			if line != "" {
				trial = line + " " + word
			}
			if f.width(trial) > maxW && line != "" {
				lines = append(lines, line)
				line = word
			} else {
				line = trial
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// fitFont is the largest bold font s fits in, from bigFonts[first:]; the
// smallest if none fits.
func fitFont(s string, maxW, first int) font {
	for _, f := range bigFonts[first:] {
		if f.width(s) <= maxW {
			return f
		}
	}
	return bold9
}
