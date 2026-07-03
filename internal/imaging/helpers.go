// Package imaging — helpers.go — small exported wrappers for tests.
package imaging

import (
        "image"
        "image/color"
)

// MustDecode decodes image bytes; panics on error. For test convenience.
func MustDecode(data []byte) *image.NRGBA {
        img, err := DecodeImage(data)
        if err != nil {
                panic(err)
        }
        return img
}

// DecodeImageForTest is the same as DecodeImage but with a distinct name so
// test files that import the package as imaging_test can use it without
// collision with internal callers.
func DecodeImageForTest(data []byte) (*image.NRGBA, error) {
        return DecodeImage(data)
}

// MakeColorImage creates a solid-color NRGBA image of the given size.
// Used by /checkcolor and /randcolor commands.
func MakeColorImage(w, h int, c color.NRGBA) *image.NRGBA {
        img := image.NewNRGBA(image.Rect(0, 0, w, h))
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        img.SetNRGBA(x, y, c)
                }
        }
        return img
}
