package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"freeman/internal/domain"
)

// TestNewCollectionHasEmptyNotNilItems guards against domain.Collection's
// Items field (deliberately without `omitempty` — see its doc comment)
// round-tripping as JSON "items": null instead of "items": [] for a
// brand-new, empty collection. A nil slice there is otherwise
// indistinguishable from a real empty one in Go, but not in JSON — and a
// frontend reading collection.items.length has no reason to expect
// null.
func TestNewCollectionHasEmptyNotNilItems(t *testing.T) {
	root := t.TempDir()
	app := NewApp()

	ws, err := app.OpenWorkspace(root)
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	if len(ws.Collections) != 1 {
		t.Fatalf("expected one default collection, got %+v", ws.Collections)
	}

	c, err := app.GetCollection(ws.Collections[0].ID)
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if c.Items == nil {
		t.Fatal("expected Items to be a non-nil empty slice, got nil")
	}

	// Byte-level check on the actual file: this is what the frontend
	// receives over the wire, and json.Unmarshal into a Go struct
	// wouldn't distinguish "[]" from a written-out empty-but-present
	// array the way the in-memory nil check above might mask.
	path := filepath.Join(root, "collections", "my-requests", "collection.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading collection.json: %v", err)
	}
	if strings.Contains(string(data), `"items":null`) || strings.Contains(string(data), `"items": null`) {
		t.Fatalf("collection.json serialized items as null, not []: %s", data)
	}
}

// Collections slugify the same way environments do, so two called
// "New collection" shared one directory and the second overwrote the
// first. os.RemoveAll spared them the undeletable half of the bug, but
// not the data loss.
func TestCollectionsWithTheSameNameGetTheirOwnDirectories(t *testing.T) {
	root := t.TempDir()
	app := NewApp()
	if _, err := app.OpenWorkspace(root); err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}

	first, err := app.CreateCollection("New collection")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := app.CreateCollection("New collection")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if app.ws.CollectionPaths[first.ID] == app.ws.CollectionPaths[second.ID] {
		t.Fatalf("both collections share a directory: %s", app.ws.CollectionPaths[first.ID])
	}

	// The first one's contents survive the second being created.
	if _, err := app.SaveRequest(first.ID, domain.Item{Name: "Only in the first", Method: "GET"}); err != nil {
		t.Fatalf("save into the first: %v", err)
	}
	reloaded, err := app.GetCollection(first.ID)
	if err != nil {
		t.Fatalf("reload the first: %v", err)
	}
	if len(reloaded.Items) != 1 {
		t.Fatalf("the first collection lost its request: %+v", reloaded.Items)
	}
}
