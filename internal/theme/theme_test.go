package theme

import "testing"

func TestLoadDefaultsToDark(t *testing.T) {
	cfg := Load(t.TempDir())
	if cfg.Active != "dark" {
		t.Fatalf("expected default active %q, got %q", "dark", cfg.Active)
	}
	if got := Resolve(cfg); got["bg"] != Dark["bg"] {
		t.Fatalf("expected dark palette, got %+v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Active: "midnight",
		Themes: map[string]Colors{
			"midnight": {"bg": "#000000", "accent": "#ff00ff"},
		},
	}
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := Load(dir)
	if got.Active != "midnight" {
		t.Fatalf("expected active %q, got %q", "midnight", got.Active)
	}

	resolved := Resolve(got)
	if resolved["bg"] != "#000000" {
		t.Fatalf("expected overridden bg, got %q", resolved["bg"])
	}
	if resolved["accent"] != "#ff00ff" {
		t.Fatalf("expected overridden accent, got %q", resolved["accent"])
	}
	// Tokens not overridden by the custom theme still fall back to Dark.
	if resolved["text"] != Dark["text"] {
		t.Fatalf("expected fallback text %q, got %q", Dark["text"], resolved["text"])
	}
}

func TestResolveUnknownActiveFallsBackToDark(t *testing.T) {
	cfg := Config{Active: "does-not-exist"}
	if got := Resolve(cfg); got["bg"] != Dark["bg"] {
		t.Fatalf("expected dark fallback, got %+v", got)
	}
}
