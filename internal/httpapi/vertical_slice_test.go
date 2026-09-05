package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"freeman/internal/core"
	"freeman/internal/domain"
	"freeman/internal/headercatalog"
)

func mustGet(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: status %d: %s", url, resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("GET %s: decode: %v", url, err)
	}
}

func mustPost(t *testing.T, url string, in, out any) {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: status %d: %s", url, resp.StatusCode, respBody)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("POST %s: decode: %v", url, err)
		}
	}
}

func mustDelete(t *testing.T, url string, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("build DELETE %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("DELETE %s: status %d, want %d: %s", url, resp.StatusCode, wantStatus, body)
	}
}

// TestVerticalSliceOverHTTP drives the same scenario as
// internal/core.TestVerticalSlice, but purely over HTTP against a real
// httptest server — proving the routing/JSON layer round-trips correctly,
// not just the core logic it wraps.
func TestVerticalSliceOverHTTP(t *testing.T) {
	root := t.TempDir()
	app := core.NewApp()
	ws, err := app.OpenWorkspace(root)
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	collectionID := ws.Collections[0].ID
	environmentID := ws.Environments[0].ID

	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	srv := httptest.NewServer(NewHandler(app, static))
	defer srv.Close()

	var env domain.Environment
	mustGet(t, srv.URL+"/api/environments/"+environmentID, &env)
	env.Variables = append(env.Variables, domain.Variable{Key: "baseUrl", Value: "https://httpbin.org", Enabled: true})
	mustPost(t, srv.URL+"/api/environments", env, nil)

	item := domain.Item{
		Name:   "Get",
		Method: "GET",
		URL:    "{{baseUrl}}/get",
		Params: []domain.QueryParam{{Key: "greeting", Value: "hello", Enabled: true}},
	}
	var saved domain.Item
	mustPost(t, srv.URL+"/api/collections/"+collectionID+"/requests", item, &saved)

	execReq := executeRequest{CollectionID: collectionID, ItemID: saved.ID, EnvironmentID: environmentID}
	var resp struct {
		StatusCode int    `json:"statusCode"`
		Body       string `json:"body"`
	}
	mustPost(t, srv.URL+"/api/execute", execReq, &resp)

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	// httpbin's /get echoes the request URL it received, proving {{baseUrl}}
	// was actually substituted rather than sent literally.
	if !strings.Contains(resp.Body, `"url": "https://httpbin.org/get?greeting=hello"`) {
		t.Fatalf("response did not reflect substituted URL:\n%s", resp.Body)
	}

	// DELETE the request, confirm it's gone, and confirm deleting it again
	// (now missing) reports an error instead of a silent success.
	mustDelete(t, srv.URL+"/api/collections/"+collectionID+"/requests/"+saved.ID, http.StatusNoContent)
	var afterDelete domain.Collection
	mustGet(t, srv.URL+"/api/collections/"+collectionID, &afterDelete)
	if afterDelete.FindItem(saved.ID) != nil {
		t.Fatalf("expected %q to be gone after DELETE", saved.ID)
	}
	mustDelete(t, srv.URL+"/api/collections/"+collectionID+"/requests/"+saved.ID, http.StatusInternalServerError)

	// Confirm the static handler serves the frontend for a browser hitting
	// the root path.
	homeResp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer homeResp.Body.Close()
	homeBody, _ := io.ReadAll(homeResp.Body)
	if !strings.Contains(string(homeBody), "<html>") {
		t.Fatalf("expected static index.html, got: %s", homeBody)
	}
}

// TestExecuteReturnsLargeBodyInline confirms the headless server hands
// back a body over httpengine.LargeResponseThreshold inline in the
// /api/execute reply — the desktop-only response cache is what trims
// large bodies now, and the server has no equivalent.
func TestExecuteReturnsLargeBodyInline(t *testing.T) {
	root := t.TempDir()
	app := core.NewApp()
	ws, err := app.OpenWorkspace(root)
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	collectionID := ws.Collections[0].ID

	big := strings.Repeat("y", 1<<20+1024)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(big))
	}))
	defer upstream.Close()

	srv := httptest.NewServer(NewHandler(app, nil))
	defer srv.Close()

	var saved domain.Item
	mustPost(t, srv.URL+"/api/collections/"+collectionID+"/requests",
		domain.Item{Name: "Big", Method: "GET", URL: upstream.URL}, &saved)

	var resp struct {
		Body      string `json:"body"`
		Truncated bool   `json:"truncated"`
		BodyFile  string `json:"bodyFile"`
	}
	mustPost(t, srv.URL+"/api/execute", executeRequest{CollectionID: collectionID, ItemID: saved.ID}, &resp)
	if resp.Truncated || resp.BodyFile != "" {
		t.Fatalf("expected the server to return a large body inline, got Truncated=%v BodyFile=%q", resp.Truncated, resp.BodyFile)
	}
	if resp.Body != big {
		t.Fatalf("expected the full %d-byte body inline, got %d bytes", len(big), len(resp.Body))
	}
}

// TestEnvironmentDeleteRoute covers POST (create) then DELETE /api/environments/{id},
// and that deleting the last one is refused.
func TestEnvironmentDeleteRoute(t *testing.T) {
	root := t.TempDir()
	app := core.NewApp()
	ws, err := app.OpenWorkspace(root)
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defaultID := ws.Environments[0].ID

	srv := httptest.NewServer(NewHandler(app, nil))
	defer srv.Close()

	var created domain.Environment
	mustPost(t, srv.URL+"/api/environments", domain.Environment{Name: "Scratch"}, &created)
	if created.ID == "" {
		t.Fatal("expected a created environment with an ID")
	}

	mustDelete(t, srv.URL+"/api/environments/"+created.ID, http.StatusNoContent)

	var list []map[string]any
	mustGet(t, srv.URL+"/api/environments", &list)
	for _, e := range list {
		if e["id"] == created.ID {
			t.Fatalf("environment %q still listed after DELETE", created.ID)
		}
	}

	mustDelete(t, srv.URL+"/api/environments/"+defaultID, http.StatusInternalServerError)
}

// TestCodegenRoute checks POST /api/codegen renders the posted item as a
// curl command, resolving {{var}} against the given environment.
func TestCodegenRoute(t *testing.T) {
	root := t.TempDir()
	app := core.NewApp()
	ws, err := app.OpenWorkspace(root)
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	envID := ws.Environments[0].ID

	srv := httptest.NewServer(NewHandler(app, nil))
	defer srv.Close()

	// Put a base URL in the environment so we can prove substitution.
	var env domain.Environment
	mustGet(t, srv.URL+"/api/environments/"+envID, &env)
	env.Variables = append(env.Variables, domain.Variable{Key: "base", Value: "https://api.example.com", Enabled: true})
	mustPost(t, srv.URL+"/api/environments", env, nil)

	body := codegenRequest{
		Item: domain.Item{
			Method:  "GET",
			URL:     "{{base}}/health",
			Headers: []domain.Header{{Key: "Accept", Value: "application/json", Enabled: true}},
		},
		EnvironmentID: envID,
		Format:        "curl",
	}
	var out struct {
		Code string `json:"code"`
	}
	mustPost(t, srv.URL+"/api/codegen", body, &out)

	if !strings.Contains(out.Code, "curl -X GET 'https://api.example.com/health'") {
		t.Fatalf("codegen did not substitute the URL var:\n%s", out.Code)
	}
	if !strings.Contains(out.Code, "-H 'Accept: application/json'") {
		t.Fatalf("codegen dropped the header:\n%s", out.Code)
	}
}

// TestHeaderCatalogRoute checks GET /api/headers serves the built-in
// catalog when the workspace has no headers.yaml, and merges one when it
// does.
func TestHeaderCatalogRoute(t *testing.T) {
	root := t.TempDir()
	app := core.NewApp()
	if _, err := app.OpenWorkspace(root); err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	srv := httptest.NewServer(NewHandler(app, nil))
	defer srv.Close()

	var got []headercatalog.Entry
	mustGet(t, srv.URL+"/api/headers", &got)
	if len(got) != len(headercatalog.Default) {
		t.Fatalf("expected %d default entries, got %d", len(headercatalog.Default), len(got))
	}

	yaml := "headers:\n  - name: X-Tenant-ID\n    values: [\"acme\"]\n"
	if err := os.WriteFile(filepath.Join(root, "headers.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	got = nil
	mustGet(t, srv.URL+"/api/headers", &got)
	if len(got) != len(headercatalog.Default)+1 {
		t.Fatalf("expected merged entry, got %d entries", len(got))
	}
	last := got[len(got)-1]
	if last.Name != "X-Tenant-ID" || len(last.Values) != 1 || last.Values[0] != "acme" {
		t.Fatalf("merged entry wrong: %+v", last)
	}
}
