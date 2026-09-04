package httpengine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"freeman/internal/domain"
)

// TestExecuteAllMethodsAndBodyTypes drives Execute across every HTTP
// method the UI offers (App.svelte's `methods` list: GET, POST, PUT,
// PATCH, DELETE, HEAD, OPTIONS) and every domain.BodyMode (none — both as
// a nil Body and as an explicit BodyModeNone —, raw, form-data, and
// x-www-form-urlencoded), confirming the method, the Content-Type, and
// the (variable-substituted) body bytes all actually reach the server as
// configured. The dedicated tests above cover urlencoded/form-data in
// more depth (disabled fields, Content-Type precedence); this one is
// about breadth across the method × body-mode space.
func TestExecuteAllMethodsAndBodyTypes(t *testing.T) {
	tests := []struct {
		name            string
		method          string
		body            *domain.Body
		wantEmpty       bool   // true: the server must see zero body bytes
		wantBodyHas     string // substring the (substituted) body must contain
		wantContentType string // "" = don't check
	}{
		{
			name:      "GET with no body (nil Body)",
			method:    http.MethodGet,
			body:      nil,
			wantEmpty: true,
		},
		{
			name:      "HEAD with explicit none mode",
			method:    http.MethodHead,
			body:      &domain.Body{Mode: domain.BodyModeNone},
			wantEmpty: true,
		},
		{
			name:      "OPTIONS with no body",
			method:    http.MethodOptions,
			body:      nil,
			wantEmpty: true,
		},
		{
			name:   "POST with raw JSON body",
			method: http.MethodPost,
			body: &domain.Body{
				Mode:           domain.BodyModeRaw,
				Raw:            `{"name":"{{name}}"}`,
				RawContentType: "application/json",
			},
			wantBodyHas:     `"name":"Bob"`,
			wantContentType: "application/json",
		},
		{
			name:   "PUT with form-data body",
			method: http.MethodPut,
			body: &domain.Body{
				Mode:       domain.BodyModeForm,
				FormFields: []domain.FormField{{Key: "name", Value: "{{name}}", Enabled: true}},
			},
			wantBodyHas: "Bob",
		},
		{
			name:   "PATCH with x-www-form-urlencoded body",
			method: http.MethodPatch,
			body: &domain.Body{
				Mode:       domain.BodyModeURLEncoded,
				FormFields: []domain.FormField{{Key: "name", Value: "{{name}}", Enabled: true}},
			},
			wantBodyHas:     "name=Bob",
			wantContentType: "application/x-www-form-urlencoded",
		},
		{
			name:      "DELETE with no body",
			method:    http.MethodDelete,
			body:      nil,
			wantEmpty: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotContentType, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotContentType = r.Header.Get("Content-Type")
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			item := domain.Item{Method: tc.method, URL: srv.URL, Body: tc.body}
			resp, err := Execute(context.Background(), item, map[string]string{"name": "Bob"})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			if gotMethod != tc.method {
				t.Fatalf("expected method %q, server saw %q", tc.method, gotMethod)
			}
			if tc.wantEmpty && gotBody != "" {
				t.Fatalf("expected an empty body, got %q", gotBody)
			}
			if tc.wantBodyHas != "" && !strings.Contains(gotBody, tc.wantBodyHas) {
				t.Fatalf("expected body to contain %q, got %q", tc.wantBodyHas, gotBody)
			}
			if tc.wantContentType != "" && gotContentType != tc.wantContentType {
				t.Fatalf("expected Content-Type %q, got %q", tc.wantContentType, gotContentType)
			}
		})
	}
}

func TestExecuteURLEncodedBody(t *testing.T) {
	var gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	item := domain.Item{
		Method: "POST",
		URL:    srv.URL,
		Body: &domain.Body{
			Mode: domain.BodyModeURLEncoded,
			FormFields: []domain.FormField{
				{Key: "username", Value: "{{user}}", Enabled: true},
				{Key: "password", Value: "secret", Enabled: true},
				{Key: "skip", Value: "me", Enabled: false},
			},
		},
	}

	resp, err := Execute(context.Background(), item, map[string]string{"user": "alice"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("unexpected Content-Type: %q", gotContentType)
	}
	if !strings.Contains(gotBody, "username=alice") || !strings.Contains(gotBody, "password=secret") {
		t.Fatalf("unexpected body (expected substituted, enabled fields): %q", gotBody)
	}
	if strings.Contains(gotBody, "skip") {
		t.Fatalf("disabled field leaked into body: %q", gotBody)
	}
}

func TestExecuteFormDataBody(t *testing.T) {
	var gotContentType, gotName, gotEmail string
	var parseErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if parseErr = r.ParseMultipartForm(1 << 20); parseErr == nil {
			gotName = r.FormValue("name")
			gotEmail = r.FormValue("email")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	item := domain.Item{
		Method: "POST",
		URL:    srv.URL,
		Body: &domain.Body{
			Mode: domain.BodyModeForm,
			FormFields: []domain.FormField{
				{Key: "name", Value: "{{name}}", Enabled: true},
				{Key: "email", Value: "a@b.com", Enabled: true},
				{Key: "skip", Value: "me", Enabled: false},
			},
		},
	}

	if _, err := Execute(context.Background(), item, map[string]string{"name": "Bob"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if parseErr != nil {
		t.Fatalf("server failed to parse the multipart body: %v", parseErr)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data; boundary=") {
		t.Fatalf("unexpected Content-Type: %q", gotContentType)
	}
	if gotName != "Bob" || gotEmail != "a@b.com" {
		t.Fatalf("unexpected fields: name=%q email=%q", gotName, gotEmail)
	}
}

// A hand-typed Content-Type header can never carry the right multipart
// boundary, so form-data must always override it rather than respecting
// it the way raw/urlencoded bodies fall back only when unset.
func TestExecuteFormDataOverridesExplicitContentTypeHeader(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	item := domain.Item{
		Method:  "POST",
		URL:     srv.URL,
		Headers: []domain.Header{{Key: "Content-Type", Value: "text/plain", Enabled: true}},
		Body: &domain.Body{
			Mode:       domain.BodyModeForm,
			FormFields: []domain.FormField{{Key: "a", Value: "b", Enabled: true}},
		},
	}

	if _, err := Execute(context.Background(), item, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data; boundary=") {
		t.Fatalf("expected the multipart Content-Type to win over the explicit header, got %q", gotContentType)
	}
}

func TestExecuteURLEncodedRespectsExplicitContentTypeHeader(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	item := domain.Item{
		Method:  "POST",
		URL:     srv.URL,
		Headers: []domain.Header{{Key: "Content-Type", Value: "application/x-www-form-urlencoded; charset=utf-8", Enabled: true}},
		Body: &domain.Body{
			Mode:       domain.BodyModeURLEncoded,
			FormFields: []domain.FormField{{Key: "a", Value: "b", Enabled: true}},
		},
	}

	if _, err := Execute(context.Background(), item, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotContentType != "application/x-www-form-urlencoded; charset=utf-8" {
		t.Fatalf("expected the explicit header to win, got %q", gotContentType)
	}
}
