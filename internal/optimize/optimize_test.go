package optimize

import (
	"image"
	"image/color"
	"image/png"
	"bytes"
	"testing"

	"github.com/chai2010/webp"
)

func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fill with a gradient so it's not trivially compressible.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestForTerminal_Resize(t *testing.T) {
	// Create a 1000x800 PNG.
	data := makePNG(1000, 800)

	result, w, h, err := ForTerminal(data, 480)
	if err != nil {
		t.Fatalf("ForTerminal: %v", err)
	}
	if w != 480 {
		t.Fatalf("width = %d, want 480", w)
	}
	if h != 384 { // 800 * (480/1000) = 384
		t.Fatalf("height = %d, want 384", h)
	}

	// Verify output is valid WebP.
	img, err := webp.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("decode output webp: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 480 || bounds.Dy() != 384 {
		t.Fatalf("output dimensions %dx%d, want 480x384", bounds.Dx(), bounds.Dy())
	}
}

func TestForTerminal_SmallImage(t *testing.T) {
	// Image smaller than maxWidth should not be upscaled.
	data := makePNG(200, 300)

	result, w, h, err := ForTerminal(data, 480)
	if err != nil {
		t.Fatalf("ForTerminal: %v", err)
	}
	if w != 200 {
		t.Fatalf("width = %d, want 200 (no upscale)", w)
	}
	if h != 300 {
		t.Fatalf("height = %d, want 300 (no upscale)", h)
	}

	// Should still be valid WebP.
	if _, err := webp.Decode(bytes.NewReader(result)); err != nil {
		t.Fatalf("decode small image output: %v", err)
	}
}

func TestForTerminal_InvalidData(t *testing.T) {
	_, _, _, err := ForTerminal([]byte("not an image"), 480)
	if err == nil {
		t.Fatal("expected error for invalid image data")
	}
}
