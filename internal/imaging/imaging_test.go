// Package imaging — imaging_test.go
package imaging_test

import (
        "bytes"
        "image"
        "image/color"
        "image/jpeg"
        "image/png"
        "testing"

        "github.com/pweper/bot/internal/imaging"
)

// makeTestImage builds a small NRGBA test image with known colors.
func makeTestImage(t *testing.T, w, h int, c color.NRGBA) []byte {
        t.Helper()
        img := image.NewNRGBA(image.Rect(0, 0, w, h))
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        img.SetNRGBA(x, y, c)
                }
        }
        var buf bytes.Buffer
        if err := png.Encode(&buf, img); err != nil {
                t.Fatalf("encode: %v", err)
        }
        return buf.Bytes()
}

func TestHexToRGB(t *testing.T) {
        r, g, b, err := imaging.HexToRGB("#FF8800")
        if err != nil {
                t.Fatalf("HexToRGB: %v", err)
        }
        if r != 255 || g != 136 || b != 0 {
                t.Errorf("got (%d,%d,%d), want (255,136,0)", r, g, b)
        }
        // Without # prefix.
        r, g, b, _ = imaging.HexToRGB("00FF7F")
        if r != 0 || g != 255 || b != 127 {
                t.Errorf("got (%d,%d,%d), want (0,255,127)", r, g, b)
        }
        // Invalid.
        _, _, _, err = imaging.HexToRGB("#XYZ")
        if err == nil {
                t.Errorf("expected error for invalid hex")
        }
        // Too short.
        _, _, _, err = imaging.HexToRGB("#FF")
        if err == nil {
                t.Errorf("expected error for short hex")
        }
}

func TestRGBToHex(t *testing.T) {
        got := imaging.RGBToHex(255, 136, 0)
        if got != "#FF8800" {
                t.Errorf("got %q, want #FF8800", got)
        }
}

func TestColorWhiteImage(t *testing.T) {
        // Recolor a pure-white image with #FF0000 → result should be reddish.
        data := makeTestImage(t, 4, 4, color.NRGBA{255, 255, 255, 255})
        out, err := imaging.Color(imaging.MustDecode(data), "#FF0000", 1.0)
        if err != nil {
                t.Fatalf("Color: %v", err)
        }
        img, err := imaging.DecodeImageForTest(out)
        if err != nil {
                t.Fatalf("decode: %v", err)
        }
        c := img.NRGBAAt(0, 0)
        // White recolored with red at full alpha should be (255, ~0, ~0).
        if c.R < 200 {
                t.Errorf("R = %d, want >= 200", c.R)
        }
        if c.G > 50 {
                t.Errorf("G = %d, want <= 50", c.G)
        }
        if c.B > 50 {
                t.Errorf("B = %d, want <= 50", c.B)
        }
}

func TestColorAlphaZero(t *testing.T) {
        // alpha=0 → image unchanged.
        orig := color.NRGBA{100, 150, 200, 255}
        data := makeTestImage(t, 2, 2, orig)
        out, err := imaging.Color(imaging.MustDecode(data), "#FF0000", 0)
        if err != nil {
                t.Fatalf("Color: %v", err)
        }
        img, _ := imaging.DecodeImageForTest(out)
        c := img.NRGBAAt(0, 0)
        if c != orig {
                t.Errorf("alpha=0 changed pixel: got %v, want %v", c, orig)
        }
}

func TestRecolorBasic(t *testing.T) {
        // Replace red pixels with green.
        img := image.NewNRGBA(image.Rect(0, 0, 4, 1))
        img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})   // red - match
        img.SetNRGBA(1, 0, color.NRGBA{0, 255, 0, 255})   // green - no match
        img.SetNRGBA(2, 0, color.NRGBA{250, 5, 5, 255})   // near-red - match (tol 10)
        img.SetNRGBA(3, 0, color.NRGBA{200, 100, 100, 255}) // pinkish - no match

        out, err := imaging.Recolor(img, "#FF0000", "#00FF00", 10)
        if err != nil {
                t.Fatalf("Recolor: %v", err)
        }
        result, _ := imaging.DecodeImageForTest(out)

        // Pixel 0: red → green.
        c := result.NRGBAAt(0, 0)
        if c.R != 0 || c.G != 255 || c.B != 0 {
                t.Errorf("pixel 0: got %v, want green", c)
        }
        // Pixel 1: green unchanged.
        c = result.NRGBAAt(1, 0)
        if c.R != 0 || c.G != 255 || c.B != 0 {
                t.Errorf("pixel 1: got %v, want green (unchanged)", c)
        }
        // Pixel 2: near-red → green.
        c = result.NRGBAAt(2, 0)
        if c.R != 0 || c.G != 255 || c.B != 0 {
                t.Errorf("pixel 2: got %v, want green", c)
        }
        // Pixel 3: pinkish unchanged.
        c = result.NRGBAAt(3, 0)
        if c.R != 200 || c.G != 100 || c.B != 100 {
                t.Errorf("pixel 3: got %v, want unchanged pinkish", c)
        }
}

func TestRecolorNoneMakesTransparent(t *testing.T) {
        img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
        img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
        img.SetNRGBA(1, 0, color.NRGBA{0, 255, 0, 255})

        out, err := imaging.Recolor(img, "#FF0000", "none", 5)
        if err != nil {
                t.Fatalf("Recolor: %v", err)
        }
        result, _ := imaging.DecodeImageForTest(out)

        c := result.NRGBAAt(0, 0)
        if c.A != 0 {
                t.Errorf("pixel 0 alpha = %d, want 0 (transparent)", c.A)
        }
        c = result.NRGBAAt(1, 0)
        if c.A != 255 {
                t.Errorf("pixel 1 alpha = %d, want 255", c.A)
        }
}

func TestFilterGrayscale(t *testing.T) {
        img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
        img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
        out, err := imaging.Filter(img, "grayscale", 50)
        if err != nil {
                t.Fatalf("Filter: %v", err)
        }
        result, _ := imaging.DecodeImageForTest(out)
        c := result.NRGBAAt(0, 0)
        // Red's luminance is ~76.
        if c.R != c.G || c.G != c.B {
                t.Errorf("grayscale pixel should have R=G=B, got %v", c)
        }
}

func TestFilterNegate(t *testing.T) {
        img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
        img.SetNRGBA(0, 0, color.NRGBA{100, 150, 200, 255})
        out, err := imaging.Filter(img, "negate", 0)
        if err != nil {
                t.Fatalf("Filter: %v", err)
        }
        result, _ := imaging.DecodeImageForTest(out)
        c := result.NRGBAAt(0, 0)
        if c.R != 155 || c.G != 105 || c.B != 55 {
                t.Errorf("negate: got %v, want (155,105,55)", c)
        }
}

func TestFilterUnknown(t *testing.T) {
        img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
        _, err := imaging.Filter(img, "nonexistent", 50)
        if err == nil {
                t.Errorf("expected error for unknown filter")
        }
}

func TestCompressResize(t *testing.T) {
        data := makeTestImage(t, 100, 100, color.NRGBA{255, 0, 0, 255})
        out, format, err := imaging.Compress(data, 50, 50, "png")
        if err != nil {
                t.Fatalf("Compress: %v", err)
        }
        if format != "png" {
                t.Errorf("format = %q, want png", format)
        }
        img, _ := imaging.DecodeImageForTest(out)
        if img.Rect.Dx() != 50 || img.Rect.Dy() != 50 {
                t.Errorf("size = %dx%d, want 50x50", img.Rect.Dx(), img.Rect.Dy())
        }
}

func TestCompressJPEGFormat(t *testing.T) {
        // Build a JPEG input.
        rgb := image.NewRGBA(image.Rect(0, 0, 100, 100))
        for y := 0; y < 100; y++ {
                for x := 0; x < 100; x++ {
                        rgb.SetRGBA(x, y, color.RGBA{255, 128, 0, 255})
                }
        }
        var buf bytes.Buffer
        if err := jpeg.Encode(&buf, rgb, &jpeg.Options{Quality: 85}); err != nil {
                t.Fatalf("jpeg encode: %v", err)
        }
        out, format, err := imaging.Compress(buf.Bytes(), 50, 50, "jpg")
        if err != nil {
                t.Fatalf("Compress: %v", err)
        }
        if format != "jpg" {
                t.Errorf("format = %q, want jpg", format)
        }
        if len(out) == 0 {
                t.Errorf("Compress returned empty output")
        }
}

func TestAimProduces4x(t *testing.T) {
        // 2x2 input → 4x4 output.
        data := makeTestImage(t, 2, 2, color.NRGBA{255, 0, 0, 255})
        out, err := imaging.Aim(data)
        if err != nil {
                t.Fatalf("Aim: %v", err)
        }
        img, _ := imaging.DecodeImageForTest(out)
        if img.Rect.Dx() != 4 || img.Rect.Dy() != 4 {
                t.Errorf("aim output = %dx%d, want 4x4", img.Rect.Dx(), img.Rect.Dy())
        }
        // All 16 pixels should be red (input was solid red).
        for y := 0; y < 4; y++ {
                for x := 0; x < 4; x++ {
                        c := img.NRGBAAt(x, y)
                        if c.R != 255 || c.G != 0 || c.B != 0 {
                                t.Errorf("pixel (%d,%d) = %v, want red", x, y, c)
                        }
                }
        }
}

func TestSplitRadar(t *testing.T) {
        // 14×14 tiles, each tile 10×10 → input 140×140.
        data := makeTestImage(t, 140, 140, color.NRGBA{0, 128, 255, 255})
        tiles, err := imaging.SplitRadar(data, "png")
        if err != nil {
                t.Fatalf("SplitRadar: %v", err)
        }
        if len(tiles) != 196 {
                t.Fatalf("tiles count = %d, want 196", len(tiles))
        }
        if tiles[0].Name != "radar00.png" {
                t.Errorf("first tile name = %q", tiles[0].Name)
        }
        if tiles[195].Name != "radar195.png" {
                t.Errorf("last tile name = %q", tiles[195].Name)
        }
}

func TestOverlayMultiply(t *testing.T) {
        // White base * red overlay = red result.
        base := makeTestImage(t, 2, 2, color.NRGBA{255, 255, 255, 255})
        overlay := makeTestImage(t, 2, 2, color.NRGBA{255, 0, 0, 255})
        out, err := imaging.Overlay(base, overlay, imaging.OverlayMultiply, 100)
        if err != nil {
                t.Fatalf("Overlay: %v", err)
        }
        img, _ := imaging.DecodeImageForTest(out)
        c := img.NRGBAAt(0, 0)
        if c.R != 255 || c.G != 0 || c.B != 0 {
                t.Errorf("overlay multiply: got %v, want red", c)
        }
}
