// Package txd — txd_test.go
//
// Tests use synthetic data (we don't have a real .txd fixture yet) to verify
// the decoders produce correct output for known input patterns.
package txd_test

import (
	"encoding/binary"
	"image/color"
	"testing"

	"github.com/pweper/bot/internal/txd"
)

func TestUnpack565(t *testing.T) {
	// 0xFFFF in 565 = (31, 63, 31) → (255, 255, 255) after bit replication.
	r, g, b, a := txd.Unpack565ForTest(0xFFFF)
	if r != 255 || g != 255 || b != 255 || a != 255 {
		t.Errorf("unpack565(0xFFFF) = (%d,%d,%d,%d), want (255,255,255,255)", r, g, b, a)
	}
	// 0x0000 = (0, 0, 0) → (0, 0, 0).
	r, g, b, _ = txd.Unpack565ForTest(0x0000)
	if r != 0 || g != 0 || b != 0 {
		t.Errorf("unpack565(0) = (%d,%d,%d), want (0,0,0)", r, g, b)
	}
}

func TestDecodeDXT1SolidColor(t *testing.T) {
	// Build a single 4x4 DXT1 block where both endpoints are white (0xFFFF)
	// and the lookup table is all 0 (use color 0 = white).
	// Block: c0=0xFFFF, c1=0xFFFF, bits=0 → all pixels = c0 = white.
	block := make([]byte, 8)
	binary.LittleEndian.PutUint16(block[0:], 0xFFFF)
	binary.LittleEndian.PutUint16(block[2:], 0xFFFF)
	binary.LittleEndian.PutUint32(block[4:], 0)

	// Build a 4x4 image.
	textures, err := txd.DecodeSingleBlockForTest("DXT1", block, 4, 4)
	if err != nil {
		t.Fatalf("DecodeSingleBlock: %v", err)
	}
	img := textures[0].Image
	// All 16 pixels should be white (255,255,255,255).
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			c := img.NRGBAAt(x, y)
			if c.R != 255 || c.G != 255 || c.B != 255 || c.A != 255 {
				t.Errorf("pixel (%d,%d) = %v, want white", x, y, c)
			}
		}
	}
}

func TestDecodeDXT1TwoColors(t *testing.T) {
	// Build a block where c0 = red (0xF800 in 565: 31,0,0), c1 = green
	// (0x07E0: 0,63,0). c0 > c1 so c2 = 2/3 red + 1/3 green, c3 = 1/3 red +
	// 2/3 green. Lookup table alternates: first 8 pixels = 0 (c0), next 8
	// = 1 (c1).
	block := make([]byte, 8)
	binary.LittleEndian.PutUint16(block[0:], 0xF800) // red
	binary.LittleEndian.PutUint16(block[2:], 0x07E0) // green
	// bits: pixels 0-7 use index 0, pixels 8-15 use index 1.
	// Each pixel is 2 bits. Index 0 = 00, index 1 = 01.
	// So bits = 01 01 01 01 01 01 01 01 00 00 00 00 00 00 00 00 (MSB first).
	// But the lookup is little-endian: pixel 0 is bits[1:0], pixel 1 is bits[3:2], etc.
	// So we want the low 16 bits (pixels 0-7) = 0, and the high 16 bits
	// (pixels 8-15) = 0x5555 (alternating 01).
	binary.LittleEndian.PutUint32(block[4:], 0x55550000)

	textures, err := txd.DecodeSingleBlockForTest("DXT1", block, 4, 4)
	if err != nil {
		t.Fatalf("DecodeSingleBlock: %v", err)
	}
	img := textures[0].Image

	// Pixel (0,0) should be c0 = red (255, 0, 0, 255).
	c := img.NRGBAAt(0, 0)
	if c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("pixel (0,0) = %v, want red 255,0,0", c)
	}
	// Pixel (0,2) should be c1 = green (0, 255, 0, 255).
	c = img.NRGBAAt(0, 2)
	if c.R != 0 || c.G != 255 || c.B != 0 {
		t.Errorf("pixel (0,2) = %v, want green 0,255,0", c)
	}
}

func TestDecodeRGB888(t *testing.T) {
	// Build a 2x2 RGB888 image with 4 different colors.
	data := []byte{
		255, 0, 0,     // (0,0) red
		0, 255, 0,     // (1,0) green
		0, 0, 255,     // (0,1) blue
		255, 255, 255, // (1,1) white
	}
	textures, err := txd.DecodeSingleBlockForTest("RGB888", data, 2, 2)
	if err != nil {
		t.Fatalf("DecodeSingleBlock: %v", err)
	}
	img := textures[0].Image

	expected := []color.NRGBA{
		{255, 0, 0, 255},
		{0, 255, 0, 255},
		{0, 0, 255, 255},
		{255, 255, 255, 255},
	}
	positions := []struct{ x, y int }{{0, 0}, {1, 0}, {0, 1}, {1, 1}}
	for i, p := range positions {
		got := img.NRGBAAt(p.x, p.y)
		want := expected[i]
		if got != want {
			t.Errorf("pixel (%d,%d) = %v, want %v", p.x, p.y, got, want)
		}
	}
}

func TestDecodeBGRA8888(t *testing.T) {
	// 1x1 BGRA = (10, 20, 30, 40) → RGBA = (30, 20, 10, 40).
	data := []byte{10, 20, 30, 40}
	textures, err := txd.DecodeSingleBlockForTest("BGRA8888", data, 1, 1)
	if err != nil {
		t.Fatalf("DecodeSingleBlock: %v", err)
	}
	c := textures[0].Image.NRGBAAt(0, 0)
	want := color.NRGBA{30, 20, 10, 40}
	if c != want {
		t.Errorf("BGRA pixel = %v, want %v", c, want)
	}
}

func TestDecodeBGR888BlackTransparent(t *testing.T) {
	// BGR888: black pixels become transparent.
	data := []byte{
		0, 0, 0,       // (0,0) black → alpha 0
		255, 255, 255, // (1,0) white → alpha 255
	}
	textures, err := txd.DecodeSingleBlockForTest("BGR888", data, 2, 1)
	if err != nil {
		t.Fatalf("DecodeSingleBlock: %v", err)
	}
	img := textures[0].Image
	c0 := img.NRGBAAt(0, 0)
	if c0.A != 0 {
		t.Errorf("black pixel alpha = %d, want 0", c0.A)
	}
	c1 := img.NRGBAAt(1, 0)
	if c1.A != 255 {
		t.Errorf("white pixel alpha = %d, want 255", c1.A)
	}
}

func TestParseEmpty(t *testing.T) {
	// Empty data should return nil textures, no error.
	textures, err := txd.Parse([]byte{})
	if err != nil {
		t.Errorf("Parse(empty) returned error: %v", err)
	}
	if textures != nil && len(textures) != 0 {
		t.Errorf("Parse(empty) returned %d textures, want 0", len(textures))
	}
}
