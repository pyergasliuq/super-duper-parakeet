// Package txd — test_helpers.go
//
// Exported wrappers for internal functions, used by txd_test.go (external
// test package). Wrappers are placed in a *_test_helpers.go file so they're
// easy to delete if we ever change the test strategy.
package txd

import (
	"image"
	"image/color"
)

// Unpack565ForTest exposes unpack565 for testing.
func Unpack565ForTest(c uint16) (r, g, b, a uint8) {
	return unpack565(c)
}

// Unpack5551ForTest exposes unpack5551 for testing.
func Unpack5551ForTest(c uint16) (r, g, b, a uint8) {
	return unpack5551(c)
}

// Unpack4444ForTest exposes unpack4444 for testing.
func Unpack4444ForTest(c uint16) (r, g, b, a uint8) {
	return unpack4444(c)
}

// DecodeSingleBlockForTest decodes a single block of the given format and
// returns the resulting Texture. Used by tests to verify decoders without
// having to build a full .txd container.
func DecodeSingleBlockForTest(format string, data []byte, w, h int) ([]Texture, error) {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	// We need to call the internal decodeTexture dispatcher, but it's not
	// exported. Instead, we replicate the dispatch logic here.
	switch format {
	case "DXT1":
		decodeDXT1(data, img, w, h)
	case "DXT3":
		decodeDXT3(data, img, w, h)
	case "DXT5":
		decodeDXT5(data, img, w, h)
	case "RGB888":
		decodeRGB888(data, img, w, h)
	case "BGRA8888", "BGRA":
		decodeBGRA8888(data, img, w, h)
	case "BGR888":
		decodeBGR888(data, img, w, h)
	case "BGR565":
		decodeBGR565(data, img, w, h)
	case "RGB565":
		decodeRGB565(data, img, w, h)
	default:
		// Fall through to the main dispatcher for less common formats.
		img = decodeTexture(format, 0, data, w, h, nil)
		if img == nil {
			return nil, nil
		}
	}
	return []Texture{{
		Name:   "test",
		Format: format,
		Width:  w,
		Height: h,
		Image:  img,
	}}, nil
}

// SetPixelForTest exposes setPixel for testing.
func SetPixelForTest(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	setPixel(img, x, y, w, h, c)
}
