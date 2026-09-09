package core

import (
	"time"

	"freeman/internal/domain"
	"freeman/internal/httpengine"
	"freeman/internal/settings"
)

// Settings returns the open workspace's app-wide defaults.
func (a *App) Settings() (settings.Settings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return settings.Settings{}, err
	}
	return settings.Load(a.ws.Root), nil
}

// SaveSettings persists s and applies it to the engine immediately, so a
// changed timeout takes effect on the next send rather than the next
// launch.
func (a *App) SaveSettings(s settings.Settings) (settings.Settings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return settings.Settings{}, err
	}
	s = settings.Clamp(s)
	if err := settings.Save(a.ws.Root, s); err != nil {
		return settings.Settings{}, err
	}
	applySettings(s)
	return s, nil
}

// applySettings pushes the workspace's answers into internal/httpengine,
// whose package-level values are what Execute actually reads. Called
// when a workspace opens and whenever the settings are saved.
//
// Package-level rather than per-request because that is where the engine
// already keeps them, alongside the cookie jar — one workspace is open
// at a time, so there is nothing to keep separate.
func applySettings(s settings.Settings) {
	httpengine.RequestTimeout = time.Duration(s.RequestTimeoutMs) * time.Millisecond
	httpengine.DefaultMaxRedirects = s.MaxRedirects
	httpengine.LargeResponseThreshold = s.InlineResponseBytes
	httpengine.MaxResponseBytes = s.MaxResponseBytes
	// The rest of the Options tab, for requests that carry no options of
	// their own — which is every request that agrees with these, since
	// the editor saves no block in that case. TimeoutMs and MaxRedirects
	// are left out on purpose; they reach the engine as the two values
	// above. See httpengine.DefaultOptions.
	httpengine.DefaultOptions = domain.Options{
		FollowRedirects:   s.FollowRedirects,
		StoreCookies:      s.StoreCookies,
		SkipTLSVerify:     s.SkipTLSVerify,
		CACertFile:        s.CACertFile,
		UseCustomCA:       s.UseCustomCA,
		ClientCertFile:    s.ClientCertFile,
		ClientCertKeyFile: s.ClientCertKeyFile,
	}
}
