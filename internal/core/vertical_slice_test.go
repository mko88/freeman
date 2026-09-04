package core

import (
	"context"
	"strings"
	"testing"

	"freeman/internal/domain"
)

// TestVerticalSlice drives the exact same App methods both transports
// (wailsapp for desktop, httpapi for the server) wrap — open a workspace,
// save an environment variable, save a request that references it with
// {{baseUrl}}, and execute it against a real public API — to prove the
// whole stack (file persistence, variable substitution, HTTP execution)
// works end to end, independent of either transport.
func TestVerticalSlice(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	app := NewApp()

	ws, err := app.OpenWorkspace(root)
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	if len(ws.Collections) != 1 || len(ws.Environments) != 1 {
		t.Fatalf("expected a default collection and environment, got %+v", ws)
	}
	collectionID := ws.Collections[0].ID
	environmentID := ws.Environments[0].ID

	env, err := app.GetEnvironment(environmentID)
	if err != nil {
		t.Fatalf("GetEnvironment: %v", err)
	}
	env.Variables = append(env.Variables, domain.Variable{Key: "baseUrl", Value: "https://httpbin.org", Enabled: true})
	if _, err := app.SaveEnvironment(*env); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}

	item := domain.Item{
		Name:   "Get",
		Method: "GET",
		URL:    "{{baseUrl}}/get",
		Params: []domain.QueryParam{{Key: "greeting", Value: "hello", Enabled: true}},
	}
	saved, err := app.SaveRequest(collectionID, item)
	if err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	resp, err := app.ExecuteRequest(ctx, collectionID, saved.ID, environmentID)
	if err != nil {
		t.Fatalf("ExecuteRequest: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	// httpbin's /get echoes the request URL it received, proving {{baseUrl}}
	// was actually substituted rather than sent literally.
	if !strings.Contains(resp.Body, `"url": "https://httpbin.org/get?greeting=hello"`) {
		t.Fatalf("response did not reflect substituted URL:\n%s", resp.Body)
	}

	// Reload from disk to confirm the request and variable really persisted
	// (not just held in memory), matching what a relaunched app would see.
	reloaded, err := app.GetCollection(collectionID)
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if got := reloaded.FindItem(saved.ID); got == nil || got.URL != "{{baseUrl}}/get" {
		t.Fatalf("saved request did not round-trip: %+v", got)
	}

	if err := app.DeleteRequest(collectionID, saved.ID); err != nil {
		t.Fatalf("DeleteRequest: %v", err)
	}
	afterDelete, err := app.GetCollection(collectionID)
	if err != nil {
		t.Fatalf("GetCollection after delete: %v", err)
	}
	if afterDelete.FindItem(saved.ID) != nil {
		t.Fatalf("expected %q to be gone after DeleteRequest", saved.ID)
	}
	if err := app.DeleteRequest(collectionID, saved.ID); err == nil {
		t.Fatal("expected deleting an already-deleted request to error")
	}
}
