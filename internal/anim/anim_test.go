package anim

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"math/rand"
	"testing"
)

// makeGIF is a gray picture with a small red square that moves each frame.
func makeGIF(t *testing.T, frames, w, h int, noisy bool) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{128, 128, 128, 255}, color.RGBA{255, 0, 0, 255}, color.RGBA{255, 255, 255, 255}}
	g := &gif.GIF{}
	rng := rand.New(rand.NewSource(1))
	for n := range frames {
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for y := range h {
			for x := range w {
				i := uint8(1)
				if noisy {
					i = uint8(rng.Intn(4))
				}
				img.Pix[y*img.Stride+x] = i
			}
		}
		for y := range 20 {
			for x := range 20 {
				img.Pix[(h/2+y)*img.Stride+w/2-30+n*4+x] = 2
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 5)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEncodeThenPreviewShowsTheSamePicture(t *testing.T) {
	data, err := Encode(bytes.NewReader(makeGIF(t, 10, 500, 250, false)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "MKA1" || data[4] != W || data[5] != H {
		t.Fatalf("bad header % x", data[:8])
	}
	out, err := Preview(data)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 10 || g.Config.Width != 240 || g.Config.Height != 135 {
		t.Fatalf("got %d frames of %dx%d", len(g.Image), g.Config.Width, g.Config.Height)
	}
	if g.Delay[0] != 5 {
		t.Errorf("delay %d, want 5", g.Delay[0])
	}
	// The red square survives shrinking and the palette, and it moves.
	reddest := func(img *image.Paletted) int {
		for x := range 240 {
			r, g, _, _ := img.At(x, 72).RGBA()
			if r>>8 > 200 && g>>8 < 60 {
				return x
			}
		}
		return -1
	}
	first, last := reddest(g.Image[0]), reddest(g.Image[9])
	if first < 0 || last <= first {
		t.Errorf("red square at %d then %d, want it visible and moving right", first, last)
	}
}

func TestBigAnimationsDropFramesToFit(t *testing.T) {
	data, err := Encode(bytes.NewReader(makeGIF(t, 40, 240, 136, true)))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxBytes {
		t.Fatalf("%d bytes, over the %d limit", len(data), MaxBytes)
	}
}

func TestEncodeRefusesWhatIsNotAGIF(t *testing.T) {
	if _, err := Encode(bytes.NewReader([]byte("hello"))); err != ErrNotGIF {
		t.Fatalf("got %v", err)
	}
}

func TestPreviewRefusesCutOffFiles(t *testing.T) {
	data, err := Encode(bytes.NewReader(makeGIF(t, 3, 120, 68, false)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Preview(data[:len(data)-5]); err == nil {
		t.Fatal("a cut-off file passed")
	}
}

func TestValidHash(t *testing.T) {
	if !ValidHash(Hash([]byte("x"))) || ValidHash("") || ValidHash("../../etc") {
		t.Fatal("wrong answer")
	}
}
