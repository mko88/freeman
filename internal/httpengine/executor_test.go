package httpengine

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"

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
		headers         []domain.Header
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
			// Content-Type is a regular header now, not a body-mode
			// concern — raw's Content-Type is whatever the item's own
			// Headers say, same as any other header.
			name:    "POST with raw JSON body",
			method:  http.MethodPost,
			headers: []domain.Header{{Key: "Content-Type", Value: "application/json", Enabled: true}},
			body: &domain.Body{
				Mode: domain.BodyModeRaw,
				Raw:  `{"name":"{{name}}"}`,
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

			item := domain.Item{Method: tc.method, URL: srv.URL, Headers: tc.headers, Body: tc.body}
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

// writeTempFile writes content to name under a fresh temp directory and
// returns the full path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestExecuteFormDataFileField(t *testing.T) {
	path := writeTempFile(t, "profile.json", `{"greeting":"hi"}`)

	var gotContentType string
	var fileBytes []byte
	var fileHeader *multipart.FileHeader
	var caption string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		caption = r.FormValue("caption")
		f, h, err := r.FormFile("avatar")
		if err == nil {
			defer f.Close()
			fileHeader = h
			fileBytes, _ = io.ReadAll(f)
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
				{Key: "caption", Value: "a profile", Enabled: true, Type: domain.FormFieldTypeText},
				{Key: "avatar", Enabled: true, Type: domain.FormFieldTypeFile, FilePath: "{{path}}"},
			},
		},
	}

	if _, err := Execute(context.Background(), item, map[string]string{"path": path}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data; boundary=") {
		t.Fatalf("unexpected Content-Type: %q", gotContentType)
	}
	if caption != "a profile" {
		t.Fatalf("expected the text field to still work alongside the file field, got caption=%q", caption)
	}
	if fileHeader == nil {
		t.Fatal("server did not receive the avatar file part")
	}
	if fileHeader.Filename != "profile.json" {
		t.Fatalf("expected filename %q, got %q", "profile.json", fileHeader.Filename)
	}
	if string(fileBytes) != `{"greeting":"hi"}` {
		t.Fatalf("unexpected file content: %q", fileBytes)
	}
	// .json is in Go's built-in extension table, so this doesn't depend
	// on the host OS's mime.types.
	if ct := fileHeader.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected the file part's Content-Type to be detected from its extension, got %q", ct)
	}
}

func TestExecuteBinaryBody(t *testing.T) {
	content := "binary payload, not that it matters here"
	path := writeTempFile(t, "payload.json", content)

	var gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	item := domain.Item{
		Method: "PUT",
		URL:    srv.URL,
		Body: &domain.Body{
			Mode:           domain.BodyModeBinary,
			BinaryFilePath: "{{path}}",
		},
	}

	if _, err := Execute(context.Background(), item, map[string]string{"path": path}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody != content {
		t.Fatalf("expected the file's exact bytes as the body, got %q", gotBody)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected Content-Type detected from the .json extension, got %q", gotContentType)
	}
}

// TestExecuteTruncatesLargeResponseBody confirms a response body over
// LargeResponseThreshold is written to disk instead of returned inline,
// and that ReadResponseBodyFile reads back exactly what was sent — the
// full round trip both the desktop app's "show anyway" and the headless
// server's GET /api/execute/body depend on.
func TestExecuteTruncatesLargeResponseBody(t *testing.T) {
	big := strings.Repeat("x", LargeResponseThreshold+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(big))
	}))
	defer srv.Close()

	item := domain.Item{Method: "GET", URL: srv.URL}
	resp, err := Execute(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !resp.Truncated {
		t.Fatalf("expected Truncated for a %d-byte body (threshold %d)", len(big), LargeResponseThreshold)
	}
	if resp.Body != "" {
		t.Fatalf("expected Body to be left empty when Truncated, got %d bytes", len(resp.Body))
	}
	if resp.SizeBytes != len(big) {
		t.Fatalf("expected SizeBytes %d, got %d", len(big), resp.SizeBytes)
	}
	if filepath.Ext(resp.BodyFile) != ".json" {
		t.Fatalf("expected a .json extension guessed from Content-Type, got %q", resp.BodyFile)
	}

	got, err := ReadResponseBodyFile(resp.BodyFile)
	if err != nil {
		t.Fatalf("ReadResponseBodyFile: %v", err)
	}
	if string(got) != big {
		t.Fatalf("expected the file to hold the exact response body, got %d bytes", len(got))
	}

	if _, err := ReadResponseBodyFile(filepath.Join(t.TempDir(), "freeman-response-evil.txt")); err == nil {
		t.Fatal("expected ReadResponseBodyFile to refuse a path outside os.TempDir()")
	}
	if _, err := ReadResponseBodyFile(filepath.Join(os.TempDir(), "not-ours.txt")); err == nil {
		t.Fatal("expected ReadResponseBodyFile to refuse a path not matching the freeman-response- naming convention")
	}
}

func TestExecuteSmallResponseBodyIsNotTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resp, err := Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Truncated || resp.BodyFile != "" {
		t.Fatalf("expected an ordinary small response to be inlined, got Truncated=%v BodyFile=%q", resp.Truncated, resp.BodyFile)
	}
	if resp.Body != `{"ok":true}` {
		t.Fatalf("unexpected body: %q", resp.Body)
	}
}

// TestExecuteDecodesContentEncodings confirms a body that comes back
// still compressed — brotli or deflate, which net/http doesn't decode on
// its own (it only auto-handles gzip) — is decoded before Execute
// returns it, and that Content-Encoding is stripped so the response
// reflects the decoded bytes.
func TestExecuteDecodesContentEncodings(t *testing.T) {
	const payload = `{"message":"hello, decoded world","n":42}`

	deflate := func(b []byte) []byte {
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		w.Write(b)
		w.Close()
		return buf.Bytes()
	}
	rawDeflate := func(b []byte) []byte {
		var buf bytes.Buffer
		w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		w.Write(b)
		w.Close()
		return buf.Bytes()
	}
	brot := func(b []byte) []byte {
		var buf bytes.Buffer
		w := brotli.NewWriter(&buf)
		w.Write(b)
		w.Close()
		return buf.Bytes()
	}
	gz := func(b []byte) []byte {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		w.Write(b)
		w.Close()
		return buf.Bytes()
	}

	cases := []struct {
		enc  string
		body []byte
	}{
		{"br", brot([]byte(payload))},
		{"deflate", deflate([]byte(payload))},
		{"deflate", rawDeflate([]byte(payload))}, // server sent raw, not zlib-wrapped
		{"gzip", gz([]byte(payload))},            // server sent it unrequested
	}

	for _, tc := range cases {
		t.Run(tc.enc, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Encoding", tc.enc)
				w.Write(tc.body)
			}))
			defer srv.Close()

			resp, err := Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL}, nil)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if resp.Body != payload {
				t.Fatalf("expected the decoded body %q, got %q", payload, resp.Body)
			}
			if resp.SizeBytes != len(payload) {
				t.Fatalf("expected SizeBytes to be the decoded length %d, got %d", len(payload), resp.SizeBytes)
			}
			if ce := resp.Headers["Content-Encoding"]; len(ce) != 0 {
				t.Fatalf("expected Content-Encoding stripped after decode, still have %v", ce)
			}
		})
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
