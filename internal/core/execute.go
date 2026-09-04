package core

import (
	"context"
	"fmt"

	"freeman/internal/httpengine"
	"freeman/internal/store"
)

// ExecuteRequest loads itemID from collectionID, resolves variables from
// environmentID (if any), and runs the request. ctx is the caller's own
// (e.g. an HTTP request's context, or a desktop app's startup context),
// not stored on App — each call gets independent cancellation.
func (a *App) ExecuteRequest(ctx context.Context, collectionID, itemID, environmentID string) (*httpengine.Response, error) {
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	c, err := store.LoadCollection(a.ws.CollectionPaths[collectionID])
	if err != nil {
		return nil, err
	}
	item := c.FindItem(itemID)
	if item == nil {
		return nil, fmt.Errorf("request %q not found in collection %q", itemID, collectionID)
	}

	vars := map[string]string{}
	if environmentID != "" {
		env, err := store.LoadEnvironment(a.ws.EnvironmentPaths[environmentID])
		if err != nil {
			return nil, err
		}
		vars = env.ResolvedVariables()
	}

	return httpengine.Execute(ctx, *item, vars)
}
