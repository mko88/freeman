package wailsapp

import (
	"freeman/internal/appdata"
	"freeman/internal/headercatalog"
)

// GetHeaderCatalog returns the common request-header names and their
// common values the frontend's header editor offers as autocomplete
// suggestions, resolved against headers.yaml in the app-data directory
// (see internal/headercatalog). Like GetTheme, there's no setter — the
// file is hand-edited. internal/httpapi exposes the same data at
// GET /api/headers for the container server and scripting.
func (a *App) GetHeaderCatalog() []headercatalog.Entry {
	return headercatalog.Resolve(headercatalog.Load(appdata.Dir()))
}
