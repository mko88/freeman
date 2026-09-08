package httpengine

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cookie is one entry in the shared jar, as the app shows it. Separate
// from http.Cookie because that type is a wire format — it carries
// Raw/Unparsed fields and a SameSite enum that mean nothing here — and
// because this one crosses the JSON bridge to the settings window.
type Cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
	// Zero for a session cookie: one that lasts until the jar is
	// cleared, which for this app means until a workspace is opened.
	Expires  time.Time `json:"expires"`
	Secure   bool      `json:"secure"`
	HTTPOnly bool      `json:"httpOnly"`
}

// Jar is the shared cookie jar, wrapping net/http/cookiejar.
//
// The wrapping exists because cookiejar.Jar can decide which cookies go
// on a request but cannot be asked what it holds, and it has no way to
// remove one. This keeps a record of every cookie it is handed so the
// settings window can list them, and rebuilds the underlying jar from
// that record when one is deleted.
//
// The record is what the app was *told*, not what cookiejar necessarily
// kept: it applies rules of its own (a cookie for a domain the URL has
// no business setting, say) and reports nothing when it declines. A
// rejected cookie therefore lists here until something rebuilds the jar,
// after which both agree, since the rebuild replays through the same
// jar. Over-listing an ignored cookie seemed a better trade than not
// being able to see the jar at all.
type Jar struct {
	mu      sync.Mutex
	inner   http.CookieJar
	entries map[string]jarEntry
}

// The URL is kept so a rebuild can replay the cookie exactly as it
// arrived — cookiejar derives the domain and path from it when the
// cookie doesn't say.
type jarEntry struct {
	url    *url.URL
	cookie *http.Cookie
}

func NewJar() *Jar {
	return &Jar{inner: newInner(), entries: map[string]jarEntry{}}
}

func newInner() http.CookieJar {
	// cookiejar.New only ever errors on a bad PublicSuffixList, and this
	// passes none.
	jar, _ := cookiejar.New(nil)
	return jar
}

func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	for _, c := range cookies {
		key := jarKey(domainOf(c, u), pathOf(c, u), c.Name)
		// A server deletes a cookie by re-sending it already expired.
		if c.MaxAge < 0 || (!c.Expires.IsZero() && !c.Expires.After(time.Now())) {
			delete(j.entries, key)
			continue
		}
		j.entries[key] = jarEntry{url: u, cookie: c}
	}
	j.mu.Unlock()
	j.inner.SetCookies(u, cookies)
}

func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	return j.inner.Cookies(u)
}

// All lists the jar, ordered so the same jar always reads the same way:
// by domain, then path, then name.
func (j *Jar) All() []Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	out := make([]Cookie, 0, len(j.entries))
	for key, e := range j.entries {
		expires := expiryOf(e.cookie)
		if !expires.IsZero() && !expires.After(now) {
			delete(j.entries, key)
			continue
		}
		out = append(out, Cookie{
			Name:     e.cookie.Name,
			Value:    e.cookie.Value,
			Domain:   domainOf(e.cookie, e.url),
			Path:     pathOf(e.cookie, e.url),
			Expires:  expires,
			Secure:   e.cookie.Secure,
			HTTPOnly: e.cookie.HttpOnly,
		})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Domain != out[b].Domain {
			return out[a].Domain < out[b].Domain
		}
		if out[a].Path != out[b].Path {
			return out[a].Path < out[b].Path
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// Delete removes one cookie and rebuilds the jar from what's left,
// because cookiejar.Jar has no way to forget one.
func (j *Jar) Delete(domain, path, name string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.entries, jarKey(domain, path, name))
	j.rebuildLocked()
}

func (j *Jar) Clear() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = map[string]jarEntry{}
	j.inner = newInner()
}

func (j *Jar) rebuildLocked() {
	inner := newInner()
	for _, e := range j.entries {
		inner.SetCookies(e.url, []*http.Cookie{e.cookie})
	}
	j.inner = inner
}

// jarKey identifies a cookie the way a browser does: the same name at a
// different domain or path is a different cookie.
func jarKey(domain, path, name string) string {
	return domain + "\n" + path + "\n" + name
}

func domainOf(c *http.Cookie, u *url.URL) string {
	if c.Domain != "" {
		return strings.TrimPrefix(c.Domain, ".")
	}
	return u.Hostname()
}

func pathOf(c *http.Cookie, u *url.URL) string {
	if c.Path != "" {
		return c.Path
	}
	if u.Path == "" {
		return "/"
	}
	return u.Path
}

// expiryOf resolves the two ways a server can say when a cookie ends.
// Max-Age wins over Expires, per RFC 6265.
func expiryOf(c *http.Cookie) time.Time {
	if c.MaxAge > 0 {
		return time.Now().Add(time.Duration(c.MaxAge) * time.Second)
	}
	return c.Expires
}
