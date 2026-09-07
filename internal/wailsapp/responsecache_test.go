package wailsapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"freeman/internal/httpengine"
)

// newTestApp returns an App with a workspace open at a temp directory.
// It calls the embedded core.App's OpenWorkspace, not wailsapp's own —
// that one also writes the "reopen this next launch" pref into the real
// user config directory, which a test has no business touching.
func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	a := NewApp()
	if _, err := a.App.OpenWorkspace(root); err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	return a, root
}

func jsonResponse(body string) *httpengine.Response {
	return &httpengine.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       body,
		SizeBytes:  len(body),
	}
}

func TestResponseCacheRoundTrip(t *testing.T) {
	a, root := newTestApp(t)

	a.saveResponseCache("r_abc123", jsonResponse(`{"ok":true}`))

	got, err := a.loadResponseCache("r_abc123")
	if err != nil {
		t.Fatalf("loadResponseCache: %v", err)
	}
	if got.Body != `{"ok":true}` || got.StatusCode != 200 {
		t.Fatalf("round trip lost data: %+v", got)
	}
	// Truncated/BodyFile are wailsapp's presentation concern, never
	// persisted — loading must always hand back an inline body.
	if got.Truncated || got.BodyFile != "" {
		t.Fatalf("expected an inline body from the cache, got Truncated=%v BodyFile=%q", got.Truncated, got.BodyFile)
	}

	// The extension comes from the Content-Type, so the "..." menu opens
	// the file in something sensible.
	path, err := a.responseCacheBodyPath("r_abc123")
	if err != nil {
		t.Fatalf("responseCacheBodyPath: %v", err)
	}
	if filepath.Ext(path) != ".json" {
		t.Fatalf("expected a .json body file, got %q", path)
	}
	if !strings.HasPrefix(path, filepath.Join(root, ".cache", "responses")) {
		t.Fatalf("body file escaped the workspace cache: %q", path)
	}

	// A .gitignore lands in .cache so a version-controlled workspace
	// doesn't see this machine-local cache as untracked files.
	if _, err := os.Stat(filepath.Join(root, ".cache", ".gitignore")); err != nil {
		t.Fatalf("expected .cache/.gitignore: %v", err)
	}
}

// TestResponseCacheReplacesBodyOnExtensionChange covers the glob-and-
// delete in saveResponseCache: the same request returning a different
// Content-Type must not leave the previous body file behind under its
// old extension, or responseCacheBodyPath could return the stale one.
func TestResponseCacheReplacesBodyOnExtensionChange(t *testing.T) {
	a, _ := newTestApp(t)

	a.saveResponseCache("r_abc123", jsonResponse(`{"ok":true}`))
	a.saveResponseCache("r_abc123", &httpengine.Response{
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"text/plain"}},
		Body:       "plain now",
	})

	path, err := a.responseCacheBodyPath("r_abc123")
	if err != nil {
		t.Fatalf("responseCacheBodyPath: %v", err)
	}
	if filepath.Ext(path) != ".txt" {
		t.Fatalf("expected the body file to follow the new Content-Type, got %q", path)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "r_abc123.body.*"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly one body file, got %v", matches)
	}
}

// TestResponseCacheRejectsPathTraversal is the regression test for an
// itemID reaching the filesystem unchecked. The IDs these functions take
// come from the frontend and the control API, and filepath.Join *cleans*
// a "../" rather than refusing it — so before the itemIDPattern guard,
// a crafted ID could read, open in an external editor, or delete files
// outside the cache directory.
func TestResponseCacheRejectsPathTraversal(t *testing.T) {
	a, root := newTestApp(t)

	// A file that a traversing ID would be able to reach if the guard
	// weren't there. Named to match what deleteResponseCache removes.
	outside := filepath.Join(root, "victim.meta.json")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	bad := []string{
		"../victim",
		"../../victim",
		`..\victim`,
		"sub/victim",
		"r_abc*",   // a glob metacharacter would match other items' files
		"r_abc[1]", // ditto
		"",
	}

	for _, id := range bad {
		t.Run(id, func(t *testing.T) {
			if got := a.cachePath(id, ".meta.json"); got != "" {
				t.Fatalf("cachePath accepted %q -> %q", id, got)
			}
			if _, err := a.responseCacheBodyPath(id); err == nil {
				t.Fatalf("responseCacheBodyPath accepted %q", id)
			}
			if _, err := a.loadResponseCache(id); err == nil {
				t.Fatalf("loadResponseCache accepted %q", id)
			}
			// Must be inert, not just unhelpful.
			a.saveResponseCache(id, jsonResponse(`{"pwned":true}`))
			a.deleteResponseCache(id)
		})
	}

	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep me" {
		t.Fatalf("a file outside the cache was touched: err=%v data=%q", err, data)
	}
}

// TestTrimLargeBody covers the swap ExecuteRequest and GetCachedResponse
// both rely on: a body over the threshold is blanked out of the response
// that crosses the Wails bridge (and gets re-mirrored by GET
// /api/ui/state on every keystroke), with BodyFile pointing at the cache
// file that holds the real thing.
func TestTrimLargeBody(t *testing.T) {
	a, _ := newTestApp(t)

	small := jsonResponse(strings.Repeat("s", 32))
	a.saveResponseCache("r_small0", small)
	a.trimLargeBody("r_small0", small)
	if small.Truncated || small.BodyFile != "" || len(small.Body) != 32 {
		t.Fatalf("a small body should be left inline, got %+v", *small)
	}

	big := jsonResponse(strings.Repeat("b", int(httpengine.LargeResponseThreshold)+1))
	a.saveResponseCache("r_big000", big)
	a.trimLargeBody("r_big000", big)
	if !big.Truncated {
		t.Fatal("expected Truncated for a body over LargeResponseThreshold")
	}
	if big.Body != "" {
		t.Fatalf("expected the body blanked, still holds %d bytes", len(big.Body))
	}
	if big.BodyFile == "" {
		t.Fatal("expected BodyFile to point at the cache file")
	}
	data, err := os.ReadFile(big.BodyFile)
	if err != nil {
		t.Fatalf("reading BodyFile: %v", err)
	}
	if int64(len(data)) != httpengine.LargeResponseThreshold+1 {
		t.Fatalf("cache file holds %d bytes, want the whole %d", len(data), httpengine.LargeResponseThreshold+1)
	}
}

// TestDeleteResponseCache confirms deleting a request drops both of its
// cache files, so a reused ID can't resurface a stale response.
func TestDeleteResponseCache(t *testing.T) {
	a, _ := newTestApp(t)

	a.saveResponseCache("r_gone00", jsonResponse(`{"ok":true}`))
	if _, err := a.loadResponseCache("r_gone00"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	a.deleteResponseCache("r_gone00")

	if _, err := a.loadResponseCache("r_gone00"); err == nil {
		t.Fatal("expected the cached response to be gone")
	}
	if _, err := a.responseCacheBodyPath("r_gone00"); err == nil {
		t.Fatal("expected the body file to be gone")
	}
}

// TestClearResponseCache covers the settings window's whole-workspace
// button — every entry goes, not just the selected request's.
func TestClearResponseCache(t *testing.T) {
	a, _ := newTestApp(t)

	a.saveResponseCache("r_one000", jsonResponse(`{"n":1}`))
	a.saveResponseCache("r_two000", jsonResponse(`{"n":2}`))

	if err := a.clearResponseCache(); err != nil {
		t.Fatalf("clearResponseCache: %v", err)
	}
	for _, id := range []string{"r_one000", "r_two000"} {
		if _, err := a.loadResponseCache(id); err == nil {
			t.Fatalf("%s survived clearResponseCache", id)
		}
	}
}

// TestResponseCacheWithoutWorkspace confirms the "no workspace open"
// path is inert rather than a nil-dereference — the control API can
// reach these before any workspace exists.
func TestResponseCacheWithoutWorkspace(t *testing.T) {
	a := NewApp()

	a.saveResponseCache("r_abc123", jsonResponse(`{"ok":true}`))
	a.deleteResponseCache("r_abc123")
	if err := a.clearResponseCache(); err != nil {
		t.Fatalf("clearResponseCache with no workspace: %v", err)
	}
	if _, err := a.loadResponseCache("r_abc123"); err == nil {
		t.Fatal("expected an error with no workspace open")
	}
}
