// Package wailsapp is the Wails-specific layer bound to the desktop
// frontend: it adds the two things only a native desktop app can do
// (a startup-context-scoped OS folder picker, and remembering the last
// workspace across launches) on top of internal/core's transport-agnostic
// application logic, which internal/httpapi wraps the same way for the
// headless server.
package wailsapp

import (
	"context"
	"encoding/base64"
	"os"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"freeman/internal/appdata"
	"freeman/internal/core"
	"freeman/internal/domain"
	"freeman/internal/httpengine"
)

// App is bound to the frontend via wails.Run's options.App.Bind. Its
// exported methods are core.App's, promoted by embedding; only the
// methods below are wailsapp's own.
type App struct {
	*core.App
	ctx         context.Context
	controlAddr string

	// One lock for both: each is a string the frontend pushes and an
	// HTTP handler reads, written once per report and never together.
	uiStateMu      sync.RWMutex
	uiState        string
	controlAPIDocs string

	// The cancel func of the send in flight, or nil. Its own lock: it's
	// written by whichever goroutine is sending and read by whichever is
	// handling the cancel, which are never the same one.
	sendMu     sync.Mutex
	cancelSend context.CancelFunc
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
// It persists resp to the response cache (see saveResponseCache) and,
// for a body over httpengine.LargeResponseThreshold, blanks Body and
// points BodyFile at that cache file — so the multi-MB string never
// crosses the Wails bridge or gets re-mirrored by GET /api/ui/state.
// ExecuteRequest sends under a context this app can cancel, so a request
// aimed at something slow doesn't have to be waited out — see
// CancelRequest. One at a time is enough: the Send button is disabled
// while a request is in flight, so there is never a second one to name.
func (a *App) ExecuteRequest(collectionID, itemID, environmentID string) (*httpengine.Response, error) {
	ctx, cancel := context.WithCancel(a.ctx)
	a.sendMu.Lock()
	a.cancelSend = cancel
	a.sendMu.Unlock()
	defer func() {
		a.sendMu.Lock()
		a.cancelSend = nil
		a.sendMu.Unlock()
		cancel()
	}()

	resp, err := a.App.ExecuteRequest(ctx, collectionID, itemID, environmentID)
	if err != nil {
		return nil, err
	}
	a.saveResponseCache(itemID, resp) // best-effort; see its own doc comment
	a.trimLargeBody(itemID, resp)
	return resp, nil
}

// CancelRequest stops the send that's in flight, if there is one. The
// call it interrupts returns the context error, which the editor shows
// where a failure would go — a cancelled request is a request that
// didn't happen, not a request that failed differently.
//
// Safe to call when nothing is in flight: it does nothing.
func (a *App) CancelRequest() {
	a.sendMu.Lock()
	cancel := a.cancelSend
	a.sendMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// trimLargeBody blanks resp.Body and points resp.BodyFile at itemID's
// cache file when the body is over httpengine.LargeResponseThreshold —
// the shared shape ExecuteRequest and GetCachedResponse both hand the
// frontend for an oversized response.
func (a *App) trimLargeBody(itemID string, resp *httpengine.Response) {
	if int64(len(resp.Body)) <= httpengine.LargeResponseThreshold {
		return
	}
	if path, err := a.responseCacheBodyPath(itemID); err == nil {
		resp.Truncated = true
		resp.BodyFile = path
		resp.Body = ""
	}
}

// DeleteRequest shadows core.App's to also drop itemID's cached response
// (see saveResponseCache) — otherwise a stale response could resurface
// if the same generated ID were ever reused.
func (a *App) DeleteRequest(collectionID, itemID string) error {
	if err := a.App.DeleteRequest(collectionID, itemID); err != nil {
		return err
	}
	a.deleteResponseCache(itemID)
	return nil
}

// DeleteCollection shadows core.App's to also drop the cached responses
// of every request it held. The cache is keyed by item ID alone, with no
// record of which collection an item belonged to, so once the collection
// file is gone nothing else could ever identify those entries.
//
// The collection is read before it's deleted, since afterwards there's
// nothing left to enumerate.
func (a *App) DeleteCollection(id string) error {
	c, err := a.App.GetCollection(id)
	if err != nil {
		return err
	}
	if err := a.App.DeleteCollection(id); err != nil {
		return err
	}
	a.forEachRequest(c.Items, func(itemID string) { a.deleteResponseCache(itemID) })
	return nil
}

// forEachRequest walks a collection's tree, which nests: folders carry
// their own Items (see domain.Item).
func (a *App) forEachRequest(items []domain.Item, fn func(itemID string)) {
	for _, item := range items {
		if len(item.Items) > 0 {
			a.forEachRequest(item.Items, fn)
		}
		if item.Type == domain.ItemTypeRequest {
			fn(item.ID)
		}
	}
}

// GetCachedResponse returns the last response itemID's request got (see
// saveResponseCache), so reselecting a request shows what it last
// returned instead of a blank pane — even across a relaunch, unlike the
// in-memory `response` App.svelte otherwise resets to null on every
// selectRequest. An error (a request never sent, or the cache was
// cleared) means "nothing cached" — App.svelte's selectRequest treats
// that the same as before, falling back to a blank response pane, not a
// surfaced error.
func (a *App) GetCachedResponse(itemID string) (*httpengine.Response, error) {
	resp, err := a.loadResponseCache(itemID)
	if err != nil {
		return nil, err
	}
	a.trimLargeBody(itemID, resp)
	return resp, nil
}

// ClearResponseCache deletes every cached response for the open
// workspace (see saveResponseCache) — the settings window's "Clear
// response cache" button. No server equivalent — the headless server
// doesn't keep this cache at all (see responsecache.go's package-level
// doc comment).
func (a *App) ClearResponseCache() error {
	return a.clearResponseCache()
}

// ClearCachedResponse drops just itemID's cached response (see
// saveResponseCache) — the response pane's "..." menu's per-request
// counterpart to ClearResponseCache. No server equivalent.
func (a *App) ClearCachedResponse(itemID string) error {
	a.deleteResponseCache(itemID)
	return nil
}

// OpenResponseCacheExternally opens itemID's cached response body (see
// saveResponseCache) in whatever application the OS associates with its
// file extension — the response pane's "..." menu's equivalent of
// OpenResponseExternally, available for any response that's ever been
// sent rather than only one big enough to have been truncated. No server
// equivalent — a browser can't launch a native application.
func (a *App) OpenResponseCacheExternally(itemID string) error {
	path, err := a.responseCacheBodyPath(itemID)
	if err != nil {
		return err
	}
	return openExternally(path)
}

// GetResponseCachePath returns itemID's cached response body's path (see
// saveResponseCache) — the response pane's "..." menu's "Copy path"
// reads it via this, then copies it to the clipboard itself.
func (a *App) GetResponseCachePath(itemID string) (string, error) {
	return a.responseCacheBodyPath(itemID)
}

// GetResponseCacheDataURI returns itemID's cached response body as a
// data: URI (its recorded Content-Type, base64) — for the response pane
// to render an image response in an <img> rather than as its raw bytes.
// The webview can't load the cache file by path directly, so the bytes
// are inlined; only used for responses small enough not to have been
// truncated in the first place.
func (a *App) GetResponseCacheDataURI(itemID string) (string, error) {
	path, err := a.responseCacheBodyPath(itemID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ct := a.responseCacheContentType(itemID)
	if ct == "" {
		ct = "application/octet-stream"
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// OpenResponseCacheInFileExplorer shows itemID's cached response body
// (see saveResponseCache) in the OS's file manager — the response pane's
// "..." menu's equivalent of OpenResponseInFileExplorer, available for
// any response that's ever been sent. No server equivalent — a browser
// can't launch a native file manager.
func (a *App) OpenResponseCacheInFileExplorer(itemID string) error {
	path, err := a.responseCacheBodyPath(itemID)
	if err != nil {
		return err
	}
	return openInFileExplorer(path)
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

// ReportControlAPIDocs receives the control API's own route/action
// catalogue from the frontend on mount, the same way ReportUIState
// receives the editor's state. It's reported rather than written here
// because the frontend already owns that list — the help modal renders
// it and consistency.py diffs it against the dispatcher — and a second
// copy in Go would be free to drift from the app it describes.
//
// GET /api/agent (see cmd/freeman/main.go) formats it for an agent that
// has no way to read a Svelte component.
func (a *App) ReportControlAPIDocs(catalogJSON string) {
	a.uiStateMu.Lock()
	defer a.uiStateMu.Unlock()
	a.controlAPIDocs = catalogJSON
}

// ControlAPIDocs returns the catalogue last reported, or "" before the
// window has mounted — agentdocs.Render says so rather than implying the
// app has no actions.
func (a *App) ControlAPIDocs() string {
	a.uiStateMu.RLock()
	defer a.uiStateMu.RUnlock()
	return a.controlAPIDocs
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
