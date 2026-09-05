package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"freeman/internal/domain"
	"freeman/internal/store"
)

type EnvironmentSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (a *App) ListEnvironments() ([]EnvironmentSummary, error) {
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	summaries := make([]EnvironmentSummary, 0, len(a.ws.EnvironmentPaths))
	for id, path := range a.ws.EnvironmentPaths {
		e, err := store.LoadEnvironment(path)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, EnvironmentSummary{ID: id, Name: e.Name})
	}
	return summaries, nil
}

func (a *App) GetEnvironment(id string) (*domain.Environment, error) {
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
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	path, exists := a.ws.EnvironmentPaths[env.ID]
	if !exists {
		env.ID = domain.NewID("e_")
		path = filepath.Join(a.ws.Root, "environments", slugify(env.Name, env.ID)+".json")
		a.ws.EnvironmentPaths[env.ID] = path
	} else if want := filepath.Join(a.ws.Root, "environments", slugify(env.Name, env.ID)+".json"); want != path {
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
	if err := os.Remove(path); err != nil {
		return err
	}
	os.Remove(store.LocalPathFor(path)) // best effort — sidecar may not exist
	delete(a.ws.EnvironmentPaths, id)
	return nil
}

func (a *App) createEnvironment(name string) (*domain.Environment, error) {
	return a.SaveEnvironment(domain.Environment{FormatVersion: "1", Name: name})
}
