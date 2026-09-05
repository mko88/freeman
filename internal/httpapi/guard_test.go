package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"freeman/internal/core"
)

// do sends one request through a guarded handler and returns its status.
func do(t *testing.T, srv *httptest.Server, method, path, contentType string, body []byte, headers map[string]string) int {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body == nil {
		// A nil body must not look like a zero-length one with a
		// Content-Length header, or the guard's "has a body" test changes
		// meaning between cases.
		req.Body = nil
		req.ContentLength = 0
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestGuardRefusesBrowserCrossOriginRequests is the regression test for
// the CSRF hole this guard closes: loopback keeps other machines out,
// but any page the user has open could reach the control API with a CORS
// "simple request" — no preflight, reply unreadable but the write still
// applied. See internal/httpapi/guard.go.
func TestGuardRefusesBrowserCrossOriginRequests(t *testing.T) {
	app := core.NewApp()
	if _, err := app.OpenWorkspace(t.TempDir()); err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	srv := httptest.NewServer(NewHandler(app, nil))
	defer srv.Close()

	env := []byte(`{"formatVersion":"1","name":"Guard Test"}`)

	cases := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        []byte
		headers     map[string]string
		want        int
	}{
		{
			name:   "a script with no browser headers is untouched",
			method: "POST", path: "/api/environments",
			contentType: "application/json", body: env,
			want: http.StatusOK,
		},
		{
			name:   "charset parameters on the content type are fine",
			method: "POST", path: "/api/environments",
			contentType: "application/json; charset=utf-8", body: env,
			want: http.StatusOK,
		},
		{
			name:   "the text/plain simple-request shape is refused",
			method: "POST", path: "/api/environments",
			contentType: "text/plain", body: env,
			want: http.StatusUnsupportedMediaType,
		},
		{
			name:   "a form content type is refused too",
			method: "POST", path: "/api/environments",
			contentType: "application/x-www-form-urlencoded", body: env,
			want: http.StatusUnsupportedMediaType,
		},
		{
			name:   "a body with no content type at all is refused",
			method: "POST", path: "/api/environments",
			body: env,
			want: http.StatusUnsupportedMediaType,
		},
		{
			name:   "a foreign Origin is refused even with the right content type",
			method: "POST", path: "/api/environments",
			contentType: "application/json", body: env,
			headers: map[string]string{"Origin": "https://evil.example"},
			want:    http.StatusForbidden,
		},
		{
			name:   "a foreign Origin is refused on reads as well",
			method: "GET", path: "/api/environments",
			headers: map[string]string{"Origin": "https://evil.example"},
			want:    http.StatusForbidden,
		},
		{
			name:   "Sec-Fetch-Site: cross-site is refused",
			method: "GET", path: "/api/environments",
			headers: map[string]string{"Sec-Fetch-Site": "cross-site"},
			want:    http.StatusForbidden,
		},
		{
			name:   "Sec-Fetch-Site: same-site is refused — same site is not same origin",
			method: "GET", path: "/api/environments",
			headers: map[string]string{"Sec-Fetch-Site": "same-site"},
			want:    http.StatusForbidden,
		},
		{
			name:   "the server's own page (same-origin) is allowed",
			method: "GET", path: "/api/environments",
			headers: map[string]string{"Sec-Fetch-Site": "same-origin"},
			want:    http.StatusOK,
		},
		{
			name:   "a bodyless DELETE needs no content type",
			method: "DELETE", path: "/api/environments/nope",
			// Not found, but it got past the guard — which is the point.
			want: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := do(t, srv, tc.method, tc.path, tc.contentType, tc.body, tc.headers)
			if got != tc.want {
				t.Fatalf("status %d, want %d", got, tc.want)
			}
		})
	}
}

// TestGuardAllowsSameOriginBrowserRequests covers the freeman-server
// case, where the browser *is* the legitimate client: its own page sends
// an Origin matching the server's host, which must pass.
func TestGuardAllowsSameOriginBrowserRequests(t *testing.T) {
	app := core.NewApp()
	if _, err := app.OpenWorkspace(t.TempDir()); err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	srv := httptest.NewServer(NewHandler(app, nil))
	defer srv.Close()

	// srv.URL is "http://127.0.0.1:<port>" — exactly what a page served
	// from this server would send as its Origin.
	got := do(t, srv, "GET", "/api/environments", "", nil, map[string]string{
		"Origin":         srv.URL,
		"Sec-Fetch-Site": "same-origin",
	})
	if got != http.StatusOK {
		t.Fatalf("same-origin request got %d, want 200", got)
	}
}
