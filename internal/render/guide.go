package render

import (
	"fmt"
	"strings"
)

// fitCount is how many copies of c fit in maxW: a typical-width letter or digit
// gives a fair "about N characters".
func fitCount(f font, c string, maxW int) int {
	n := 0
	for f.width(strings.Repeat(c, n+1)) <= maxW {
		n++
	}
	return n
}

// LayoutGuide describes the board's screen to someone (or an AI) building
// screens: the layouts and how much text fits, measured with the board's fonts.
func LayoutGuide() string {
	return fmt.Sprintf(`The screen is 240x135 pixels: white text on black, with an accent color (default orange #F54E00) and an optional background color.
Every screen has a title at the top in the accent color: about %d characters fit, longer titles are cut with "...". Screens in an app show a dot each in the corner.

Layouts:
- big_number: one big value and an optional subtitle. The value takes the largest font it fits: about %d digits at the biggest size, %d at the next; longer values drop to a small font. The subtitle fits about %d characters. sub_color "auto" turns the subtitle green when it starts with + and red with -.
- list: rows from a list in the data. items_path points at the list; item_label and item_value are templates evaluated against each item (paths relative to the item). %d rows a page, the board pages through the rest; max_items caps the rows (default 10, at most 30). Labels are cut to fit beside the value.
- text: up to %d characters of text, wrapped to about %d characters a line, %d lines a page.
- spark: a value (about %d digits) above a line chart of a list of numbers; series_path points at that list.
- progress: a value, a bar filled pct_value out of pct_max, and a subtitle.
- gif: a looping GIF filling the whole screen, with no title. The user uploads it on the screen's page in makerApp; the gif field holds what the upload gives back. It's cropped to fit and brought down to 15 colors, and long GIFs lose frames to fit the board.`,
		fitCount(bold9, "n", screenW-20),
		fitCount(bold24, "8", screenW-16), fitCount(bold18, "8", screenW-16),
		fitCount(sans9, "n", screenW-16),
		rowsPerPage,
		maxBody, fitCount(sans9, "n", screenW-16), rowsPerPage,
		fitCount(bold18, "8", screenW-16))
}
