// Package weapon generates weapon.dat mod files for Black Russia by modifying
// weapon.json / weapon_overrides.json / weapon_presets.json with custom
// ammo (PT) and accuracy (RAZB) values for DESERT_EAGLE.
//
// Original Python (main.py:1590-1620):
//   - For weapon.json: find weapon with uniqueName == "DESERT_EAGLE", set
//     ammo=PT, accuracy=RAZB.
//   - For weapon_overrides.json: set weapons["DESERT_EAGLE"]["ammo"]=PT,
//     ["accuracy"]=RAZB.
//   - For weapon_presets.json: set antiSpreadStaticAim["DESERT_EAGLE"]["accuracy"]=RAZB,
//     antiReload["DESERT_EAGLE"]=PT.
//
// Bug in Python: the JSON output uses ensure_ascii=False for weapon.json
// and weapon_presets.json, but the default (ensure_ascii=True) for
// weapon_overrides.json. We use compact JSON for all three (no ASCII
// escaping needed — the files are pure ASCII).
package weapon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ApplyParams walks the given folder, finds the three JSON files, and
// applies PT (ammo) and RAZB (accuracy) values to DESERT_EAGLE entries.
//
// Returns nil on success. Missing files are silently skipped (matches
// Python behaviour — `if os.path.exists(...)`).
func ApplyParams(folder string, pt, razb int) error {
	// 1. weapon.json
	wjPath := filepath.Join(folder, "weapon.json")
	if data, err := os.ReadFile(wjPath); err == nil {
		var doc struct {
			Weapons []struct {
				UniqueName string `json:"uniqueName"`
				Ammo       int    `json:"ammo"`
				Accuracy   int    `json:"accuracy"`
			} `json:"weapons"`
		}
		if err := json.Unmarshal(data, &doc); err == nil {
			for i := range doc.Weapons {
				if doc.Weapons[i].UniqueName == "DESERT_EAGLE" {
					doc.Weapons[i].Ammo = pt
					doc.Weapons[i].Accuracy = razb
					break
				}
			}
			out, _ := json.MarshalIndent(doc, "", "  ")
			_ = os.WriteFile(wjPath, out, 0o644)
		}
	}

	// 2. weapon_overrides.json
	woPath := filepath.Join(folder, "weapon_overrides.json")
	if data, err := os.ReadFile(woPath); err == nil {
		var doc struct {
			Weapons map[string]struct {
				Ammo     int `json:"ammo"`
				Accuracy int `json:"accuracy"`
			} `json:"weapons"`
		}
		if err := json.Unmarshal(data, &doc); err == nil {
			if w, ok := doc.Weapons["DESERT_EAGLE"]; ok {
				w.Ammo = pt
				w.Accuracy = razb
				doc.Weapons["DESERT_EAGLE"] = w
				out, _ := json.Marshal(doc)
				_ = os.WriteFile(woPath, out, 0o644)
			}
		}
	}

	// 3. weapon_presets.json
	wpPath := filepath.Join(folder, "weapon_presets.json")
	if data, err := os.ReadFile(wpPath); err == nil {
		// We use map[string]json.RawMessage to avoid locking in a specific
		// structure — the file has multiple top-level keys and we only
		// modify two of them.
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(data, &doc); err == nil {
			// antiSpreadStaticAim["DESERT_EAGLE"]["accuracy"] = razb
			if raw, ok := doc["antiSpreadStaticAim"]; ok {
				var asa map[string]map[string]int
				if json.Unmarshal(raw, &asa) == nil {
					if entry, ok := asa["DESERT_EAGLE"]; ok {
						entry["accuracy"] = razb
						asa["DESERT_EAGLE"] = entry
						if b, err := json.Marshal(asa); err == nil {
							doc["antiSpreadStaticAim"] = b
						}
					}
				}
			}
			// antiReload["DESERT_EAGLE"] = pt
			if raw, ok := doc["antiReload"]; ok {
				var ar map[string]int
				if json.Unmarshal(raw, &ar) == nil {
					ar["DESERT_EAGLE"] = pt
					if b, err := json.Marshal(ar); err == nil {
						doc["antiReload"] = b
					}
				}
			}
			out, _ := json.Marshal(doc)
			_ = os.WriteFile(wpPath, out, 0o644)
		}
	}

	return nil
}

// Preset describes one weapon preset folder.
type Preset struct {
	Name   string // "Стандарт", "⚡ Ускор + Антик", etc.
	Folder string // "weapons/presest1", etc.
	Desc   string
}

// Presets maps preset ID → Preset.
var Presets = map[string]Preset{
	"1": {Name: "Стандарт", Folder: "weapons/presest1", Desc: "Стандартный веапон"},
	"2": {Name: "⚡ Ускор + Антик", Folder: "weapons/presest2", Desc: "Ускоренная стрельба, антикилл, раскрывающийся прицел"},
	"3": {Name: "🔄 Без перезарядки + Динамичный", Folder: "weapons/presest3", Desc: "Без перезарядки, динамичный прицел"},
	"4": {Name: "🎯 Без перезарядки + Статичный", Folder: "weapons/presest4", Desc: "Без перезарядки, статичный прицел"},
}

// GetPreset returns the preset for the given ID, or an error.
func GetPreset(id string) (Preset, error) {
	p, ok := Presets[id]
	if !ok {
		return Preset{}, fmt.Errorf("unknown preset %q (available: 1, 2, 3, 4)", id)
	}
	return p, nil
}
