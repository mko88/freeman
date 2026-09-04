// Package wailsapp is the Wails-specific layer bound to the desktop
// frontend: it adds the two things only a native desktop app can do
// (a startup-context-scoped OS folder picker, and remembering the last
// workspace across launches) on top of internal/core's transport-agnostic
// application logic, which internal/httpapi wraps the same way for the
// headless server.
package wailsapp

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"freeman/internal/appdata"
	"freeman/internal/core"
	"freeman/internal/httpengine"
)

// App is bound to the frontend via wails.Run's options.App.Bind. Its
// exported methods are core.App's, promoted by embedding; only the
// methods below are wailsapp's own.
type App struct {
	*core.App
	ctx         context.Context
	controlAddr string

	uiStateMu sync.RWMutex
	uiState   string
}

func NewApp() *App {
	return &App{App: core.NewApp()}
}

// Startup is wired as wails' OnStartup; it reopens the last workspace, if
// any, so the app doesn't start empty every launch.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if prefs := appdata.LoadPrefs(); prefs.LastWorkspace != "" {
		_, _ = a.OpenWorkspace(prefs.LastWorkspace)
	}
}

// SelectWorkspaceFolder opens a native folder picker and returns the
// chosen path, or "" if the user cancelled. No server equivalent — a
// browser can't open the server's filesystem dialog.
func (a *App) SelectWorkspaceFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a Freeman workspace folder",
	})
}

// SelectFile opens a native file picker and returns the chosen path, or
// "" if the user cancelled. No server equivalent — a browser can't open
// the server's filesystem dialog. Used for form-data file fields and the
// binary body mode; the control API sets a path directly instead (see
// App.svelte's addRequestFormField/setRequestFormField/setRequestField
// dispatch cases), the same "no dialog available" split OpenWorkspace has
// against SelectWorkspaceFolder.
func (a *App) SelectFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a file",
	})
}

// OpenWorkspace shadows core.App's: same behavior, plus remembering root
// as the workspace to reopen on next launch — meaningless for a server,
// which is always told its (fixed) workspace via FREEMAN_WORKSPACE.
func (a *App) OpenWorkspace(root string) (*core.WorkspaceInfo, error) {
	info, err := a.App.OpenWorkspace(root)
	if err != nil {
		return nil, err
	}
	_ = appdata.SavePrefs(appdata.Prefs{LastWorkspace: root})
	return info, nil
}

// ExecuteRequest shadows core.App's to supply the Wails startup context
// core.App.ExecuteRequest now takes explicitly, keeping this method's own
// signature (no ctx param) unchanged for the existing generated bindings.
func (a *App) ExecuteRequest(collectionID, itemID, environmentID string) (*httpengine.Response, error) {
	return a.App.ExecuteRequest(a.ctx, collectionID, itemID, environmentID)
}

// SetControlAPIAddr records where cmd/freeman's control API (see
// startControlAPI) is actually listening, so ControlAPIAddr can report it
// to the frontend. Called once from main, before wails.Run.
func (a *App) SetControlAPIAddr(addr string) {
	a.controlAddr = addr
}

// ControlAPIAddr returns the address set by SetControlAPIAddr, for the
// frontend to display (see App.svelte's status bar) — "" if it was never
// set, which shouldn't happen since main sets it before the frontend can
// possibly mount.
func (a *App) ControlAPIAddr() string {
	return a.controlAddr
}

// DispatchUIAction emits a "ui:action" Wails event carrying action and an
// optional payload, for App.svelte's central dispatcher to act on. This
// is the desktop control API's answer to "drive the UI itself" (open the
// env editor, select an environment, send the current request, ...) —
// distinct from internal/core's data operations (save a request,
// execute one), which don't touch what's currently on screen at all.
// Curated by design: each action needs a case in App.svelte's dispatcher,
// there's no generic "click this selector" escape hatch.
func (a *App) DispatchUIAction(action string, payload map[string]any) {
	runtime.EventsEmit(a.ctx, "ui:action", action, payload)
}

// ReportUIState is DispatchUIAction's read-side counterpart: App.svelte
// calls it after every ui:action, with a JSON-encoded snapshot of the
// editor's draft state (the fields setRequestField/setEnvironmentVariable/
// etc. can set, plus things nothing sets directly, like the last
// response), so UIState always has a fresh mirror of what's currently on
// screen — a script/agent can then read that back over the control API
// (GET /api/ui/state) instead of screenshotting the window to check.
// Takes the already-encoded JSON string rather than map[string]any: a
// Wails-bound method taking a plain object argument from the frontend
// didn't reliably reach here in testing (state kept reading back as
// whatever was first reported, never the latest) — encoding on the
// Svelte side and passing a string sidesteps whatever that was, and
// UIState can then just write it straight back out as-is (see
// cmd/freeman/main.go), no re-marshaling. Keys are curated to match the
// field names set*Field-style actions use — see App.svelte's
// reportUIState function for the exact shape, and keep this doc's "so a
// getter exists for every setter" intent in mind when adding a new
// settable field (per CLAUDE.md's standing rule).
func (a *App) ReportUIState(stateJSON string) {
	a.uiStateMu.Lock()
	defer a.uiStateMu.Unlock()
	a.uiState = stateJSON
}

// UIState returns the most recently reported state as raw JSON (see
// ReportUIState) — "{}" if the frontend hasn't reported anything yet
// (e.g. no workspace open). cmd/freeman's GET /api/ui/state writes this
// straight out as the response body.
func (a *App) UIState() string {
	a.uiStateMu.RLock()
	defer a.uiStateMu.RUnlock()
	if a.uiState == "" {
		return "{}"
	}
	return a.uiState
}
