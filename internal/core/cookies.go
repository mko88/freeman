package core

import "freeman/internal/httpengine"

// Cookies lists the shared jar. It needs no open workspace: the jar
// belongs to the running app, not to the folder it has open — though
// OpenWorkspace clears it, since cookies belong to whoever you were
// talking to.
func (a *App) Cookies() []httpengine.Cookie {
	return httpengine.CookieJar().All()
}

// DeleteCookie removes one, identified the way a browser identifies
// one: the same name at a different domain or path is a different
// cookie.
func (a *App) DeleteCookie(domain, path, name string) {
	httpengine.CookieJar().Delete(domain, path, name)
}

// ClearCookies empties the jar — the "sign out of everything" button.
func (a *App) ClearCookies() {
	httpengine.ResetCookies()
}
