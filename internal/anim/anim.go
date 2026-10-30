// Package anim turns an uploaded GIF into the small animation file a board
// plays. The board's storage is only 128 KB, shared with games, so the picture
// is cut to 120x68 (the board doubles it to fill the screen), brought down to
// 15 colors, and each frame stores only the pixels that changed. If the result
// is still too big, every other frame is dropped until it fits.
//
// The file, little-endian (firmware/src/anim.cpp reads it):
//
//	"MKA1", u8 width, u8 height, u8 colors, u8 0, u16 frames
//	u16 RGB565 palette[colors]
//	each frame: u16 delay in ms, u16 length, then that many bytes of runs
//
// A run is one byte, color<<4 | n, for n+1 pixels (n = 15 adds the next byte to
// the count). Color 15 means "unchanged", so a frame needs the one before it;
// the first frame has none of them, which makes looping back to it safe.
package anim

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
	"math"
	"regexp"
	"slices"
)

const (
	W, H      = 120, 68
	MaxBytes  = 60 * 1024 // leaves room for a game or two on the board
	MaxUpload = 8 << 20
	MaxFrames = 150
	colors    = 15
	unchanged = 15
	minDelay  = 40 // ms: about what the board can draw
	maxSide   = 2048
	maxInput  = 1000 // frames in the uploaded GIF
)

var ErrNotGIF = errors.New("That file isn't a GIF we can read.")
var ErrTooLarge = errors.New("That GIF is too large: keep it under 2048 pixels a side and 1000 frames.")

var validHash = regexp.MustCompile(`^[0-9a-f]{12}$`)

// ValidHash reports whether s can name an animation.
func ValidHash(s string) bool { return validHash.MatchString(s) }

// Hash names an animation by its content.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

type frame struct {
	px    []color.RGBA // W*H
	delay int          // ms
}

// Encode converts a GIF to the board's animation file.
func Encode(r io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxUpload+1))
	if err != nil {
		return nil, ErrNotGIF
	}
	if len(raw) > MaxUpload {
		return nil, ErrTooLarge
	}
	cfg, err := gif.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrNotGIF
	}
	if cfg.Width > maxSide || cfg.Height > maxSide || cfg.Width == 0 || cfg.Height == 0 {
		return nil, ErrTooLarge
	}
	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil || len(g.Image) == 0 {
		return nil, ErrNotGIF
	}
	if len(g.Image) > maxInput {
		return nil, ErrTooLarge
	}
	frames := composite(g)
	for len(frames) > MaxFrames {
		frames = halve(frames)
	}
	pal, index := quantize(frames)
	for {
		data := pack(frames, pal, index)
		if len(data) <= MaxBytes {
			return data, nil
		}
		if len(frames) == 1 {
			return nil, errors.New("That picture is too detailed to fit on the board.")
		}
		frames = halve(frames)
	}
}

// composite plays the GIF, following its disposal rules, and shrinks each frame.
func composite(g *gif.GIF) []frame {
	w, h := g.Config.Width, g.Config.Height
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	out := make([]frame, 0, len(g.Image))
	for i, img := range g.Image {
		var saved *image.RGBA
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			saved = image.NewRGBA(canvas.Rect)
			copy(saved.Pix, canvas.Pix)
		}
		draw.Draw(canvas, img.Bounds(), img, img.Bounds().Min, draw.Over)
		delay := 100
		if i < len(g.Delay) && g.Delay[i] > 0 {
			delay = max(g.Delay[i]*10, minDelay)
		}
		out = append(out, frame{px: shrink(canvas), delay: delay})
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, img.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved.Pix)
		}
	}
	return out
}

// shrink crops the middle to the screen's shape and averages down to W*H.
func shrink(src *image.RGBA) []color.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	cw, ch := sw, sh
	if sw*H > sh*W {
		cw = sh * W / H
	} else {
		ch = sw * H / W
	}
	cw, ch = max(cw, 1), max(ch, 1)
	x0, y0 := (sw-cw)/2, (sh-ch)/2
	out := make([]color.RGBA, W*H)
	for y := range H {
		ya, yb := y0+y*ch/H, y0+(y+1)*ch/H
		yb = max(yb, ya+1)
		for x := range W {
			xa, xb := x0+x*cw/W, x0+(x+1)*cw/W
			xb = max(xb, xa+1)
			var r, g, b, n int
			for sy := ya; sy < yb; sy++ {
				row := src.Pix[sy*src.Stride:]
				for sx := xa; sx < xb; sx++ {
					p := row[sx*4 : sx*4+4]
					// Transparent shows the screen's black.
					r += int(p[0]) * int(p[3]) / 255
					g += int(p[1]) * int(p[3]) / 255
					b += int(p[2]) * int(p[3]) / 255
					n++
				}
			}
			out[y*W+x] = color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255}
		}
	}
	return out
}

// halve drops every other frame, adding its time to the one before.
func halve(frames []frame) []frame {
	out := make([]frame, 0, len(frames)/2+1)
	for i := 0; i < len(frames); i += 2 {
		f := frames[i]
		if i+1 < len(frames) {
			f.delay += frames[i+1].delay
		}
		out = append(out, f)
	}
	return out
}

// key is a color at 5 bits a channel.
func key(c color.RGBA) int { return int(c.R>>3)<<10 | int(c.G>>3)<<5 | int(c.B>>3) }

type box struct {
	keys []int
}

// tint is the axis from gray to colored: how far red and green differ.
const tint = 3

// channel is a key's red, green or blue, or its tint, each 0 to 31.
func channel(k, axis int) int {
	r, g, b := k>>10&31, k>>5&31, k&31
	switch axis {
	case 0:
		return r
	case 1:
		return g
	case 2:
		return b
	}
	return (r - g + 31) / 2
}

// quantize picks one palette for the whole animation by median cut, always
// splitting the box with the widest color range. That keeps a small bright
// detail, like red eyes in a gray picture, that popularity alone would lose.
// Tint counts double, so grays never get averaged with colors into a muddy brown.
func quantize(frames []frame) ([]color.RGBA, func(color.RGBA) uint8) {
	counts := map[int]int{}
	for _, f := range frames {
		for _, c := range f.px {
			counts[key(c)]++
		}
	}
	all := make([]int, 0, len(counts))
	for k := range counts {
		all = append(all, k)
	}
	slices.Sort(all)
	boxes := []box{{keys: all}}
	for len(boxes) < colors {
		best, bestRange, axis := -1, 0, 0
		for i, b := range boxes {
			if len(b.keys) < 2 {
				continue
			}
			for ch := range 4 {
				lo, hi := 31, 0
				for _, k := range b.keys {
					v := channel(k, ch)
					lo, hi = min(lo, v), max(hi, v)
				}
				spread := hi - lo
				if ch == tint {
					spread *= 2
				}
				if spread > bestRange {
					best, bestRange, axis = i, spread, ch
				}
			}
		}
		if best < 0 {
			break
		}
		b := boxes[best].keys
		slices.SortFunc(b, func(x, y int) int { return (channel(x, axis)-channel(y, axis))*32768 + x - y })
		total := 0
		for _, k := range b {
			total += counts[k]
		}
		// Split by pixels, but keep at least one color on each side.
		mid, seen := 1, 0
		for i, k := range b {
			seen += counts[k]
			if seen*2 >= total {
				mid = min(max(i, 1), len(b)-1)
				break
			}
		}
		boxes[best] = box{keys: b[:mid]}
		boxes = append(boxes, box{keys: b[mid:]})
	}
	pal := make([]color.RGBA, 0, len(boxes))
	for _, b := range boxes {
		var r, g, bl, n int
		for _, k := range b.keys {
			c := counts[k]
			r += (k >> 10 & 31) * c
			g += (k >> 5 & 31) * c
			bl += (k & 31) * c
			n += c
		}
		n = max(n, 1)
		pal = append(pal, color.RGBA{uint8(r * 255 / (31 * n)), uint8(g * 255 / (31 * n)), uint8(bl * 255 / (31 * n)), 255})
	}
	cache := map[int]uint8{}
	index := func(c color.RGBA) uint8 {
		k := key(c)
		if i, ok := cache[k]; ok {
			return i
		}
		best, bestD := 0, math.MaxInt
		for i, p := range pal {
			dr, dg, db := int(c.R)-int(p.R), int(c.G)-int(p.G), int(c.B)-int(p.B)
			if d := dr*dr + dg*dg + db*db; d < bestD {
				best, bestD = i, d
			}
		}
		cache[k] = uint8(best)
		return uint8(best)
	}
	return pal, index
}

func pack(frames []frame, pal []color.RGBA, index func(color.RGBA) uint8) []byte {
	var out bytes.Buffer
	out.WriteString("MKA1")
	out.Write([]byte{W, H, byte(len(pal)), 0})
	binary.Write(&out, binary.LittleEndian, uint16(len(frames)))
	for _, c := range pal {
		binary.Write(&out, binary.LittleEndian, uint16(c.R>>3)<<11|uint16(c.G>>2)<<5|uint16(c.B>>3))
	}
	prev := make([]uint8, W*H)
	cur := make([]uint8, W*H)
	for n, f := range frames {
		for i, c := range f.px {
			cur[i] = index(c)
		}
		var runs []byte
		for i := 0; i < len(cur); {
			c := cur[i]
			if n > 0 && c == prev[i] {
				c = unchanged
			}
			j := i + 1
			for j < len(cur) && j-i < 271 {
				next := cur[j]
				if n > 0 && next == prev[j] {
					next = unchanged
				}
				if next != c {
					break
				}
				j++
			}
			if count := j - i; count <= 15 {
				runs = append(runs, c<<4|byte(count-1))
			} else {
				runs = append(runs, c<<4|15, byte(count-16))
			}
			i = j
		}
		binary.Write(&out, binary.LittleEndian, uint16(f.delay))
		binary.Write(&out, binary.LittleEndian, uint16(len(runs)))
		out.Write(runs)
		copy(prev, cur)
	}
	return out.Bytes()
}

// Preview turns an animation file back into a GIF, doubled like the board
// doubles it, so the website shows exactly what the board will play.
func Preview(data []byte) ([]byte, error) {
	bad := errors.New("not an animation file")
	if len(data) < 10 || string(data[:4]) != "MKA1" {
		return nil, bad
	}
	w, h, ncolors := int(data[4]), int(data[5]), int(data[6])
	count := int(binary.LittleEndian.Uint16(data[8:]))
	pos := 10 + ncolors*2
	if len(data) < pos || ncolors == 0 {
		return nil, bad
	}
	pal := make(color.Palette, ncolors)
	for i := range pal {
		v := binary.LittleEndian.Uint16(data[10+i*2:])
		pal[i] = color.RGBA{uint8(v>>11) << 3, uint8(v>>5) << 2, uint8(v) << 3, 255}
	}
	screen := make([]uint8, w*h)
	out := &gif.GIF{}
	for range count {
		if pos+4 > len(data) {
			return nil, bad
		}
		delay := int(binary.LittleEndian.Uint16(data[pos:]))
		size := int(binary.LittleEndian.Uint16(data[pos+2:]))
		pos += 4
		if pos+size > len(data) {
			return nil, bad
		}
		runs, at := data[pos:pos+size], 0
		pos += size
		for i := 0; i < len(runs); i++ {
			c, n := runs[i]>>4, int(runs[i]&15)+1
			if n == 16 {
				i++
				if i == len(runs) {
					return nil, bad
				}
				n += int(runs[i])
			}
			if at+n > len(screen) {
				return nil, bad
			}
			if c != unchanged {
				for j := range n {
					screen[at+j] = c
				}
			}
			at += n
		}
		img := image.NewPaletted(image.Rect(0, 0, w*2, h*2-1), pal)
		for y := range h*2 - 1 {
			for x := range w * 2 {
				img.Pix[y*img.Stride+x] = screen[y/2*w+x/2]
			}
		}
		out.Image = append(out.Image, img)
		out.Delay = append(out.Delay, max(delay/10, 1))
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
