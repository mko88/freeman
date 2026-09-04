// Package appdata resolves the per-user, per-OS application-data
// directory Freeman uses for state that isn't part of any workspace
// (currently just which workspace folder to reopen on launch).
package appdata

import (
	"os"
	"path/filepath"
)

const dirName = "freeman"

// Dir returns the freeman app-data directory, creating it if it doesn't
// exist yet: %AppData%\freeman on Windows, $XDG_CONFIG_HOME/freeman (or
// ~/.config/freeman) on Linux, per os.UserConfigDir. Returns "" if the
// OS-standard location can't be determined; callers treat "" as "skip
// this" rather than failing.
func Dir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, dirName)
	os.MkdirAll(dir, 0o755)
	return dir
}
