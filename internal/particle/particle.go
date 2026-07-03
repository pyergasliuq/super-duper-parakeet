// Package particle generates GTA SA particle.cfg files.
//
// Original Python (main.py:5551-5571):
//
//	with open('particleCH.cfg', 'r') as infile: t = infile.read()
//	t = t.replace("r22", r_val).replace("g22", g_val).replace("b22", b_val)
//	if len(j) > 2:
//	    t = t.replace("Q11", j[2]).replace("U11", j[4] if len(j) > 4 else "0")
//	      .replace("R11", j[5] if len(j) > 5 else "0").replace("T11", j[3] if len(j) > 3 else "0")
//	with open(grn1_path, 'w') as outfile: outfile.write(t)
//
// The template (particleCH.cfg) contains markers r22/g22/b22 (the blood color)
// and Q11/T11/U11/R11 (the particle size and other params).
//
// We replicate the same string-replace logic for byte-for-byte compat.
package particle

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed template.cfg
var Template string

// Params holds the substitution values.
type Params struct {
	R, G, B   string // color values (RGB strings, e.g. "255")
	Size      string // particle size (replaces Q11)
	TrailLen  string // trail length (replaces T11)
	UVal      string // U value (replaces U11)
	RVal      string // R value (replaces R11)
}

// Generate applies the params to the template and returns the .cfg content.
func Generate(p Params) string {
	out := Template
	out = strings.ReplaceAll(out, "r22", p.R)
	out = strings.ReplaceAll(out, "g22", p.G)
	out = strings.ReplaceAll(out, "b22", p.B)
	if p.Size != "" {
		out = strings.ReplaceAll(out, "Q11", p.Size)
	}
	if p.TrailLen != "" {
		out = strings.ReplaceAll(out, "T11", p.TrailLen)
	}
	if p.UVal != "" {
		out = strings.ReplaceAll(out, "U11", p.UVal)
	} else {
		out = strings.ReplaceAll(out, "U11", "0")
	}
	if p.RVal != "" {
		out = strings.ReplaceAll(out, "R11", p.RVal)
	} else {
		out = strings.ReplaceAll(out, "R11", "0")
	}
	return out
}

// GenerateFromHex takes a hex color and optional size/trail/u/r values.
// hexColor is "#RRGGBB"; size/trail/u/r are decimal strings.
func GenerateFromHex(hexColor, size, trail, u, r string) (string, error) {
	rgb, err := hexToRGB(hexColor)
	if err != nil {
		return "", err
	}
	return Generate(Params{
		R:        fmt.Sprintf("%d", rgb[0]),
		G:        fmt.Sprintf("%d", rgb[1]),
		B:        fmt.Sprintf("%d", rgb[2]),
		Size:     size,
		TrailLen: trail,
		UVal:     u,
		RVal:     r,
	}), nil
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
