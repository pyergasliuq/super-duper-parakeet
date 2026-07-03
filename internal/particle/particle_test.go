// Package particle — particle_test.go
package particle_test

import (
	"strings"
	"testing"

	"github.com/pweper/bot/internal/particle"
)

func TestGenerate(t *testing.T) {
	out := particle.Generate(particle.Params{
		R:    "255",
		G:    "100",
		B:    "50",
		Size: "10",
	})
	// Verify markers were replaced.
	if strings.Contains(out, "r22") {
		t.Error("r22 not replaced")
	}
	if strings.Contains(out, "g22") {
		t.Error("g22 not replaced")
	}
	if strings.Contains(out, "b22") {
		t.Error("b22 not replaced")
	}
	if strings.Contains(out, "Q11") {
		t.Error("Q11 not replaced")
	}
}

func TestGenerateFromHex(t *testing.T) {
	out, err := particle.GenerateFromHex("#FF6432", "10", "5", "0", "0")
	if err != nil {
		t.Fatalf("GenerateFromHex: %v", err)
	}
	// Color values should appear (255, 100, 50).
	if !strings.Contains(out, "255") {
		t.Error("R=255 not found")
	}
}

func TestGenerateDefaultsUAndR(t *testing.T) {
	// When U and R are empty, they should default to "0".
	out := particle.Generate(particle.Params{
		R:    "255",
		G:    "0",
		B:    "0",
		Size: "5",
	})
	// All U11/R11 should be replaced with "0".
	if strings.Contains(out, "U11") {
		t.Error("U11 not replaced (should default to 0)")
	}
	if strings.Contains(out, "R11") {
		t.Error("R11 not replaced (should default to 0)")
	}
}
