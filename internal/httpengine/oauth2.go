package httpengine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"freeman/internal/domain"
)

// The client-credentials grant, which is the one an API test needs: it
// asks for a token with an ID and a secret and no person at a browser.
// The authorization-code grant is deliberately not here — it needs a
// redirect back to a listener and a human approving a consent screen,
// which is a different feature rather than a bigger version of this one.

// tokenCacheEntry is one fetched token and when it stops being usable.
type tokenCacheEntry struct {
	token   string
	expires time.Time
}

var (
	tokenMu    sync.Mutex
	tokenCache = map[string]tokenCacheEntry{}
)

// tokenLeeway retires a token slightly before the server would, so a
// request isn't sent with one that expires in flight.
const tokenLeeway = 30 * time.Second

// ResetTokens empties the token cache — called when a workspace opens,
// for the same reason the cookie jar is: a token belongs to whoever
// issued it, not to the app.
func ResetTokens() {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	tokenCache = map[string]tokenCacheEntry{}
}

// oauth2Token returns a cached token if one is still good, otherwise
// fetches a new one. Keyed by everything that identifies the token, so
// two requests asking for different scopes don't share one.
func oauth2Token(ctx context.Context, a *domain.Auth, vars map[string]string, opts domain.Options) (string, error) {
	tokenURL := Substitute(a.TokenURL, vars)
	clientID := Substitute(a.ClientID, vars)
	clientSecret := Substitute(a.ClientSecret, vars)
	scope := Substitute(a.Scope, vars)

	if tokenURL == "" {
		return "", fmt.Errorf("oauth2: no token URL")
	}
	key := strings.Join([]string{tokenURL, clientID, scope}, "\n")

	tokenMu.Lock()
	if e, ok := tokenCache[key]; ok && time.Now().Before(e.expires) {
		tokenMu.Unlock()
		return e.token, nil
	}
	tokenMu.Unlock()

	token, lifetime, stated, err := fetchToken(ctx, tokenURL, clientID, clientSecret, scope, opts)
	if err != nil {
		return "", err
	}

	// No expires_in at all is cached briefly rather than not at all:
	// long enough to spare a token call per request in a run, short
	// enough that a short-lived token isn't reused once it has gone
	// stale. A server that states a lifetime is believed — including
	// when it states one already spent, which is not the same thing as
	// saying nothing.
	if !stated {
		lifetime = time.Minute
	}
	if usable := lifetime - tokenLeeway; usable > 0 {
		tokenMu.Lock()
		tokenCache[key] = tokenCacheEntry{token: token, expires: time.Now().Add(usable)}
		tokenMu.Unlock()
	}
	// Either way this token is used now — the server just issued it.
	// Not caching it only means the next request asks again.
	return token, nil
}

// fetchToken performs the grant. Credentials go in the form body rather
// than a Basic header: both are allowed by RFC 6749, and the body is
// what more servers accept in practice.
// The bool reports whether the server stated a lifetime at all, which
// the caller needs to tell "didn't say" from "said it's already spent".
func fetchToken(ctx context.Context, tokenURL, clientID, clientSecret, scope string, opts domain.Options) (string, time.Duration, bool, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	if clientID != "" {
		form.Set("client_id", clientID)
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	if scope != "" {
		form.Set("scope", scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, false, fmt.Errorf("oauth2: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// The token call is its own request: no cookie jar and no redirect
	// policy, because it is not the request under test.
	//
	// The TLS settings are the exception, and have to be carried. A
	// token endpoint sits on the same host as the API often enough that
	// a certificate the request was told to accept is the same one the
	// token call meets — and a client certificate is frequently how the
	// token endpoint identifies you in the first place. Left off, "Skip
	// TLS certificate check" looked broken: the request never got as far
	// as its own transport, failing at the token step with an x509 error
	// that reads exactly like the option being ignored.
	client := &http.Client{}
	if opts.TLS() {
		tr, err := transportFor(opts)
		if err != nil {
			return "", 0, false, fmt.Errorf("oauth2: %w", err)
		}
		client.Transport = tr
		defer tr.CloseIdleConnections()
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, false, fmt.Errorf("oauth2: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, false, fmt.Errorf("oauth2: reading token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// The server's own message is the useful part — an OAuth2 error
		// body names what was wrong with the grant.
		return "", 0, false, fmt.Errorf("oauth2: token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   *int   `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", 0, false, fmt.Errorf("oauth2: token response was not JSON: %w", err)
	}
	if out.AccessToken == "" {
		return "", 0, false, fmt.Errorf("oauth2: token response carried no access_token")
	}
	if out.ExpiresIn == nil {
		return out.AccessToken, 0, false, nil
	}
	return out.AccessToken, time.Duration(*out.ExpiresIn) * time.Second, true, nil
}
