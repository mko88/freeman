package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"freeman/internal/domain"
)

func TestEnvironmentCRUD(t *testing.T) {
	root := t.TempDir()
	app := NewApp()
	ws, err := app.OpenWorkspace(root)
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defaultID := ws.Environments[0].ID

	// Create.
	created, err := app.SaveEnvironment(domain.Environment{FormatVersion: "1", Name: "Staging"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || created.ID == defaultID {
		t.Fatalf("expected a fresh ID for a new environment, got %q", created.ID)
	}
	stagingPath := filepath.Join(root, "environments", slugify("Staging", created.ID)+".json")
	if _, err := os.Stat(stagingPath); err != nil {
		t.Fatalf("expected %s on disk: %v", stagingPath, err)
	}

	// Rename — the file follows the name.
	renamed, err := app.SaveEnvironment(domain.Environment{FormatVersion: "1", ID: created.ID, Name: "Production"})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "Production" {
		t.Fatalf("expected Name Production, got %q", renamed.Name)
	}
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("expected the old %s to be gone after rename", stagingPath)
	}
	prodPath := filepath.Join(root, "environments", slugify("Production", created.ID)+".json")
	if _, err := os.Stat(prodPath); err != nil {
		t.Fatalf("expected %s after rename: %v", prodPath, err)
	}

	// Delete.
	if err := app.DeleteEnvironment(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(prodPath); !os.IsNotExist(err) {
		t.Fatalf("expected %s gone after delete", prodPath)
	}
	if _, err := app.GetEnvironment(created.ID); err == nil {
		t.Fatal("expected GetEnvironment to fail for a deleted environment")
	}

	// The last one can't be deleted.
	if err := app.DeleteEnvironment(defaultID); err == nil {
		t.Fatal("expected DeleteEnvironment to refuse the last environment")
	}
}

// TestEnvironmentConcurrentListAndRename hammers ListEnvironments while a
// second goroutine renames an environment (a rename moves the file to a
// new path). Before App.mu serialized these, the list would either race
// the EnvironmentPaths map or read a path a concurrent rename had just
// moved away — a 500 in the desktop control API, hit intermittently by
// scripts/test_control_api.py at --delay 0. Run with -race.
func TestEnvironmentConcurrentListAndRename(t *testing.T) {
	root := t.TempDir()
	app := NewApp()
	if _, err := app.OpenWorkspace(root); err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	created, err := app.SaveEnvironment(domain.Environment{FormatVersion: "1", Name: "Name 0"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 64)

	go func() {
		defer wg.Done()
		for i := 1; i <= 40; i++ {
			if _, err := app.SaveEnvironment(domain.Environment{
				FormatVersion: "1", ID: created.ID, Name: fmt.Sprintf("Name %d", i),
			}); err != nil {
				errs <- fmt.Errorf("rename %d: %w", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if _, err := app.ListEnvironments(); err != nil {
				errs <- fmt.Errorf("list %d: %w", i, err)
				return
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// Two environments with the same name used to slugify to one filename:
// the second overwrote the first, and deleting either left the other
// pointing at a file that was gone — so listing the workspace failed and
// that entry could never be deleted. Reported from the settings window,
// where "New environment" is what both of them are called.
func TestEnvironmentsWithTheSameNameGetTheirOwnFiles(t *testing.T) {
	root := t.TempDir()
	app := NewApp()
	if _, err := app.OpenWorkspace(root); err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}

	first, err := app.SaveEnvironment(domain.Environment{Name: "New environment"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := app.SaveEnvironment(domain.Environment{Name: "New environment"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("two saves should be two environments")
	}

	firstPath := app.ws.EnvironmentPaths[first.ID]
	secondPath := app.ws.EnvironmentPaths[second.ID]
	if firstPath == secondPath {
		t.Fatalf("both environments share a file: %s", firstPath)
	}
	for _, p := range []string{firstPath, secondPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s on disk: %v", p, err)
		}
	}

	// Deleting one leaves the other readable, which is what broke:
	// workspaceInfo loads every environment to count its variables.
	if err := app.DeleteEnvironment(first.ID); err != nil {
		t.Fatalf("delete first: %v", err)
	}
	if _, err := app.CurrentWorkspace(); err != nil {
		t.Fatalf("listing the workspace after a delete: %v", err)
	}
	if err := app.DeleteEnvironment(second.ID); err != nil {
		t.Fatalf("delete second: %v", err)
	}
}

// The other half: an entry whose file has already gone still has to be
// removable, or it stays in the list forever with no way to clear it.
func TestDeleteEnvironmentWhoseFileIsAlreadyGone(t *testing.T) {
	root := t.TempDir()
	app := NewApp()
	if _, err := app.OpenWorkspace(root); err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	env, err := app.SaveEnvironment(domain.Environment{Name: "Doomed"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := os.Remove(app.ws.EnvironmentPaths[env.ID]); err != nil {
		t.Fatalf("removing the file behind its back: %v", err)
	}
	if err := app.DeleteEnvironment(env.ID); err != nil {
		t.Fatalf("deleting an environment whose file is gone: %v", err)
	}
	if _, ok := app.ws.EnvironmentPaths[env.ID]; ok {
		t.Error("the entry survived the delete")
	}
}
