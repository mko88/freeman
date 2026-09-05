package wailsapp

import (
	"encoding/json"
	"os"
	"path/filepath"

	"freeman/internal/httpengine"
)

// This file persists one response per request — the last one it got — to
// <workspaceRoot>/.cache/responses, independent of the ephemeral OS-temp
// file httpengine.Execute itself writes for an oversized response (see
// httpengine.Response.Truncated's doc comment): that one exists purely
// to keep a huge body off the Wails IPC bridge for the *current*
// response, and is cleaned up as soon as the next one supersedes it.
// This cache is durable — it's what makes reselecting a request (see
// App.GetCachedResponse) show its last response again instead of a
// blank pane, even after quitting and relaunching the app — and it
// exists for every response, not just an oversized one, so the response
// pane's "..." menu (OpenResponseCacheExternally/GetResponseCachePath/
// OpenResponseCacheInFileExplorer) always has a real file to act on.
//
// It lives under the workspace (not a per-user app-data directory) so
// each workspace has its own — "Clear response cache" then only clears
// the open one, and two workspaces can't shadow each other's entries.
// Only the desktop app wires this in; the headless server doesn't cache
// responses.

// responsesDir returns <workspaceRoot>/.cache/responses, creating it if
// needed. "" if no workspace is open — every function below treats that
// as "skip this" rather than failing. Drops a .gitignore in .cache the
// first time, so a version-controlled workspace doesn't see this
// machine-local cache as untracked files (the workspace format is meant
// to diff cleanly).
func (a *App) responsesDir() string {
	root := a.WorkspaceRoot()
	if root == "" {
		return ""
	}
	cache := filepath.Join(root, ".cache")
	dir := filepath.Join(cache, "responses")
	os.MkdirAll(dir, 0o755)
	if gitignore := filepath.Join(cache, ".gitignore"); !fileExists(gitignore) {
		os.WriteFile(gitignore, []byte("*\n"), 0o644)
	}
	return dir
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// saveResponseCache persists resp for itemID as two files — <itemID>
// .meta.json (status/headers/duration/size) and <itemID>.body<ext> (the
// exact response body, ext guessed from its Content-Type via
// httpengine.ExtensionFor). Best-effort: a failure (no workspace open, a
// full disk) is silently ignored rather than failing a request that
// already succeeded — the same reasoning httpengine.Execute itself uses
// for its own temp-file write.
func (a *App) saveResponseCache(itemID string, resp *httpengine.Response) {
	dir := a.responsesDir()
	if dir == "" {
		return
	}

	body := []byte(resp.Body)
	if resp.Truncated {
		data, err := httpengine.ReadResponseBodyFile(resp.BodyFile)
		if err != nil {
			return
		}
		body = data
	}

	// Remove any previous body file for this item first — its extension
	// (and so its filename) may not match this response's, e.g. a
	// request that returned JSON last time and plain text this time.
	if old, _ := filepath.Glob(filepath.Join(dir, itemID+".body.*")); old != nil {
		for _, f := range old {
			os.Remove(f)
		}
	}

	contentType := ""
	if v := resp.Headers["Content-Type"]; len(v) > 0 {
		contentType = v[0]
	}
	bodyPath := filepath.Join(dir, itemID+".body"+httpengine.ExtensionFor(contentType))
	if err := os.WriteFile(bodyPath, body, 0o644); err != nil {
		return
	}

	meta := *resp
	meta.Body = ""
	meta.Truncated = false
	meta.BodyFile = ""
	if data, err := json.Marshal(&meta); err == nil {
		os.WriteFile(filepath.Join(dir, itemID+".meta.json"), data, 0o644)
	}
}

// responseCacheContentType returns the Content-Type recorded in itemID's
// cached response meta (see saveResponseCache), "" if there's none.
func (a *App) responseCacheContentType(itemID string) string {
	dir := a.responsesDir()
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, itemID+".meta.json"))
	if err != nil {
		return ""
	}
	var r httpengine.Response
	if json.Unmarshal(data, &r) != nil {
		return ""
	}
	if v := r.Headers["Content-Type"]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// responseCacheBodyPath returns itemID's cached body file's path (see
// saveResponseCache) — an error if nothing is cached for it yet.
func (a *App) responseCacheBodyPath(itemID string) (string, error) {
	dir := a.responsesDir()
	if dir == "" {
		return "", os.ErrNotExist
	}
	matches, err := filepath.Glob(filepath.Join(dir, itemID+".body.*"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", os.ErrNotExist
	}
	return matches[0], nil
}

// loadResponseCache returns the last cached response for itemID (see
// saveResponseCache) — an error (wrapping os.ErrNotExist for the normal
// "nothing cached yet" case: a request never sent, or the cache was
// cleared) otherwise. A body over httpengine.LargeResponseThreshold
// comes back Truncated exactly the way a live Execute response would,
// with BodyFile pointing at a fresh temp file (see
// httpengine.WriteResponseBodyFile) — so OpenResponseExternally/
// OpenResponseInFileExplorer and the response pane's own truncated-body
// callout work identically whether the response just arrived or was
// reloaded from disk.
func (a *App) loadResponseCache(itemID string) (*httpengine.Response, error) {
	dir := a.responsesDir()
	if dir == "" {
		return nil, os.ErrNotExist
	}

	metaData, err := os.ReadFile(filepath.Join(dir, itemID+".meta.json"))
	if err != nil {
		return nil, err
	}
	var resp httpengine.Response
	if err := json.Unmarshal(metaData, &resp); err != nil {
		return nil, err
	}

	bodyPath, err := a.responseCacheBodyPath(itemID)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, err
	}

	if len(body) > httpengine.LargeResponseThreshold {
		contentType := ""
		if v := resp.Headers["Content-Type"]; len(v) > 0 {
			contentType = v[0]
		}
		if tempPath, werr := httpengine.WriteResponseBodyFile(body, contentType); werr == nil {
			resp.Truncated = true
			resp.BodyFile = tempPath
			resp.Body = ""
			return &resp, nil
		}
	}
	resp.Body = string(body)
	resp.Truncated = false
	resp.BodyFile = ""
	return &resp, nil
}

// clearResponseCache deletes every cached response for the open
// workspace — the settings window's "Clear response cache" button.
func (a *App) clearResponseCache() error {
	dir := a.responsesDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		os.Remove(filepath.Join(dir, e.Name()))
	}
	return nil
}

// deleteResponseCache removes one request's cached response, if any —
// called when the request itself is deleted (see App.DeleteRequest), so
// the cache doesn't keep an orphaned entry around for a request that no
// longer exists. Best-effort: nothing to delete is not an error worth
// surfacing.
func (a *App) deleteResponseCache(itemID string) {
	dir := a.responsesDir()
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, itemID+".meta.json"))
	if matches, _ := filepath.Glob(filepath.Join(dir, itemID+".body.*")); matches != nil {
		for _, f := range matches {
			os.Remove(f)
		}
	}
}
