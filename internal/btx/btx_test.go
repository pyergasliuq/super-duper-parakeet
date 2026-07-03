// Package btx — btx_test.go
//
// Tests use the real astcenc binary (must be on $PATH or at /home/z/bin/astcenc).
// We test all 16 quality × speed combinations on a small synthetic image.
//
// BUG FIX: previous tests verified pow2 padding + mip chain. The new
// implementation matches Python _compress_to_btx_bytes: single mip, no
// padding, original dimensions preserved.
package btx_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os/exec"
	"testing"

	"github.com/pweper/bot/internal/btx"
)

func hasAstcenc(t *testing.T) bool {
	t.Helper()
	paths := []string{"astcenc", "/home/z/bin/astcenc", "/usr/local/bin/astcenc"}
	for _, p := range paths {
		cmd := exec.Command(p, "-v")
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x * 255) / w),
				G: uint8((y * 255) / h),
				B: uint8((x + y) * 8 % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	if !hasAstcenc(t) {
		t.Skip("astcenc not installed; skipping")
	}
	pngData := makeTestPNG(t, 32, 32)
	enc := btx.NewEncoder(btx.Config{AstcencPath: "/home/z/bin/astcenc", Threads: 2})

	btxData, err := enc.EncodePNG(pngData, btx.QualityBalanced, btx.SpeedFast)
	if err != nil {
		t.Fatalf("EncodePNG: %v", err)
	}
	// Check BTX magic.
	if btxData[0] != 0x02 || btxData[1] != 0x00 || btxData[2] != 0x00 || btxData[3] != 0x00 {
		t.Errorf("btx magic = %x, want 02000000", btxData[:4])
	}

	decPNG, err := enc.DecodeBTX(btxData)
	if err != nil {
		t.Fatalf("DecodeBTX: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(decPNG))
	if err != nil {
		t.Fatalf("decoded PNG is invalid: %v", err)
	}
	if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
		t.Errorf("decoded size = %dx%d, want 32x32", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

// TestNoPow2Padding verifies that non-power-of-2 dimensions are preserved.
// This is the key bug fix: Python doesn't pad to pow2.
func TestNoPow2Padding(t *testing.T) {
	if !hasAstcenc(t) {
		t.Skip("astcenc not installed; skipping")
	}
	// 100×100 — not a power of 2.
	pngData := makeTestPNG(t, 100, 100)
	enc := btx.NewEncoder(btx.Config{AstcencPath: "/home/z/bin/astcenc", Threads: 2})

	btxData, err := enc.EncodePNG(pngData, btx.QualityLowWeight, btx.SpeedFast)
	if err != nil {
		t.Fatalf("EncodePNG: %v", err)
	}

	decPNG, err := enc.DecodeBTX(btxData)
	if err != nil {
		t.Fatalf("DecodeBTX: %v", err)
	}
	img, _ := png.Decode(bytes.NewReader(decPNG))
	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 100 {
		t.Errorf("decoded size = %dx%d, want 100x100 (no pow2 padding)",
			img.Bounds().Dx(), img.Bounds().Dy())
	}
}

// TestSingleMipLevel verifies the KTX1 header has numMipLevels=1.
// This is the key bug fix: Python produces single-mip BTX files.
func TestSingleMipLevel(t *testing.T) {
	if !hasAstcenc(t) {
		t.Skip("astcenc not installed; skipping")
	}
	pngData := makeTestPNG(t, 64, 64)
	enc := btx.NewEncoder(btx.Config{AstcencPath: "/home/z/bin/astcenc", Threads: 2})

	btxData, err := enc.EncodePNG(pngData, btx.QualityBalanced, btx.SpeedFast)
	if err != nil {
		t.Fatalf("EncodePNG: %v", err)
	}

	// Parse KTX1 header (skip BTX magic + KTX1 magic).
	// Offset 0: BTX magic (4 bytes)
	// Offset 4: KTX1 magic (12 bytes)
	// Offset 16: 13 uint32 fields
	// numMipLevels is field[11] (0-indexed) at offset 16 + 11*4 = 60.
	if len(btxData) < 64 {
		t.Fatalf("btx data too short: %d", len(btxData))
	}
	numMipLevels := uint32(btxData[60]) | uint32(btxData[61])<<8 |
		uint32(btxData[62])<<16 | uint32(btxData[63])<<24
	if numMipLevels != 1 {
		t.Errorf("numMipLevels = %d, want 1 (Python produces single-mip BTX)", numMipLevels)
	}
}

// TestFileSizeMatchesPython verifies the output size matches what Python
// _compress_to_btx_bytes would produce for the same input.
//
// For a 64×64 image with 8×8 block:
//   blocks = (64/8) × (64/8) = 8 × 8 = 64
//   astc_data = 64 × 16 = 1024 bytes
//   total = 4 (BTX) + 12 (KTX magic) + 52 (header) + 4 (mip size) + 1024 = 1096 bytes
func TestFileSizeMatchesPython(t *testing.T) {
	if !hasAstcenc(t) {
		t.Skip("astcenc not installed; skipping")
	}
	pngData := makeTestPNG(t, 64, 64)
	enc := btx.NewEncoder(btx.Config{AstcencPath: "/home/z/bin/astcenc", Threads: 2})

	btxData, err := enc.EncodePNG(pngData, btx.QualityLowWeight, btx.SpeedFast)
	if err != nil {
		t.Fatalf("EncodePNG: %v", err)
	}

	// Expected: 4 + 12 + 52 + 4 + 1024 = 1096 bytes.
	expectedMin := 4 + 12 + 52 + 4 + 1024
	if len(btxData) < expectedMin {
		t.Errorf("file size = %d, want at least %d (Python equivalent)", len(btxData), expectedMin)
	}
	// Should NOT have extra mip data (would be ~2-3× larger).
	expectedMax := expectedMin + 100 // small tolerance
	if len(btxData) > expectedMax {
		t.Errorf("file size = %d, want <= %d (no mip chain, no padding)", len(btxData), expectedMax)
	}
}

func TestAll16Combinations(t *testing.T) {
	if !hasAstcenc(t) {
		t.Skip("astcenc not installed; skipping")
	}
	pngData := makeTestPNG(t, 32, 32)
	enc := btx.NewEncoder(btx.Config{AstcencPath: "/home/z/bin/astcenc", Threads: 2})

	for _, q := range btx.AllQualityValues() {
		for _, s := range btx.AllSpeedValues() {
			t.Run(string(q)+"_"+string(s), func(t *testing.T) {
				btxData, err := enc.EncodePNG(pngData, q, s)
				if err != nil {
					t.Fatalf("EncodePNG(%s,%s): %v", q, s, err)
				}
				if len(btxData) < 16 {
					t.Fatalf("btx data too short for (%s,%s): %d bytes", q, s, len(btxData))
				}
			})
		}
	}
}

func TestQualityLabels(t *testing.T) {
	cases := []struct {
		q    btx.Quality
		want string
	}{
		{btx.QualityAuto, "Авто (умный подбор)"},
		{btx.QualityMaxQuality, "Максимальное качество (4×4)"},
	}
	for _, c := range cases {
		got := btx.QualityLabel(c.q)
		if got != c.want {
			t.Errorf("QualityLabel(%q) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestSpeedLabels(t *testing.T) {
	cases := []struct {
		s    btx.Speed
		want string
	}{
		{btx.SpeedAuto, "Авто (подбор по размеру)"},
		{btx.SpeedMaxQuality, "Максимальное качество (-thorough)"},
	}
	for _, c := range cases {
		got := btx.SpeedLabel(c.s)
		if got != c.want {
			t.Errorf("SpeedLabel(%q) = %q, want %q", c.s, got, c.want)
		}
	}
}

// TestDecodePythonGeneratedFile verifies we can decode the file that Python
// _compress_to_btx_bytes produces (and the user uploaded as "correct").
func TestDecodePythonGeneratedFile(t *testing.T) {
	if !hasAstcenc(t) {
		t.Skip("astcenc not installed; skipping")
	}
	// This test only runs if the user-uploaded "correct" BTX file is available.
	path := "/home/z/my-project/upload/photo_2026-03-02_01-08-43 (2).btx"
	data, err := readFileIfExists(path)
	if err != nil {
		t.Skipf("correct BTX file not found at %s: %v", path, err)
	}
	enc := btx.NewEncoder(btx.Config{AstcencPath: "/home/z/bin/astcenc", Threads: 2})
	pngData, err := enc.DecodeBTX(data)
	if err != nil {
		t.Fatalf("DecodeBTX failed on Python-generated file: %v", err)
	}
	if len(pngData) == 0 {
		t.Errorf("decoded PNG is empty")
	}
	// Verify it's a valid PNG.
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("decoded PNG is invalid: %v", err)
	}
	// The correct file is 640×640 (8×8 block, single mip).
	if img.Bounds().Dx() != 640 || img.Bounds().Dy() != 640 {
		t.Errorf("decoded size = %dx%d, want 640x640", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func readFileIfExists(path string) ([]byte, error) {
	cmd := exec.Command("cat", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
