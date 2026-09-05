package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestExecuteBodyRoute drives a response over httpengine.LargeResponseThreshold
// through POST /api/execute, confirming it comes back truncated with a
// bodyFile reference, and that GET /api/execute/body reads the full body
// back — the headless server's counterpart to the desktop app's "show
// anyway". It also confirms the route refuses a path it didn't write.
func TestExecuteBodyRoute(t *testing.T) {
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
		SizeBytes int    `json:"sizeBytes"`
	}
	mustPost(t, srv.URL+"/api/execute", executeRequest{CollectionID: collectionID, ItemID: saved.ID}, &resp)
	if !resp.Truncated || resp.Body != "" || resp.BodyFile == "" {
		t.Fatalf("expected a truncated response with a bodyFile, got %+v", resp)
	}
	if resp.SizeBytes != len(big) {
		t.Fatalf("expected sizeBytes %d, got %d", len(big), resp.SizeBytes)
	}

	bodyResp, err := http.Get(srv.URL + "/api/execute/body?path=" + url.QueryEscape(resp.BodyFile))
	if err != nil {
		t.Fatalf("GET /api/execute/body: %v", err)
	}
	defer bodyResp.Body.Close()
	got, _ := io.ReadAll(bodyResp.Body)
	if bodyResp.StatusCode != http.StatusOK || string(got) != big {
		t.Fatalf("expected the full body back (status %d), got %d bytes", bodyResp.StatusCode, len(got))
	}

	rejected, err := http.Get(srv.URL + "/api/execute/body?path=" + url.QueryEscape(filepath.Join(root, "collection.json")))
	if err != nil {
		t.Fatalf("GET /api/execute/body: %v", err)
	}
	defer rejected.Body.Close()
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected a path outside the response-file convention to be refused, got %d", rejected.StatusCode)
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
