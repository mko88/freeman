package core

import (
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
// otherwise overwrites the existing file, and persists it.
func (a *App) SaveEnvironment(env domain.Environment) (*domain.Environment, error) {
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	path, exists := a.ws.EnvironmentPaths[env.ID]
	if !exists {
		env.ID = domain.NewID("e_")
		path = filepath.Join(a.ws.Root, "environments", slugify(env.Name, env.ID)+".json")
		a.ws.EnvironmentPaths[env.ID] = path
	}
	if err := store.SaveEnvironment(path, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

func (a *App) createEnvironment(name string) (*domain.Environment, error) {
	return a.SaveEnvironment(domain.Environment{FormatVersion: "1", Name: name})
}
