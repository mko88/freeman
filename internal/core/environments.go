package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"freeman/internal/domain"
	"freeman/internal/store"
)

// EnvironmentSummary is what the settings list shows for an environment
// it hasn't opened: enough to name it and say how much is in it, without
// loading its variables.
type EnvironmentSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	VariableCount int    `json:"variableCount"`
}

func (a *App) ListEnvironments() ([]EnvironmentSummary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listEnvironments()
}

// listEnvironments assumes a.mu is held (see the *Locked-helper note on App).
func (a *App) listEnvironments() ([]EnvironmentSummary, error) {
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	summaries := make([]EnvironmentSummary, 0, len(a.ws.EnvironmentPaths))
	for id, path := range a.ws.EnvironmentPaths {
		e, err := store.LoadEnvironment(path)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, EnvironmentSummary{ID: id, Name: e.Name, VariableCount: len(e.Variables)})
	}
	return summaries, nil
}

func (a *App) GetEnvironment(id string) (*domain.Environment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return store.LoadEnvironment(a.ws.EnvironmentPaths[id])
}

// SaveEnvironment creates env (assigning an ID and file path) if it's new,
// otherwise overwrites the existing file, and persists it. If a rename
// makes the name-derived filename stale, the tracked file (and its
// .local.json secrets sidecar, if any) is renamed to match.
func (a *App) SaveEnvironment(env domain.Environment) (*domain.Environment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.saveEnvironment(env)
}

// saveEnvironment assumes a.mu is held (see the *Locked-helper note on App).
func (a *App) saveEnvironment(env domain.Environment) (*domain.Environment, error) {
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	envPath := func(slug string) string {
		return filepath.Join(a.ws.Root, "environments", slug+".json")
	}
	path, exists := a.ws.EnvironmentPaths[env.ID]
	if !exists {
		env.ID = domain.NewID("e_")
		path = uniquePath(slugify(env.Name, env.ID), envPath, a.ws.EnvironmentPaths, env.ID)
		a.ws.EnvironmentPaths[env.ID] = path
	} else if want := uniquePath(slugify(env.Name, env.ID), envPath, a.ws.EnvironmentPaths, env.ID); want != path {
		if err := os.Rename(path, want); err == nil {
			os.Rename(store.LocalPathFor(path), store.LocalPathFor(want)) // best effort — sidecar may not exist
			a.ws.EnvironmentPaths[env.ID] = want
			path = want
		}
	}
	if err := store.SaveEnvironment(path, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// DeleteEnvironment removes an environment's tracked file (and its
// .local.json secrets sidecar, if any). It refuses to delete the last
// one — the app assumes at least one environment always exists (see
// OpenWorkspace, which creates a default).
func (a *App) DeleteEnvironment(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	path, ok := a.ws.EnvironmentPaths[id]
	if !ok {
		return fmt.Errorf("environment %q not found", id)
	}
	if len(a.ws.EnvironmentPaths) <= 1 {
		return errors.New("can't delete the last environment")
	}
	// A file that has already gone still counts as deleted: otherwise an
	// entry whose file vanished can never be removed from the list.
	if err := os.Remove(path); !removedOrMissing(err) {
		return err
	}
	os.Remove(store.LocalPathFor(path)) // best effort — sidecar may not exist
	delete(a.ws.EnvironmentPaths, id)
	return nil
}

// createEnvironment is only called from OpenWorkspace, which holds a.mu.
func (a *App) createEnvironment(name string) (*domain.Environment, error) {
	return a.saveEnvironment(domain.Environment{FormatVersion: "1", Name: name})
}
