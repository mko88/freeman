package wailsapp

import "freeman/internal/version"

// GetVersion reports which build is running, for the line under the
// title in the help modal. GET /api/version is the same thing for the
// web build and for scripts.
//
// Returns a map rather than version.Info for the same reason GetTheme
// does: Wails' binding generator emits a TS model only for types it
// sees in a binding's signature, and a one-field-per-key map needs no
// model at all.
func (a *App) GetVersion() map[string]string {
	info := version.Get()
	return map[string]string{
		"version": info.Version,
		"commit":  info.Commit,
		"date":    info.Date,
	}
}
