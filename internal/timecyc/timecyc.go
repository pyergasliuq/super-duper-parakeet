// Package timecyc generates GTA SA / Black Russia timecyc.json files.
//
// Two commands use this package:
//   - /timecyc #sky_bot #sky_top #cloud #sun — manual colors.
//   - /aitimecyc <description> — AI-generated colors via the ai package.
//
// The template (main.json) contains placeholders:
//   "SkyBottomRGB":[SBR016]   ← replaced with [R,G,B]
//   "SkyTopRGB":[STR016]
//   "CloudRGB":[CLR016]
//   "SunCoreRGB":[SCR016]
//
// Original Python (main.py:2554-2606) used string .replace(); we do the same
// for byte-for-byte compatibility.
package timecyc

import (
	_ "embed"
	"fmt"
	"strings"
)

// Template is the embedded main.json asset with placeholders.
//
//go:embed template.json
var Template string

// Colors holds the 4 sky/sun colors for timecyc generation.
type Colors struct {
	SkyBottomRGB [3]int // [R, G, B] 0-255
	SkyTopRGB    [3]int
	CloudRGB     [3]int
	SunCoreRGB   [3]int
}

// Generate applies the colors to the template and returns the resulting JSON.
func Generate(c Colors) string {
	out := Template
	out = strings.Replace(out, `"SkyBottomRGB":[SBR016]`, fmt.Sprintf(`"SkyBottomRGB":[%d,%d,%d]`, c.SkyBottomRGB[0], c.SkyBottomRGB[1], c.SkyBottomRGB[2]), -1)
	out = strings.Replace(out, `"SkyTopRGB":[STR016]`, fmt.Sprintf(`"SkyTopRGB":[%d,%d,%d]`, c.SkyTopRGB[0], c.SkyTopRGB[1], c.SkyTopRGB[2]), -1)
	out = strings.Replace(out, `"CloudRGB":[CLR016]`, fmt.Sprintf(`"CloudRGB":[%d,%d,%d]`, c.CloudRGB[0], c.CloudRGB[1], c.CloudRGB[2]), -1)
	out = strings.Replace(out, `"SunCoreRGB":[SCR016]`, fmt.Sprintf(`"SunCoreRGB":[%d,%d,%d]`, c.SunCoreRGB[0], c.SunCoreRGB[1], c.SunCoreRGB[2]), -1)
	return out
}

// HexToRGB parses "#RRGGBB" → [3]int.
func HexToRGB(hex string) ([3]int, error) {
	var rgb [3]int
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return rgb, fmt.Errorf("invalid hex %q (want #RRGGBB)", hex)
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

// GenerateFromHexes takes 4 hex strings and returns the JSON.
// Convenience wrapper for /timecyc command.
func GenerateFromHexes(skyBot, skyTop, cloud, sun string) (string, error) {
	sb, err := HexToRGB(skyBot)
	if err != nil {
		return "", fmt.Errorf("sky_bottom: %w", err)
	}
	st, err := HexToRGB(skyTop)
	if err != nil {
		return "", fmt.Errorf("sky_top: %w", err)
	}
	cl, err := HexToRGB(cloud)
	if err != nil {
		return "", fmt.Errorf("cloud: %w", err)
	}
	su, err := HexToRGB(sun)
	if err != nil {
		return "", fmt.Errorf("sun: %w", err)
	}
	return Generate(Colors{
		SkyBottomRGB: sb,
		SkyTopRGB:    st,
		CloudRGB:     cl,
		SunCoreRGB:   su,
	}), nil
}

// AIColors is the JSON shape returned by the AI for /aitimecyc.
// Includes all 4 sky/sun colors plus optional Ambient/Directional/Far/Fog.
type AIColors struct {
	SkyBottomRGB  [3]int   `json:"SkyBottomRGB"`
	SkyTopRGB     [3]int   `json:"SkyTopRGB"`
	CloudRGB      [3]int   `json:"CloudRGB"`
	SunCoreRGB    [3]int   `json:"SunCoreRGB"`
	AmbientRGB    [3]int   `json:"AmbientRGB,omitempty"`
	DirectionalRGB [3]int  `json:"DirectionalRGB,omitempty"`
	FarClip       float64  `json:"FarClip,omitempty"`
	FogStart      float64  `json:"FogStart,omitempty"`
}

// GenerateFromAI applies AI-generated colors to the template.
// Same as Generate() but takes AIColors for ergonomics.
func GenerateFromAI(c AIColors) string {
	return Generate(Colors{
		SkyBottomRGB: c.SkyBottomRGB,
		SkyTopRGB:    c.SkyTopRGB,
		CloudRGB:     c.CloudRGB,
		SunCoreRGB:   c.SunCoreRGB,
	})
}
