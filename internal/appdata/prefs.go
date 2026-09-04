package appdata

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Prefs is small cross-workspace state, saved so the app can reopen the
// last workspace on launch instead of asking every time.
type Prefs struct {
	LastWorkspace string `json:"lastWorkspace"`
}

func prefsPath() string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "prefs.json")
}

// LoadPrefs returns zero-value Prefs if none have been saved yet.
func LoadPrefs() Prefs {
	path := prefsPath()
	if path == "" {
		return Prefs{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Prefs{}
	}
	var p Prefs
	if err := json.Unmarshal(data, &p); err != nil {
		return Prefs{}
	}
	return p
}

func SavePrefs(p Prefs) error {
	path := prefsPath()
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
