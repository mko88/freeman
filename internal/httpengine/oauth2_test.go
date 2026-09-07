package httpengine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"freeman/internal/domain"
)

// lastForm remembers the form the token endpoint was last posted, so a
// test can assert the grant was sent the way RFC 6749 describes.
type lastForm struct {
	mu     sync.Mutex
	values url.Values
}

func (f *lastForm) set(v url.Values) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values = v
}

func (f *lastForm) Get(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.values.Get(key)
}

func (f *lastForm) All() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.values
}

// tokenServer answers the client-credentials grant, counting how often
// it was asked and recording what it was sent.
func tokenServer(t *testing.T, expiresIn int) (*httptest.Server, *atomic.Int32, *lastForm) {
	t.Helper()
	var calls atomic.Int32
	form := &lastForm{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		form.set(r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"Bearer","expires_in":%d}`, calls.Load(), expiresIn)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, form
}

func oauthItem(tokenURL, target string) domain.Item {
	return domain.Item{
		Method: "GET",
		URL:    target,
		Auth: &domain.Auth{
			Type:         domain.AuthTypeOAuth2,
			TokenURL:     tokenURL,
			ClientID:     "id",
			ClientSecret: "secret",
			Scope:        "read",
		},
	}
}

func TestOAuth2FetchesAndSendsBearerToken(t *testing.T) {
	ResetTokens()
	defer ResetTokens()

	tok, calls, form := tokenServer(t, 3600)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Header.Get("Authorization"))
	}))
	defer api.Close()

	resp, err := Execute(context.Background(), oauthItem(tok.URL, api.URL), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Body != "Bearer tok-1" {
		t.Fatalf("the fetched token should be sent as a bearer, got %q", resp.Body)
	}
	if got := form.Get("grant_type"); got != "client_credentials" {
		t.Fatalf("grant_type should be client_credentials, got %q", got)
	}
	if form.Get("client_id") != "id" || form.Get("client_secret") != "secret" || form.Get("scope") != "read" {
		t.Fatalf("credentials should go in the form body, got %v", form.All())
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one token call, got %d", calls.Load())
	}
}

// The point of the cache: a run of requests should cost one token call,
// not one each.
func TestOAuth2ReusesATokenUntilItExpires(t *testing.T) {
	ResetTokens()
	defer ResetTokens()

	tok, calls, _ := tokenServer(t, 3600)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Header.Get("Authorization"))
	}))
	defer api.Close()

	for i := 0; i < 3; i++ {
		resp, err := Execute(context.Background(), oauthItem(tok.URL, api.URL), nil)
		if err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
		if resp.Body != "Bearer tok-1" {
			t.Fatalf("every request should reuse the first token, got %q on call %d", resp.Body, i)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected the token to be fetched once, got %d calls", calls.Load())
	}

	// A different scope is a different token, so it must not hit the
	// same cache entry.
	item := oauthItem(tok.URL, api.URL)
	item.Auth.Scope = "write"
	if _, err := Execute(context.Background(), item, nil); err != nil {
		t.Fatalf("Execute with another scope: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("a different scope should fetch its own token, got %d calls", calls.Load())
	}
}

// An expired token has to be refetched rather than resent. expires_in 0
// falls under the leeway, so it is never considered usable.
func TestOAuth2RefetchesAnExpiredToken(t *testing.T) {
	ResetTokens()
	defer ResetTokens()

	tok, calls, _ := tokenServer(t, -1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer api.Close()

	for i := 0; i < 2; i++ {
		if _, err := Execute(context.Background(), oauthItem(tok.URL, api.URL), nil); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("an already-expired token should not be reused, got %d calls", calls.Load())
	}
}

// A request whose credentials couldn't be obtained must not go out
// unauthenticated as though nothing were wrong.
func TestOAuth2FailureStopsTheRequest(t *testing.T) {
	ResetTokens()
	defer ResetTokens()

	tok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid_client"}`)
	}))
	defer tok.Close()

	var reached atomic.Bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
	}))
	defer api.Close()

	_, err := Execute(context.Background(), oauthItem(tok.URL, api.URL), nil)
	if err == nil {
		t.Fatal("expected the request to fail when the token could not be fetched")
	}
	if !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("the server's own message is the useful part, got %v", err)
	}
	if reached.Load() {
		t.Fatal("the request must not be sent without the credentials it asked for")
	}
}
