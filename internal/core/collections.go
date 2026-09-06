package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"freeman/internal/domain"
	"freeman/internal/store"
)

type CollectionSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (a *App) ListCollections() ([]CollectionSummary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listCollections()
}

// listCollections assumes a.mu is held (see the *Locked-helper note on App).
func (a *App) listCollections() ([]CollectionSummary, error) {
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	summaries := make([]CollectionSummary, 0, len(a.ws.CollectionPaths))
	for id, path := range a.ws.CollectionPaths {
		c, err := store.LoadCollection(path)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, CollectionSummary{ID: id, Name: c.Name})
	}
	return summaries, nil
}

func (a *App) GetCollection(id string) (*domain.Collection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return store.LoadCollection(a.ws.CollectionPaths[id])
}

// SaveRequest upserts item (assigning an ID and Type if it's new) into
// collectionID's tree and persists the file. Folder nesting isn't
// exposed by the frontend yet, so new items land at the collection root.
func (a *App) SaveRequest(collectionID string, item domain.Item) (*domain.Item, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	path := a.ws.CollectionPaths[collectionID]
	c, err := store.LoadCollection(path)
	if err != nil {
		return nil, err
	}

	if item.ID == "" {
		item.ID = domain.NewID("r_")
	}
	item.Type = domain.ItemTypeRequest

	c.UpsertItem(item)
	if err := store.SaveCollection(path, c); err != nil {
		return nil, err
	}
	return &item, nil
}

// DeleteRequest removes itemID from collectionID's tree and persists the
// change. Deleting an item that doesn't exist is reported as an error
// rather than a silent no-op, so a caller (UI or script) gets clear
// feedback instead of assuming it worked.
func (a *App) DeleteRequest(collectionID, itemID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	path := a.ws.CollectionPaths[collectionID]
	c, err := store.LoadCollection(path)
	if err != nil {
		return err
	}
	if !c.RemoveItem(itemID) {
		return fmt.Errorf("request %q not found in collection %q", itemID, collectionID)
	}
	return store.SaveCollection(path, c)
}

// CreateCollection adds an empty collection to the open workspace.
func (a *App) CreateCollection(name string) (*domain.Collection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return a.createCollection(name)
}

// RenameCollection changes a collection's display name and moves its
// directory to match, so the workspace stays browsable by name on disk —
// the same courtesy saveEnvironment does for an environment's file. A
// collection is a directory holding collection.json, so it's the
// directory that carries the name.
//
// A failed move is not fatal: the name still changes, and the stale
// directory keeps working because the path is tracked, not derived.
func (a *App) RenameCollection(id, name string) (*domain.Collection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	path, ok := a.ws.CollectionPaths[id]
	if !ok {
		return nil, fmt.Errorf("collection %q not found", id)
	}
	c, err := store.LoadCollection(path)
	if err != nil {
		return nil, err
	}
	c.Name = name

	dir := filepath.Dir(path)
	if want := filepath.Join(a.ws.Root, "collections", slugify(name, id)); want != dir {
		if err := os.Rename(dir, want); err == nil {
			path = filepath.Join(want, "collection.json")
			a.ws.CollectionPaths[id] = path
		}
	}
	if err := store.SaveCollection(path, c); err != nil {
		return nil, err
	}
	return c, nil
}

// DeleteCollection removes a collection's directory and everything in
// it. It refuses the last one — the app assumes a workspace always has
// somewhere to put a request (see OpenWorkspace, which creates a default
// when it finds none).
func (a *App) DeleteCollection(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	path, ok := a.ws.CollectionPaths[id]
	if !ok {
		return fmt.Errorf("collection %q not found", id)
	}
	if len(a.ws.CollectionPaths) <= 1 {
		return errors.New("can't delete the last collection")
	}
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		return err
	}
	delete(a.ws.CollectionPaths, id)
	return nil
}

// createCollection assumes a.mu is held (see the *Locked-helper note on App).
func (a *App) createCollection(name string) (*domain.Collection, error) {
	id := domain.NewID("c_")
	c := &domain.Collection{
		FormatVersion: "1",
		ID:            id,
		Name:          name,
		// Items has no `omitempty` (see domain.Collection) — a nil slice
		// would round-trip as JSON "null" rather than "[]", which is not
		// what anyone would want to see (or hand-edit) in a fresh
		// collection.json.
		Items: []domain.Item{},
	}
	path := filepath.Join(a.ws.Root, "collections", slugify(name, id), "collection.json")
	if err := store.SaveCollection(path, c); err != nil {
		return nil, err
	}
	a.ws.CollectionPaths[id] = path
	return c, nil
}
