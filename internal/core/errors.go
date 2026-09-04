package core

import "errors"

// ErrNoWorkspace is returned by any App method that needs an open
// workspace when none has been opened yet. Desktop's Wails frontend
// always calls OpenWorkspace/CurrentWorkspace first, so this was
// previously unreachable in practice — it matters once App is driven by
// an external caller (see internal/httpapi's desktop control API) that
// might hit e.g. ExecuteRequest before any workspace exists.
var ErrNoWorkspace = errors.New("no workspace is open")

func (a *App) requireWorkspace() error {
	if a.ws == nil {
		return ErrNoWorkspace
	}
	return nil
}
