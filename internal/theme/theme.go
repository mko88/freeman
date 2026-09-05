// Package theme resolves Freeman's UI color palette. A built-in Dark
// palette ships as the default; a user can override individual tokens or
// define additional named palettes by hand-editing theme.yaml in the
// app-data directory (see internal/appdata) — there's no in-app editor,
// this is a config file by design.
package theme

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Colors is a palette: keys are token names (e.g. "bg", "text", "accent"),
// matching the CSS custom properties (--fm-<key>) the frontend defines in
// theme.css — keep the two in sync by hand.
type Colors map[string]string

// Dark is Freeman's built-in default palette. The frontend's theme.css
// hardcodes these same values under :root so the first paint is already
// correct with no round trip to Go — theme.ts only needs to apply
// overrides when Resolve returns something different from Dark.
var Dark = Colors{
	"bg":           "#14181a",
	"bgPanel":      "#0f1214",
	"bgElevated":   "rgba(255, 255, 255, 0.05)",
	"bgHover":      "rgba(255, 255, 255, 0.075)",
	"bgResponse":   "rgba(0, 0, 0, 0.35)",
	"border":       "rgba(255, 255, 255, 0.14)",
	"borderSubtle": "rgba(255, 255, 255, 0.08)",
	"text":         "#edebe4",
	"textMuted":    "rgba(237, 235, 228, 0.58)",
	"accent":       "#e3a23c",
	"error":        "#e2604f",
	"success":      "#4fae8a",
	"warning":      "#d9863a",
	// Per-HTTP-method colors: a real structural device, not decoration —
	// the same method reads the same color everywhere it appears (sidebar
	// row accent, method select, method tag). GET/PUT/HEAD/OPTIONS get
	// their own hues; POST/PATCH/DELETE intentionally reuse accent/
	// success/error so the method system and the status/action system
	// share one palette instead of two unrelated ones.
	"methodGet":     "#5ca8e0",
	"methodPut":     "#b98ce0",
	"methodNeutral": "rgba(237, 235, 228, 0.58)",
}

const filename = "theme.yaml"

// Config is theme.yaml's shape: which palette is active, plus any
// user-defined ones. Active is "dark" or the name of an entry in Themes.
type Config struct {
	Active string            `yaml:"active"`
	Themes map[string]Colors `yaml:"themes,omitempty"`
}

// Load returns the persisted config, defaulting to {Active: "dark"} if
// theme.yaml doesn't exist or fails to parse.
func Load(dir string) Config {
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		return Config{Active: "dark"}
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{Active: "dark"}
	}
	if cfg.Active == "" {
		cfg.Active = "dark"
	}
	return cfg
}

// Save persists cfg to dir as YAML.
func Save(dir string, cfg Config) error {
	if cfg.Active == "" {
		cfg.Active = "dark"
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filename), data, 0o644)
}

// Resolve returns the active palette. Values from a custom theme override
// Dark's per-key — so a user only needs to list the tokens they actually
// want to change in theme.yaml, not the whole palette.
func Resolve(cfg Config) Colors {
	if cfg.Active == "" || cfg.Active == "dark" {
		return Dark
	}
	custom, ok := cfg.Themes[cfg.Active]
	if !ok {
		return Dark
	}
	resolved := make(Colors, len(Dark))
	for k, v := range Dark {
		resolved[k] = v
	}
	for k, v := range custom {
		resolved[k] = v
	}
	return resolved
}
