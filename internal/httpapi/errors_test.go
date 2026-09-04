package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"freeman/internal/core"
)

// TestHealthAndNoWorkspace covers the desktop control API's two new
// behaviors: a nil static FS means "/" isn't mounted at all (the GUI
// itself is the frontend, not this server), and hitting a workspace-
// scoped route before any workspace is open returns a clean 409 instead
// of panicking on a nil workspace.
func TestHealthAndNoWorkspace(t *testing.T) {
	app := core.NewApp()
	srv := httptest.NewServer(NewHandler(app, nil))
	defer srv.Close()

	healthResp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", healthResp.StatusCode)
	}

	rootResp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer rootResp.Body.Close()
	if rootResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unmounted root, got %d", rootResp.StatusCode)
	}

	collResp, err := http.Get(srv.URL + "/api/collections")
	if err != nil {
		t.Fatalf("GET /api/collections: %v", err)
	}
	defer collResp.Body.Close()
	if collResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 before a workspace is open, got %d", collResp.StatusCode)
	}
}
