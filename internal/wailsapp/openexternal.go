package wailsapp

import (
	"fmt"
	"os/exec"
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
