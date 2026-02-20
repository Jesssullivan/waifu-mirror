// Package optimize resizes and converts images for optimal terminal rendering.
// Target format is WebP at max 480px width (portrait) or 480px height (landscape).
// 24-bit color is preserved for halfblocks/Kitty protocol rendering.
package optimize

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"github.com/chai2010/webp"
	"golang.org/x/image/draw"
)

// ForTerminal resizes an image to fit within maxWidth pixels (maintaining
// aspect ratio) and encodes as WebP. Returns the encoded bytes, final
// width, final height, and any error.
func ForTerminal(data []byte, maxWidth int) ([]byte, int, int, error) {
	// Decode the input image.
	img, _, err := decodeImage(data)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("optimize: decode: %w", err)
	}

	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()

	// Calculate target dimensions maintaining aspect ratio.
	newW, newH := origW, origH
	if origW > maxWidth {
		ratio := float64(maxWidth) / float64(origW)
		newW = maxWidth
		newH = int(float64(origH) * ratio)
	}

	// Resize using high-quality Catmull-Rom interpolation.
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	// Encode as WebP.
	var buf bytes.Buffer
	if err := webp.Encode(&buf, dst, &webp.Options{Quality: 85}); err != nil {
		return nil, 0, 0, fmt.Errorf("optimize: encode webp: %w", err)
	}

	return buf.Bytes(), newW, newH, nil
}

// decodeImage tries multiple image formats.
func decodeImage(data []byte) (image.Image, string, error) {
	r := bytes.NewReader(data)

	// Try standard formats first.
	img, format, err := image.Decode(r)
	if err == nil {
		return img, format, nil
	}

	// Try WebP.
	r.Reset(data)
	img, err = webp.Decode(r)
	if err == nil {
		return img, "webp", nil
	}

	// Try individual decoders as fallback.
	r.Reset(data)
	if img, err = png.Decode(r); err == nil {
		return img, "png", nil
	}

	r.Reset(data)
	if img, err = jpeg.Decode(r); err == nil {
		return img, "jpeg", nil
	}

	r.Reset(data)
	if img, err = gif.Decode(r); err == nil {
		return img, "gif", nil
	}

	return nil, "", fmt.Errorf("unsupported image format")
}
