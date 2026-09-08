package httpengine

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url %q: %v", raw, err)
	}
	return u
}

func names(cookies []Cookie) []string {
	out := make([]string, len(cookies))
	for i, c := range cookies {
		out[i] = c.Name
	}
	return out
}

// The whole point of wrapping cookiejar: being able to ask what's in it.
func TestJarListsWhatItWasGiven(t *testing.T) {
	j := NewJar()
	u := mustURL(t, "https://api.example.com/v1/orders")
	j.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "abc", Secure: true, HttpOnly: true},
		{Name: "prefs", Value: "dark", Path: "/"},
	})

	all := j.All()
	if got := names(all); len(got) != 2 {
		t.Fatalf("expected two cookies, got %v", got)
	}
	// Sorted by domain, then path, then name — so the same jar always
	// reads the same way.
	if all[0].Name != "prefs" || all[1].Name != "session" {
		t.Errorf("not ordered by path then name: %v", names(all))
	}
	if all[1].Domain != "api.example.com" {
		t.Errorf("domain should come from the URL when the cookie omits it: %q", all[1].Domain)
	}
	if all[1].Path != "/v1/orders" {
		t.Errorf("path should come from the URL when the cookie omits it: %q", all[1].Path)
	}
	if !all[1].Secure || !all[1].HTTPOnly {
		t.Errorf("flags lost: %+v", all[1])
	}
}

// Deleting has to reach the underlying jar, not just the listing —
// cookiejar has no removal, so this is the case that would rot.
func TestJarDeleteStopsTheCookieBeingSent(t *testing.T) {
	j := NewJar()
	u := mustURL(t, "https://api.example.com/")
	j.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "abc", Path: "/"},
		{Name: "prefs", Value: "dark", Path: "/"},
	})
	if got := len(j.Cookies(u)); got != 2 {
		t.Fatalf("expected both cookies to be sent, got %d", got)
	}

	j.Delete("api.example.com", "/", "session")

	if got := names(j.All()); len(got) != 1 || got[0] != "prefs" {
		t.Errorf("listing still shows it: %v", got)
	}
	sent := j.Cookies(u)
	if len(sent) != 1 || sent[0].Name != "prefs" {
		t.Errorf("the jar still sends it: %+v", sent)
	}
}

// A server deletes a cookie by re-sending it already expired, and the
// listing has to follow.
func TestJarForgetsExpiredCookies(t *testing.T) {
	j := NewJar()
	u := mustURL(t, "https://api.example.com/")
	j.SetCookies(u, []*http.Cookie{{Name: "session", Value: "abc", Path: "/"}})
	if len(j.All()) != 1 {
		t.Fatal("setup: expected one cookie")
	}

	j.SetCookies(u, []*http.Cookie{{Name: "session", Value: "", Path: "/", MaxAge: -1}})
	if got := j.All(); len(got) != 0 {
		t.Errorf("a Max-Age of -1 should remove it, got %v", names(got))
	}

	// And one that simply runs out on its own.
	j.SetCookies(u, []*http.Cookie{{Name: "brief", Value: "x", Path: "/", Expires: time.Now().Add(-time.Minute)}})
	if got := j.All(); len(got) != 0 {
		t.Errorf("an already-past Expires should not be kept, got %v", names(got))
	}
}

// Same name, different domain or path, is a different cookie — so
// deleting one must leave the other.
func TestJarKeysOnDomainAndPath(t *testing.T) {
	j := NewJar()
	j.SetCookies(mustURL(t, "https://a.example.com/"), []*http.Cookie{{Name: "id", Value: "1", Path: "/"}})
	j.SetCookies(mustURL(t, "https://b.example.com/"), []*http.Cookie{{Name: "id", Value: "2", Path: "/"}})
	if got := j.All(); len(got) != 2 {
		t.Fatalf("expected two separate cookies, got %v", names(got))
	}

	j.Delete("a.example.com", "/", "id")
	got := j.All()
	if len(got) != 1 || got[0].Domain != "b.example.com" {
		t.Fatalf("deleting one domain's cookie took the other: %+v", got)
	}
}

func TestJarClear(t *testing.T) {
	j := NewJar()
	u := mustURL(t, "https://api.example.com/")
	j.SetCookies(u, []*http.Cookie{{Name: "session", Value: "abc", Path: "/"}})
	j.Clear()
	if got := j.All(); len(got) != 0 {
		t.Errorf("listing after Clear: %v", names(got))
	}
	if got := j.Cookies(u); len(got) != 0 {
		t.Errorf("the jar still sends after Clear: %+v", got)
	}
}
