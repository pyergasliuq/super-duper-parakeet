// Package colorcyc generates GTA SA ColorCycle .dat files.
//
// Original Python (main.py:2546-2552):
//
//	with open('BASEcolorcycle.dat', 'r') as f: template_data = f.read()
//	final_data = template_data.replace("r", r).replace("g", g).replace("b", b)
//	with open(grn1, 'w') as f: f.write(final_data)
//
// The template (BASEcolorcycle.dat) contains literal "r", "g", "b" markers
// where the actual color values should be substituted. The Python code
// replaces ALL occurrences of these single letters — including those inside
// numbers like "0.000" which contains no "r"/"g"/"b" by luck, but it's a
// bug if any number contained those letters. We replicate the exact
// behaviour for compatibility.
//
// The values r/g/b passed in are decimal strings like "0.300" representing
// normalized RGB values (0.0 - 1.0). They're computed from a hex color via
// `c / 100` after dividing by 2.55 — but the Python conversion is messy.
// We expose two APIs: Generate(r, g, b string) for raw values and
// GenerateFromHex(hex) for the convenience conversion.
package colorcyc

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed template.dat
var Template string

// Generate replaces "r", "g", "b" in the template with the given values.
//
// IMPORTANT: this matches the Python behaviour exactly, including the buggy
// global replace. If r="0.5", every "r" in the template becomes "0.5" —
// including any that were part of "r0" (the original marker prefix). This
// is intentional for byte-for-byte compat.
func Generate(r, g, b string) string {
	out := Template
	out = strings.ReplaceAll(out, "r", r)
	out = strings.ReplaceAll(out, "g", g)
	out = strings.ReplaceAll(out, "b", b)
	return out
}

// GenerateFromHex takes a "#RRGGBB" color, converts it to three normalized
// decimal strings (R/100, G/100, B/100 — matching the Python conversion
// `c/100` for c in [0,255]), and applies them to the template.
//
// Bug in Python: the conversion was `round(c / 100, 3)` for c in 0..255,
// which produces values in 0.0..2.55 — NOT 0.0..1.0. We replicate for compat.
func GenerateFromHex(hex string) (string, error) {
	rgb, err := hexToRGB(hex)
	if err != nil {
		return "", err
	}
	r := fmt.Sprintf("%.3f", float64(rgb[0])/100.0)
	g := fmt.Sprintf("%.3f", float64(rgb[1])/100.0)
	b := fmt.Sprintf("%.3f", float64(rgb[2])/100.0)
	return Generate(r, g, b), nil
}

// GenerateFromBlack takes a single "black" value (used by /colorcyc 1.2 in
// Python) and applies it to all three channels.
func GenerateFromBlack(black string) (string, error) {
	return Generate(black, black, black), nil
}

func hexToRGB(hex string) ([3]int, error) {
	var rgb [3]int
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return rgb, fmt.Errorf("invalid hex %q", hex)
	}
	for i := 0; i < 3; i++ {
		var v int
		for j := 0; j < 2; j++ {
			c := hex[i*2+j]
			switch {
			case c >= '0' && c <= '9':
				v = v*16 + int(c-'0')
			case c >= 'a' && c <= 'f':
				v = v*16 + int(c-'a'+10)
			case c >= 'A' && c <= 'F':
				v = v*16 + int(c-'A'+10)
			default:
				return rgb, fmt.Errorf("invalid hex char %q", c)
			}
		}
		rgb[i] = v
	}
	return rgb, nil
}
