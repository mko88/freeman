package wailsapp

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// openExternally asks the OS to open path with whatever application is
// associated with it — the same effect as double-clicking it in a file
// manager. There's no cross-platform stdlib call for this; each OS has
// its own shell command.
func openExternally(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// The empty string is `start`'s own (required) window-title
		// argument, not part of the path — without it, `start` treats a
		// quoted path as the title and never opens it.
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("opening files externally isn't supported on %s", runtime.GOOS)
	}
	return cmd.Start()
}

// openInFileExplorer asks the OS's file manager to show path — Explorer
// and Finder both support opening a folder with a specific file already
// selected in it; Linux has no universal equivalent, so this falls back
// to just opening path's containing folder.
func openInFileExplorer(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// /select, (comma attached, no space before the path) is
		// Explorer's own documented syntax for this.
		cmd = exec.Command("explorer", "/select,"+path)
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "linux":
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	default:
		return fmt.Errorf("opening a file manager isn't supported on %s", runtime.GOOS)
	}
	return cmd.Start()
}
