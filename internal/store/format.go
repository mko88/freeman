// Package store reads and writes Freeman's plain-file workspace format
// (collections/*/collection.json, environments/*.json). It depends only
// on domain, and treats the files on disk as the source of truth — it
// caches nothing itself.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	return os.Rename(tmpPath, path)
}
