package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"freeman/internal/domain"
)

// TestExecuteRequestCommonMethods exercises GET/POST/PUT/PATCH/DELETE
// against httpbin.org's method-specific echo endpoints, each with a
// custom header and (where applicable) a JSON body, to prove request
// building/execution works uniformly across every method the frontend's
// method dropdown offers — not just GET, which is all TestVerticalSlice
// covers.
func TestExecuteRequestCommonMethods(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	app := NewApp()

	ws, err := app.OpenWorkspace(root)
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	collectionID := ws.Collections[0].ID

	cases := []struct {
		method string
		body   string
	}{
		{"GET", ""},
		{"POST", `{"hello":"world"}`},
		{"PUT", `{"hello":"world"}`},
		{"PATCH", `{"hello":"world"}`},
		{"DELETE", ""},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			item := domain.Item{
				Name:    tc.method,
				Method:  tc.method,
				URL:     "https://httpbin.org/" + strings.ToLower(tc.method),
				Headers: []domain.Header{{Key: "X-Test-Method", Value: tc.method, Enabled: true}},
			}
			if tc.body != "" {
				item.Body = &domain.Body{Mode: domain.BodyModeRaw, Raw: tc.body, RawContentType: "application/json"}
			}

			saved, err := app.SaveRequest(collectionID, item)
			if err != nil {
				t.Fatalf("SaveRequest: %v", err)
			}

			resp, err := app.ExecuteRequest(ctx, collectionID, saved.ID, "")
			if err != nil {
				t.Fatalf("ExecuteRequest: %v", err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
			}

			var echoed struct {
				Headers map[string]string `json:"headers"`
				JSON    map[string]any    `json:"json"`
			}
			if err := json.Unmarshal([]byte(resp.Body), &echoed); err != nil {
				t.Fatalf("decode httpbin response: %v", err)
			}
			if echoed.Headers["X-Test-Method"] != tc.method {
				t.Fatalf("expected X-Test-Method header echoed back, got %q", echoed.Headers["X-Test-Method"])
			}
			if tc.body != "" && echoed.JSON["hello"] != "world" {
				t.Fatalf("expected request body echoed back, got %+v", echoed.JSON)
			}

			t.Logf("%s https://httpbin.org/%s -> %s, %d bytes, %s",
				tc.method, strings.ToLower(tc.method), resp.Status, resp.SizeBytes, resp.Duration)
		})
	}
}
