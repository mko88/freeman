// Package store reads and writes Freeman's plain-file workspace format
// (collections/*/collection.json, environments/*.json). It depends only
// on domain, and treats the files on disk as the source of truth — it
// caches nothing itself.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// writeJSON marshals v with stable 2-space indentation (struct field
// order, not map iteration order) and a trailing newline, so collection
// and environment files diff cleanly in git.
//
// Written atomically — to a temp file in the same directory, then
// renamed over path — rather than truncating path in place. A plain
// os.WriteFile lets a concurrent reader (e.g. the control API's GET
// /api/collections/{id} or /api/environments/{id}, served on its own
// goroutine, hitting the same file a Wails-bound Save call just wrote)
// observe a torn/truncated file mid-write and fail to parse it. This was
// found live: scripts/test_control_api.py's rapid-fire regression check
// polls right after firing two back-to-back saveEnvironment actions and
// hit exactly this. os.Rename is atomic on the same volume (both POSIX
// rename(2) and Windows' MoveFileEx w/ MOVEFILE_REPLACE_EXISTING, which
// os.Rename uses) — a reader always sees either the old or the new file
// in full, never a partial one.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return retryOnWindowsFileLock(func() error { return os.Rename(tmpPath, path) })
}

// readFile is os.ReadFile with the same retry: opening path for read can
// just as transiently fail while a rename is briefly replacing it (the
// other side of the same race renameWithRetry guards — see
// retryOnWindowsFileLock).
func readFile(path string) ([]byte, error) {
	var data []byte
	err := retryOnWindowsFileLock(func() error {
		var readErr error
		data, readErr = os.ReadFile(path)
		return readErr
	})
	return data, err
}

// retryOnWindowsFileLock retries fn a few times on failure. Windows can
// transiently fail either side of a rename-over-an-existing-file with
// "Access is denied" / "used by another process" if something else
// briefly has the destination open even just to read it — a concurrent
// GET /api/collections/{id} or /api/environments/{id}, a virus scanner, a
// search indexer. POSIX has no such restriction (renaming over an open
// file, or opening a file mid-rename, both just work), so this loop is a
// no-op there — the first attempt always succeeds. Found live: a burst
// of saveEnvironment/saveRequest calls immediately followed by
// scripts/test_control_api.py polling (reading) the same files hit both
// sides of this intermittently.
//
// Stops immediately (no retries) on a "file does not exist" error — that
// one's never transient, and is a normal, expected result in callers
// like LoadEnvironment's optional .local.json sidecar; retrying it would
// just add a pointless delay to the common case of it genuinely not
// being there.
func retryOnWindowsFileLock(fn func() error) error {
	const attempts = 10
	var err error
	for i := 0; i < attempts; i++ {
		err = fn()
		if err == nil || os.IsNotExist(err) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}
