package wailsapp

import (
	"encoding/json"
	"os"
	"path/filepath"

	"freeman/internal/httpengine"
)

// This file persists one response per request — the last one it got — to
// <workspaceRoot>/.cache/responses. It's the single place a response
// body lives on disk: durable (reselecting a request shows its last
// response again, even after a relaunch — see App.GetCachedResponse),
// the file the response pane's "..." menu acts on, and the source
// App.ExecuteRequest reads a large body back from after blanking it out
// of the response it hands the frontend (see httpengine.Response.Truncated).
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
// httpengine.ExtensionFor). Called with resp.Body still inline (the
// trimming in App.ExecuteRequest happens after this), best-effort: a
// failure (no workspace open, a full disk) is silently ignored rather
// than failing a request that already succeeded.
func (a *App) saveResponseCache(itemID string, resp *httpengine.Response) {
	dir := a.responsesDir()
	if dir == "" {
		return
	}

	body := []byte(resp.Body)

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
// cleared) otherwise. Body comes back inline; App.GetCachedResponse
// applies the same large-body trimming ExecuteRequest does before
// handing it to the frontend.
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
