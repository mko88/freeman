package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
