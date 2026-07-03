// Package ai — shared helpers.
package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ExtractJSON finds the first {...} block in raw AI output.
func ExtractJSON(raw string) (string, error) {
	re := regexp.MustCompile(`(?s)\{.*\}`)
	match := re.FindString(raw)
	if match == "" {
		return "", fmt.Errorf("AI did not return JSON")
	}
	return match, nil
}

// ExtractHex finds the first #RRGGBB hex color.
func ExtractHex(raw string) (string, error) {
	re := regexp.MustCompile(`#[A-Fa-f0-9]{6}`)
	match := re.FindString(raw)
	if match == "" {
		return "", fmt.Errorf("AI did not return hex: %q", raw)
	}
	return strings.ToUpper(match), nil
}

// ParseJSON is a convenience wrapper.
func ParseJSON(data string, v any) error {
	return json.Unmarshal([]byte(data), v)
}
