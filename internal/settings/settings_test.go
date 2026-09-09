package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// A missing or unreadable settings.yaml must never stop a workspace from
// opening — it falls back to what Freeman shipped with.
func TestLoadFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	if got := Load(dir); got != Defaults() {
		t.Fatalf("no file should give the defaults, got %+v", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte("{{{ not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir); got != Defaults() {
		t.Fatalf("a broken file should give the defaults, got %+v", got)
	}
}

// A file listing one key keeps the built-in answer for the rest, so
// adding a setting doesn't invalidate everyone's file.
func TestLoadKeepsDefaultsForAbsentKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte("requestTimeoutMs: 5000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load(dir)
	if got.RequestTimeoutMs != 5000 {
		t.Errorf("timeout not read: %+v", got)
	}
	if got.MaxRedirects != Defaults().MaxRedirects {
		t.Errorf("an absent key should keep its default: %+v", got)
	}
}

func TestClamp(t *testing.T) {
	d := Defaults()
	cases := []struct {
		name string
		in   Settings
		want func(Settings) bool
	}{
		{
			// Both mean "no limit", which is a real choice — not an error.
			"negative timeout and cap become zero",
			Settings{RequestTimeoutMs: -1, MaxRedirects: -5},
			func(s Settings) bool { return s.RequestTimeoutMs == 0 && s.MaxRedirects == 0 },
		},
		{
			// A zero response cap would show nothing at all.
			"zero response sizes fall back",
			Settings{},
			func(s Settings) bool {
				return s.InlineResponseBytes == d.InlineResponseBytes && s.MaxResponseBytes == d.MaxResponseBytes
			},
		},
		{
			// Reading less than is shown inline would truncate a body the
			// pane was about to render whole.
			"a max below the inline threshold is raised to it",
			Settings{InlineResponseBytes: 4 << 20, MaxResponseBytes: 1 << 20},
			func(s Settings) bool { return s.MaxResponseBytes == 4<<20 },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Clamp(tc.in); !tc.want(got) {
				t.Fatalf("Clamp(%+v) = %+v", tc.in, got)
			}
		})
	}
}

// Written as a value Clamp would leave alone, so this tests the round
// trip and not the clamping — every field that has a floor is set above
// it. MaxRedirects stays 0 because 0 there means "no cap", not "unset".
func TestSaveRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := Settings{
		RequestTimeoutMs:    1500,
		MaxRedirects:        0,
		InlineResponseBytes: 2 << 20,
		MaxResponseBytes:    8 << 20,
		FontUI:              "Inter",
		FontMono:            "Cascadia Code",
		FontScalePercent:    115,
	}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir); got != want {
		t.Fatalf("round trip lost something: got %+v, want %+v", got, want)
	}
}

// The scale has a floor and a ceiling like every other setting, and a 0
// — a settings.yaml written before the field existed — means the default
// rather than an interface scaled to nothing.
func TestClampFontScale(t *testing.T) {
	for in, want := range map[int]int{0: 100, 10: 70, 69: 70, 70: 70, 130: 130, 201: 200, -5: 70} {
		if got := Clamp(Settings{FontScalePercent: in}).FontScalePercent; got != want {
			t.Errorf("Clamp(FontScalePercent: %d) = %d, want %d", in, got, want)
		}
	}
}
