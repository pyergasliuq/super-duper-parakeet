// Package weapon — weapon_test.go
package weapon_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pweper/bot/internal/weapon"
)

func TestApplyParamsWeaponJSON(t *testing.T) {
	dir := t.TempDir()
	// Create a weapon.json with DESERT_EAGLE.
	wj := map[string]any{
		"weapons": []map[string]any{
			{"uniqueName": "AK47", "ammo": 30, "accuracy": 50},
			{"uniqueName": "DESERT_EAGLE", "ammo": 7, "accuracy": 80},
		},
	}
	b, _ := json.MarshalIndent(wj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "weapon.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := weapon.ApplyParams(dir, 100, 25); err != nil {
		t.Fatalf("ApplyParams: %v", err)
	}

	// Verify the change.
	data, _ := os.ReadFile(filepath.Join(dir, "weapon.json"))
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	weapons := got["weapons"].([]any)
	for _, w := range weapons {
		m := w.(map[string]any)
		if m["uniqueName"] == "DESERT_EAGLE" {
			if m["ammo"] != float64(100) {
				t.Errorf("DESERT_EAGLE ammo = %v, want 100", m["ammo"])
			}
			if m["accuracy"] != float64(25) {
				t.Errorf("DESERT_EAGLE accuracy = %v, want 25", m["accuracy"])
			}
			return
		}
	}
	t.Error("DESERT_EAGLE not found in output")
}

func TestApplyParamsOverridesJSON(t *testing.T) {
	dir := t.TempDir()
	wo := map[string]any{
		"weapons": map[string]map[string]int{
			"DESERT_EAGLE": {"ammo": 7, "accuracy": 80},
			"AK47":         {"ammo": 30, "accuracy": 50},
		},
	}
	b, _ := json.Marshal(wo)
	if err := os.WriteFile(filepath.Join(dir, "weapon_overrides.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := weapon.ApplyParams(dir, 50, 10); err != nil {
		t.Fatalf("ApplyParams: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "weapon_overrides.json"))
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	weapons := got["weapons"].(map[string]any)
	de := weapons["DESERT_EAGLE"].(map[string]any)
	if de["ammo"] != float64(50) {
		t.Errorf("ammo = %v, want 50", de["ammo"])
	}
	if de["accuracy"] != float64(10) {
		t.Errorf("accuracy = %v, want 10", de["accuracy"])
	}
}

func TestApplyParamsPresetsJSON(t *testing.T) {
	dir := t.TempDir()
	wp := map[string]any{
		"antiSpreadStaticAim": map[string]map[string]int{
			"DESERT_EAGLE": {"accuracy": 80},
		},
		"antiReload": map[string]int{
			"DESERT_EAGLE": 7,
		},
	}
	b, _ := json.Marshal(wp)
	if err := os.WriteFile(filepath.Join(dir, "weapon_presets.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := weapon.ApplyParams(dir, 99, 15); err != nil {
		t.Fatalf("ApplyParams: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "weapon_presets.json"))
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	asa := got["antiSpreadStaticAim"].(map[string]any)
	de := asa["DESERT_EAGLE"].(map[string]any)
	if de["accuracy"] != float64(15) {
		t.Errorf("antiSpreadStaticAim accuracy = %v, want 15", de["accuracy"])
	}
	ar := got["antiReload"].(map[string]any)
	if ar["DESERT_EAGLE"] != float64(99) {
		t.Errorf("antiReload = %v, want 99", ar["DESERT_EAGLE"])
	}
}

func TestApplyParamsMissingFiles(t *testing.T) {
	// Should not error if files don't exist.
	dir := t.TempDir()
	if err := weapon.ApplyParams(dir, 100, 25); err != nil {
		t.Errorf("ApplyParams should succeed with missing files: %v", err)
	}
}

func TestGetPreset(t *testing.T) {
	p, err := weapon.GetPreset("1")
	if err != nil {
		t.Fatalf("GetPreset: %v", err)
	}
	if p.Name != "Стандарт" {
		t.Errorf("preset 1 name = %q", p.Name)
	}

	_, err = weapon.GetPreset("99")
	if err == nil {
		t.Error("expected error for unknown preset")
	}
}
