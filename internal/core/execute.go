package core

import (
	"context"
	"fmt"

	"freeman/internal/domain"
	"freeman/internal/httpengine"
	"freeman/internal/store"
)

// ExecuteRequest loads itemID from collectionID, resolves variables from
// environmentID (if any), and runs the request. ctx is the caller's own
// (e.g. an HTTP request's context, or a desktop app's startup context),
// not stored on App — each call gets independent cancellation.
func (a *App) ExecuteRequest(ctx context.Context, collectionID, itemID, environmentID string) (*httpengine.Response, error) {
	// Everything that touches the workspace happens under a.mu; the
	// actual HTTP call (up to a 30s timeout) is deliberately outside it,
	// so a slow request doesn't block every other core operation.
	item, vars, err := a.resolveRequest(collectionID, itemID, environmentID)
	if err != nil {
		return nil, err
	}
	return httpengine.Execute(ctx, *item, vars)
}

func (a *App) resolveRequest(collectionID, itemID, environmentID string) (*domain.Item, map[string]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, nil, err
	}
	c, err := store.LoadCollection(a.ws.CollectionPaths[collectionID])
	if err != nil {
		return nil, nil, err
	}
	item := c.FindItem(itemID)
	if item == nil {
		return nil, nil, fmt.Errorf("request %q not found in collection %q", itemID, collectionID)
	}

	vars := map[string]string{}
	if environmentID != "" {
		env, err := store.LoadEnvironment(a.ws.EnvironmentPaths[environmentID])
		if err != nil {
			return nil, nil, err
		}
		vars = env.ResolvedVariables()
	}
	return item, vars, nil
}
