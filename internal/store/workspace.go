package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Workspace is a directory the user has pointed Freeman at, containing a
// collections/ and an environments/ subdirectory. It holds no cached
// domain data itself — only enough to load/save individual files on
// demand, so the files on disk stay the source of truth.
type Workspace struct {
	Root string

	// CollectionPaths maps a collection's ID to its collection.json path.
	CollectionPaths map[string]string
	// EnvironmentPaths maps an environment's ID to its tracked <slug>.json
	// path (SaveEnvironment derives the .local.json path from it).
	EnvironmentPaths map[string]string
}

// DiscoverWorkspace scans root/collections/*/collection.json and
// root/environments/*.json (skipping *.local.json override files) and
// returns a Workspace indexing what it found by ID. A brand new workspace
// with neither subdirectory yet is valid and simply comes back empty.
func DiscoverWorkspace(root string) (*Workspace, error) {
	ws := &Workspace{
		Root:             root,
		CollectionPaths:  map[string]string{},
		EnvironmentPaths: map[string]string{},
	}

	collFiles, err := filepath.Glob(filepath.Join(root, "collections", "*", "collection.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(collFiles)
	for _, path := range collFiles {
		c, err := LoadCollection(path)
		if err != nil {
			return nil, err
		}
		ws.CollectionPaths[c.ID] = path
	}

	envFiles, err := filepath.Glob(filepath.Join(root, "environments", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(envFiles)
	for _, path := range envFiles {
		if strings.HasSuffix(path, ".local.json") {
			continue
		}
		e, err := LoadEnvironment(path)
		if err != nil {
			return nil, err
		}
		ws.EnvironmentPaths[e.ID] = path
	}

	return ws, nil
}

// EnsureLayout creates the collections/ and environments/ directories and
// a .gitignore (if one doesn't already exist) so *.local.json secret
// overrides never get committed by accident.
func EnsureLayout(root string) error {
	if err := os.MkdirAll(filepath.Join(root, "collections"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "environments"), 0o755); err != nil {
		return err
	}
	gitignore := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(gitignore); os.IsNotExist(err) {
		return os.WriteFile(gitignore, []byte("*.local.json\n"), 0o644)
	}
	return nil
}
