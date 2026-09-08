package wailsapp

import "freeman/internal/httpengine"

// The shared cookie jar, for the settings window's Cookies tab. Thin
// pass-throughs: core owns the behaviour so the desktop app and the HTTP
// API can't drift.
func (a *App) GetCookies() []httpengine.Cookie {
	return a.App.Cookies()
}

func (a *App) DeleteCookie(domain, path, name string) []httpengine.Cookie {
	a.App.DeleteCookie(domain, path, name)
	return a.App.Cookies()
}

func (a *App) ClearCookies() []httpengine.Cookie {
	a.App.ClearCookies()
	return a.App.Cookies()
}
