package wailsapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

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

// itemIDPattern matches what domain.NewID produces: a short prefix plus
// alphanumerics. Every function below turns an itemID into a filesystem
// path, and that ID arrives straight from the frontend or the control
// API — filepath.Join *cleans* a "../" rather than refusing it, so an
// unchecked ID could read, open in an external editor, or delete files
// outside the cache directory. Constraining the charset also keeps glob
// metacharacters (*, ?, [) out of the Glob patterns used below.
var itemIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// cachePath returns <responsesDir>/<itemID><suffix>, or "" if no
// workspace is open or itemID isn't a plain ID. Callers treat "" the
// same way they already treat a missing workspace: nothing to do.
func (a *App) cachePath(itemID, suffix string) string {
	if !itemIDPattern.MatchString(itemID) {
		return ""
	}
	dir := a.responsesDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, itemID+suffix)
}

// removeCachedBodies deletes itemID's body file whatever extension it
// was last written with (see saveResponseCache).
func (a *App) removeCachedBodies(itemID string) {
	pattern := a.cachePath(itemID, ".body.*")
	if pattern == "" {
		return
	}
	matches, _ := filepath.Glob(pattern)
	for _, f := range matches {
		os.Remove(f)
	}
}

// saveResponseCache persists resp for itemID as two files — <itemID>
// .meta.json (status/headers/duration/size) and <itemID>.body<ext> (the
// exact response body, ext guessed from its Content-Type via
// httpengine.ExtensionFor). Called with resp.Body still inline (the
// trimming in App.ExecuteRequest happens after this), best-effort: a
// failure (no workspace open, a full disk) is silently ignored rather
// than failing a request that already succeeded.
func (a *App) saveResponseCache(itemID string, resp *httpengine.Response) {
	metaPath := a.cachePath(itemID, ".meta.json")
	if metaPath == "" {
		return
	}

	body := []byte(resp.Body)

	// Remove any previous body file for this item first — its extension
	// (and so its filename) may not match this response's, e.g. a
	// request that returned JSON last time and plain text this time.
	a.removeCachedBodies(itemID)

	contentType := ""
	if v := resp.Headers["Content-Type"]; len(v) > 0 {
		contentType = v[0]
	}
	bodyPath := a.cachePath(itemID, ".body"+httpengine.ExtensionFor(contentType))
	if err := os.WriteFile(bodyPath, body, 0o644); err != nil {
		return
	}

	meta := *resp
	meta.Body = ""
	meta.Truncated = false
	meta.BodyFile = ""
	if data, err := json.Marshal(&meta); err == nil {
		os.WriteFile(metaPath, data, 0o644)
	}
}

// responseCacheContentType returns the Content-Type recorded in itemID's
// cached response meta (see saveResponseCache), "" if there's none.
func (a *App) responseCacheContentType(itemID string) string {
	metaPath := a.cachePath(itemID, ".meta.json")
	if metaPath == "" {
		return ""
	}
	data, err := os.ReadFile(metaPath)
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
	pattern := a.cachePath(itemID, ".body.*")
	if pattern == "" {
		return "", os.ErrNotExist
	}
	matches, err := filepath.Glob(pattern)
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
	metaPath := a.cachePath(itemID, ".meta.json")
	if metaPath == "" {
		return nil, os.ErrNotExist
	}

	metaData, err := os.ReadFile(metaPath)
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
	if metaPath := a.cachePath(itemID, ".meta.json"); metaPath != "" {
		os.Remove(metaPath)
	}
	a.removeCachedBodies(itemID)
}
