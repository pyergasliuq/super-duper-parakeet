// Package colorcyc — colorcyc_test.go
package colorcyc_test

import (
	"strings"
	"testing"

	"github.com/pweper/bot/internal/colorcyc"
)

func TestGenerate(t *testing.T) {
	out := colorcyc.Generate("0.500", "0.250", "0.750")
	if !strings.Contains(out, "0.500") {
		t.Error("r value not substituted")
	}
	if !strings.Contains(out, "0.250") {
		t.Error("g value not substituted")
	}
	if !strings.Contains(out, "0.750") {
		t.Error("b value not substituted")
	}
}

func TestGenerateFromHex(t *testing.T) {
	// #FF0000 = (255, 0, 0) → r="2.550", g="0.000", b="0.000" (matches
	// Python's c/100 conversion).
	out, err := colorcyc.GenerateFromHex("#FF0000")
	if err != nil {
		t.Fatalf("GenerateFromHex: %v", err)
	}
	if !strings.Contains(out, "2.550") {
		t.Error("r=2.550 not found in output")
	}
}

func TestGenerateFromBlack(t *testing.T) {
	out, err := colorcyc.GenerateFromBlack("1.200")
	if err != nil {
		t.Fatalf("GenerateFromBlack: %v", err)
	}
	if !strings.Contains(out, "1.200") {
		t.Error("value not substituted")
	}
}
