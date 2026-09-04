package wailsapp

import (
	"freeman/internal/appdata"
	"freeman/internal/theme"
)

// GetTheme returns the active color palette (see internal/theme), resolved
// against theme.yaml in the app-data directory. There's no SetTheme — the
// file is meant to be hand-edited, not switched from within the app.
//
// Returns a plain map[string]string rather than theme.Colors: Wails'
// binding generator only emits a TS type for named struct types, so a
// named map type here would produce a .d.ts referencing a model that was
// never actually generated.
func (a *App) GetTheme() map[string]string {
	return theme.Resolve(theme.Load(appdata.Dir()))
}
