// Package timecyc — timecyc_test.go
package timecyc_test

import (
	"strings"
	"testing"

	"github.com/pweper/bot/internal/timecyc"
)

func TestHexToRGB(t *testing.T) {
	rgb, err := timecyc.HexToRGB("#FF8800")
	if err != nil {
		t.Fatalf("HexToRGB: %v", err)
	}
	if rgb != [3]int{255, 136, 0} {
		t.Errorf("got %v, want [255 136 0]", rgb)
	}
}

func TestGenerate(t *testing.T) {
	out := timecyc.Generate(timecyc.Colors{
		SkyBottomRGB: [3]int{10, 20, 30},
		SkyTopRGB:    [3]int{40, 50, 60},
		CloudRGB:     [3]int{70, 80, 90},
		SunCoreRGB:   [3]int{100, 110, 120},
	})
	// Verify the placeholders were replaced.
	if strings.Contains(out, "[SBR016]") {
		t.Error("SkyBottomRGB placeholder not replaced")
	}
	if strings.Contains(out, "[STR016]") {
		t.Error("SkyTopRGB placeholder not replaced")
	}
	if strings.Contains(out, "[CLR016]") {
		t.Error("CloudRGB placeholder not replaced")
	}
	if strings.Contains(out, "[SCR016]") {
		t.Error("SunCoreRGB placeholder not replaced")
	}
	// Verify the values were inserted.
	if !strings.Contains(out, `"SkyBottomRGB":[10,20,30]`) {
		t.Error("SkyBottomRGB value not inserted correctly")
	}
}

func TestGenerateFromHexes(t *testing.T) {
	out, err := timecyc.GenerateFromHexes("#0A141E", "#28323C", "#46505A", "#646E78")
	if err != nil {
		t.Fatalf("GenerateFromHexes: %v", err)
	}
	if !strings.Contains(out, `"SkyBottomRGB":[10,20,30]`) {
		t.Error("HexToRGB conversion + insertion failed")
	}
}

func TestGenerateFromHexesInvalidHex(t *testing.T) {
	_, err := timecyc.GenerateFromHexes("#XYZ", "#000000", "#000000", "#000000")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}
