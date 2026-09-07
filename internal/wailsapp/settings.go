package wailsapp

import "freeman/internal/settings"

// GetSettings and SaveSettings surface the workspace's app-wide defaults
// (see internal/settings) to the settings window. SaveSettings returns
// the stored value rather than nothing, because internal/settings.Clamp
// may have adjusted what was sent — the form shows what was actually
// kept.
func (a *App) GetSettings() (settings.Settings, error) {
	return a.App.Settings()
}

func (a *App) SaveSettings(s settings.Settings) (settings.Settings, error) {
	return a.App.SaveSettings(s)
}
