package httpengine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"freeman/internal/domain"
)

func redirects(n int) *int { return &n }

func follow(v bool) *domain.Options { return &domain.Options{FollowRedirects: v, StoreCookies: true} }
func cookiesOn(v bool) *domain.Options {
	return &domain.Options{FollowRedirects: true, StoreCookies: v}
}

// A request that predates Options behaves exactly as it did before them:
// redirects followed, cookies kept.
func TestOptionsDefaultWhenUnset(t *testing.T) {
	got := optionsOf(domain.Item{}, nil)
	if !got.FollowRedirects || !got.StoreCookies {
		t.Fatalf("an unset Options should follow and store, got %+v", got)
	}
}

// Which client certificate to present belongs to the environment you're
// pointed at — staging's is not production's — so the paths take the
// same {{var}} substitution as the URL. Without this the file is opened
// under its literal name and every mutual-TLS request fails.
func TestOptionsSubstituteClientCertPaths(t *testing.T) {
	item := domain.Item{Options: &domain.Options{
		FollowRedirects:   true,
		StoreCookies:      true,
		ClientCertFile:    "{{certDir}}/client.pem",
		ClientCertKeyFile: "{{certDir}}/client.key",
	}}
	got := optionsOf(item, map[string]string{"certDir": "/etc/freeman/certs"})
	if got.ClientCertFile != "/etc/freeman/certs/client.pem" {
		t.Errorf("certificate path not substituted: %q", got.ClientCertFile)
	}
	if got.ClientCertKeyFile != "/etc/freeman/certs/client.key" {
		t.Errorf("key path not substituted: %q", got.ClientCertKeyFile)
	}
}

// Not following is the whole point of the switch: it's the only way to
// see a 3xx at all, and so the only way to assert on its Location.
func TestExecuteCanReturnTheRedirectItself(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			http.Redirect(w, r, "/here", http.StatusMovedPermanently)
			return
		}
		fmt.Fprint(w, "arrived")
	}))
	defer srv.Close()

	item := domain.Item{Method: "GET", URL: srv.URL + "/moved", Options: follow(false)}
	resp, err := Execute(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected the 301 itself, got %d", resp.StatusCode)
	}
	if loc := resp.Headers["Location"]; len(loc) == 0 || loc[0] != "/here" {
		t.Fatalf("the Location header should survive, got %v", resp.Headers["Location"])
	}

	item.Options = follow(true)
	resp, err = Execute(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("Execute following: %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.Body != "arrived" {
		t.Fatalf("following should land on the target, got %d %q", resp.StatusCode, resp.Body)
	}
}

// A redirect loop has to end somewhere, and the message has to say why.
func TestExecuteStopsAtMaxRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	defer srv.Close()

	item := domain.Item{
		Method:  "GET",
		URL:     srv.URL,
		Options: &domain.Options{FollowRedirects: true, MaxRedirects: redirects(3), StoreCookies: true},
	}
	_, err := Execute(context.Background(), item, nil)
	if err == nil {
		t.Fatal("expected an error once the cap was hit")
	}
	if !strings.Contains(err.Error(), "stopped after 3 redirects") {
		t.Fatalf("the error should say what stopped it, got %v", err)
	}
}

// The reason the jar exists: sign in on one request, be signed in on the
// next. Both requests are separate Execute calls, as they are in the app.
func TestExecuteCarriesCookiesBetweenRequests(t *testing.T) {
	ResetCookies()
	defer ResetCookies()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc123", Path: "/"})
			fmt.Fprint(w, "ok")
			return
		}
		if c, err := r.Cookie("session"); err == nil {
			fmt.Fprintf(w, "signed in as %s", c.Value)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "anonymous")
	}))
	defer srv.Close()

	if _, err := Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL + "/login"}, nil); err != nil {
		t.Fatalf("login: %v", err)
	}
	resp, err := Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL + "/me"}, nil)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if resp.Body != "signed in as abc123" {
		t.Fatalf("the session cookie should have been sent back, got %q", resp.Body)
	}

	// Opting out isolates a request from that session — which is what
	// makes a test that must run anonymous expressible.
	resp, err = Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL + "/me", Options: cookiesOn(false)}, nil)
	if err != nil {
		t.Fatalf("me without cookies: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a request that stores no cookies should send none, got %d %q", resp.StatusCode, resp.Body)
	}
}

// ResetCookies is what core.App calls when a workspace opens: cookies
// belong to whoever you were talking to, not to the app.
func TestResetCookiesForgetsTheSession(t *testing.T) {
	ResetCookies()
	defer ResetCookies()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc123", Path: "/"})
			return
		}
		if _, err := r.Cookie("session"); err == nil {
			fmt.Fprint(w, "remembered")
			return
		}
		fmt.Fprint(w, "forgotten")
	}))
	defer srv.Close()

	if _, err := Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL + "/login"}, nil); err != nil {
		t.Fatalf("login: %v", err)
	}
	ResetCookies()
	resp, err := Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL + "/me"}, nil)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if resp.Body != "forgotten" {
		t.Fatalf("the jar should be empty after a reset, got %q", resp.Body)
	}
}

// httptest.NewTLSServer uses a certificate no system root signed, which
// is exactly the shape of a staging box with a self-signed cert: without
// the switch it can't be called at all.
func TestExecuteSkipTLSVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secure")
	}))
	defer srv.Close()

	item := domain.Item{Method: "GET", URL: srv.URL}
	if _, err := Execute(context.Background(), item, nil); err == nil {
		t.Fatal("expected an untrusted certificate to be refused by default")
	}

	item.Options = &domain.Options{FollowRedirects: true, StoreCookies: true, SkipTLSVerify: true}
	resp, err := Execute(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("Execute with SkipTLSVerify: %v", err)
	}
	if resp.Body != "secure" {
		t.Fatalf("got %q", resp.Body)
	}
}

// A bad certificate path has to say so, not fail as some opaque
// transport error later on.
func TestExecuteReportsUnreadableClientCert(t *testing.T) {
	item := domain.Item{
		Method: "GET",
		URL:    "https://example.invalid",
		Options: &domain.Options{
			FollowRedirects:   true,
			StoreCookies:      true,
			ClientCertFile:    filepath.Join(t.TempDir(), "nope.pem"),
			ClientCertKeyFile: filepath.Join(t.TempDir(), "nope.key"),
		},
	}
	_, err := Execute(context.Background(), item, nil)
	if err == nil || !strings.Contains(err.Error(), "client certificate") {
		t.Fatalf("expected a client-certificate error, got %v", err)
	}
}

// The per-request timeout overrides the app-wide default, in both
// directions — this one is shorter than a slow endpoint.
func TestExecutePerRequestTimeout(t *testing.T) {
	original := RequestTimeout
	RequestTimeout = 0 // no app-wide deadline, so only the option can stop it
	defer func() { RequestTimeout = original }()

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	defer srv.Close()
	defer close(done)

	item := domain.Item{
		Method:  "GET",
		URL:     srv.URL,
		Options: &domain.Options{FollowRedirects: true, StoreCookies: true, TimeoutMs: 100},
	}
	start := time.Now()
	if _, err := Execute(context.Background(), item, nil); err == nil {
		t.Fatal("expected the per-request timeout to fire")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("should have given up in ~100ms, took %s", elapsed)
	}
}

// The redirect cap reads literally now: nil is the app default, a
// number is that number, and 0 is no cap at all. The distinction
// matters for data saved before the field existed, where the key is
// absent — that has to keep meaning the default, not "unlimited".
func TestOptionsRedirectCapSemantics(t *testing.T) {
	var hops int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	defer srv.Close()

	send := func(opts *domain.Options) error {
		hops = 0
		_, err := Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL, Options: opts}, nil)
		return err
	}

	// Absent options: the app default, whatever it currently is.
	original := DefaultMaxRedirects
	DefaultMaxRedirects = 4
	defer func() { DefaultMaxRedirects = original }()
	if err := send(nil); err == nil {
		t.Fatal("an endless chain should stop at the default cap")
	}
	if hops != 4 {
		t.Errorf("nil options should follow DefaultMaxRedirects (4) hops, got %d", hops)
	}

	// An explicit cap.
	if err := send(&domain.Options{FollowRedirects: true, MaxRedirects: redirects(2)}); err == nil {
		t.Fatal("an endless chain should stop at the request's cap")
	}
	if hops != 2 {
		t.Errorf("an explicit cap of 2 should follow 2 hops, got %d", hops)
	}

	// Zero means no cap: only the timeout ends it, so this uses a short
	// one rather than following forever.
	originalTimeout := RequestTimeout
	RequestTimeout = 300 * time.Millisecond
	defer func() { RequestTimeout = originalTimeout }()
	err := send(&domain.Options{FollowRedirects: true, MaxRedirects: redirects(0)})
	if err == nil {
		t.Fatal("expected the timeout to end an uncapped chain")
	}
	if hops <= 4 {
		t.Errorf("0 should mean no cap, but it stopped after %d hops", hops)
	}
}
