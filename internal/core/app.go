// Package core holds the transport-agnostic application logic shared by
// both the Wails desktop app (internal/wailsapp) and the headless HTTP
// server (internal/httpapi): workspace/collection/environment/request
// operations. It depends on domain, store, and httpengine only — never on
// Wails or net/http — so either transport can wrap it without duplicating
// this logic.
package core

import (
	"regexp"
	"strings"
	"sync"

	"freeman/internal/httpengine"
	"freeman/internal/store"
)

// App holds the currently open workspace. Callers (wailsapp.App,
// httpapi's handler) own how/when OpenWorkspace gets called — desktop
// reopens the last-used one from appdata prefs, the server opens a fixed
// path from an env var — App itself has no opinion on that.
//
// mu serializes every exported method: the desktop control API (see
// internal/wailsapp) drives App from HTTP handler goroutines at the same
// time the Wails frontend does, so a GET /api/environments ranging
// ws.EnvironmentPaths could otherwise race a saveEnvironment mutating it
// — a Go map read/write data race, and, once SaveEnvironment started
// renaming files on a name change, a read of a path a concurrent rename
// had just moved. Exported methods take mu; the unexported *Locked-style
// helpers they share with each other (workspaceInfo calling the list
// helpers, OpenWorkspace calling createEnvironment) assume it's held.
type App struct {
	mu sync.Mutex
	ws *store.Workspace
}

func NewApp() *App {
	return &App{}
}

// WorkspaceInfo is everything a caller needs after opening a workspace:
// enough to populate a sidebar without a second round trip.
type WorkspaceInfo struct {
	Root         string               `json:"root"`
	Collections  []CollectionSummary  `json:"collections"`
	Environments []EnvironmentSummary `json:"environments"`
}

// OpenWorkspace scans root for collections/environments, creating the
// layout and a default collection/environment if it's empty so a brand
// new workspace isn't a blank screen.
func (a *App) OpenWorkspace(root string) (*WorkspaceInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := store.EnsureLayout(root); err != nil {
		return nil, err
	}
	ws, err := store.DiscoverWorkspace(root)
	if err != nil {
		return nil, err
	}
	a.ws = ws

	// A new workspace is a different set of APIs, so whatever session the
	// last one had established doesn't belong to it.
	httpengine.ResetCookies()

	if len(ws.CollectionPaths) == 0 {
		if _, err := a.createCollection("My Requests"); err != nil {
			return nil, err
		}
	}
	if len(ws.EnvironmentPaths) == 0 {
		if _, err := a.createEnvironment("Development"); err != nil {
			return nil, err
		}
	}

	return a.workspaceInfo()
}

// CurrentWorkspace returns the already-open workspace, or nil if none is
// open yet.
func (a *App) CurrentWorkspace() (*WorkspaceInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.ws == nil {
		return nil, nil
	}
	return a.workspaceInfo()
}

// WorkspaceRoot returns the open workspace's root directory, or "" if
// none is open. Used by httpapi to resolve theme.yaml from within the
// workspace (the one thing guaranteed to persist across container
// restarts via a volume mount) rather than a per-user app-data directory,
// which has no durable meaning in a container.
func (a *App) WorkspaceRoot() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.ws == nil {
		return ""
	}
	return a.ws.Root
}

// workspaceInfo assumes a.mu is held (see the *Locked-helper note on App).
func (a *App) workspaceInfo() (*WorkspaceInfo, error) {
	collections, err := a.listCollections()
	if err != nil {
		return nil, err
	}
	environments, err := a.listEnvironments()
	if err != nil {
		return nil, err
	}
	return &WorkspaceInfo{Root: a.ws.Root, Collections: collections, Environments: environments}, nil
}

var slugPattern = regexp.MustCompile(`[^a-z0-9-]+`)

// slugify turns a display name into a filesystem-safe slug, falling back
// to id (always unique) if the name has no usable characters.
func slugify(name, id string) string {
	s := slugPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return id
	}
	return s
}
