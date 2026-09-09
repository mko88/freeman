package httpengine

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
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

// writeServerCA saves the certificate an httptest TLS server presents,
// as the PEM file a user would point the CA option at. For a self-signed
// server that certificate is its own issuer, which is exactly the shape
// of an internal CA.
func writeServerCA(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	block := &pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("writing the CA file: %v", err)
	}
	return path
}

// The point of the option: a certificate nothing on the machine trusts
// verifies against the CA you name, with the check still happening —
// the difference between this and SkipTLSVerify.
func TestExecuteVerifiesAgainstACustomCA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "verified")
	}))
	defer srv.Close()

	item := domain.Item{Method: "GET", URL: srv.URL}
	if _, err := Execute(context.Background(), item, nil); err == nil {
		t.Fatal("expected an untrusted certificate to be refused by default")
	}

	item.Options = &domain.Options{
		FollowRedirects: true,
		StoreCookies:    true,
		CACertFile:      writeServerCA(t, srv),
		UseCustomCA:     true,
	}
	resp, err := Execute(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("Execute with a custom CA: %v", err)
	}
	if resp.Body != "verified" {
		t.Fatalf("got %q", resp.Body)
	}
}

// writeUnrelatedCA generates a throwaway self-signed CA and writes it
// out — a certificate that issued nothing in the test. Generated rather
// than reusing another httptest server's, because every
// httptest.NewTLSServer presents the same built-in certificate, so two
// of them are not two CAs.
func writeUnrelatedCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unrelated-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating the certificate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "unrelated-ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing the CA file: %v", err)
	}
	return path
}

// Switched on, the custom CA is the whole trust store: a certificate
// that doesn't chain to the named file is refused, whatever the machine
// thinks of it. Without that, "custom CA" would only ever widen what's
// accepted, and pinning to one CA would be impossible.
func TestCustomCAReplacesTheSystemTrustStore(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	item := domain.Item{
		Method: "GET",
		URL:    srv.URL,
		Options: &domain.Options{
			FollowRedirects: true,
			StoreCookies:    true,
			CACertFile:      writeUnrelatedCA(t),
			UseCustomCA:     true,
		},
	}
	if _, err := Execute(context.Background(), item, nil); err == nil {
		t.Fatal("a certificate that doesn't chain to the named CA must be refused")
	}
}

// The switch is what applies the file, so the path can be kept while the
// option is off — the same request then behaves as it did before.
func TestCustomCAIsOffUntilSwitchedOn(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	item := domain.Item{
		Method: "GET",
		URL:    srv.URL,
		Options: &domain.Options{
			FollowRedirects: true,
			StoreCookies:    true,
			CACertFile:      writeServerCA(t, srv),
			UseCustomCA:     false,
		},
	}
	if _, err := Execute(context.Background(), item, nil); err == nil {
		t.Fatal("the CA file should do nothing until the switch is on")
	}
}

// A path that isn't there, or isn't a certificate, has to say so rather
// than fail as an opaque handshake error later.
func TestCustomCAReportsABadFile(t *testing.T) {
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notPEM, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	for name, file := range map[string]string{
		"missing":   filepath.Join(dir, "nope.pem"),
		"not a PEM": notPEM,
	} {
		item := domain.Item{
			Method: "GET",
			URL:    "https://example.invalid",
			Options: &domain.Options{
				FollowRedirects: true,
				StoreCookies:    true,
				CACertFile:      file,
				UseCustomCA:     true,
			},
		}
		_, err := Execute(context.Background(), item, nil)
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if !strings.Contains(err.Error(), "CA certificate") {
			t.Fatalf("%s: the error should name the CA certificate, got %v", name, err)
		}
	}
}

// The workspace's defaults have to reach a request that carries no
// options of its own — which is every request that agrees with them,
// because the editor saves no options block in that case.
//
// The bug this pins: with "Skip TLS certificate check" set as the
// workspace default, a request agreeing with it was sent with a fixed
// set of options instead and verified anyway. There was no way to spell
// the setting that worked, since disagreeing wrote a block saying false.
func TestDefaultOptionsReachARequestThatHasNone(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "reached")
	}))
	defer srv.Close()

	item := domain.Item{Method: "GET", URL: srv.URL}
	if _, err := Execute(context.Background(), item, nil); err == nil {
		t.Fatal("expected an untrusted certificate to be refused before the default is changed")
	}

	restore := DefaultOptions
	t.Cleanup(func() { DefaultOptions = restore })
	DefaultOptions = domain.Options{FollowRedirects: true, StoreCookies: true, SkipTLSVerify: true}

	resp, err := Execute(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("a request with no options should be sent with the workspace's: %v", err)
	}
	if resp.Body != "reached" {
		t.Fatalf("got %q", resp.Body)
	}
}

// The other half: a request that states an option still wins over the
// default, including when it states the quieter of the two.
func TestARequestsOwnOptionsBeatTheDefaults(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	restore := DefaultOptions
	t.Cleanup(func() { DefaultOptions = restore })
	DefaultOptions = domain.Options{FollowRedirects: true, StoreCookies: true, SkipTLSVerify: true}

	item := domain.Item{
		Method:  "GET",
		URL:     srv.URL,
		Options: &domain.Options{FollowRedirects: true, StoreCookies: true, SkipTLSVerify: false},
	}
	if _, err := Execute(context.Background(), item, nil); err == nil {
		t.Fatal("a request that says not to skip the check must still verify")
	}
}

// A certificate path set as the workspace default takes {{var}}
// substitution too — which is the point of setting one there, since it
// then means whatever the open environment calls it.
func TestDefaultOptionsSubstituteVariables(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "verified")
	}))
	defer srv.Close()

	restore := DefaultOptions
	t.Cleanup(func() { DefaultOptions = restore })
	DefaultOptions = domain.Options{
		FollowRedirects: true,
		StoreCookies:    true,
		CACertFile:      "{{caPath}}",
		UseCustomCA:     true,
	}

	vars := map[string]string{"caPath": writeServerCA(t, srv)}
	resp, err := Execute(context.Background(), domain.Item{Method: "GET", URL: srv.URL}, vars)
	if err != nil {
		t.Fatalf("the default CA path should be substituted like any other: %v", err)
	}
	if resp.Body != "verified" {
		t.Fatalf("got %q", resp.Body)
	}
}
