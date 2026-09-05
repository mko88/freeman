package core

import (
	"freeman/internal/codegen"
	"freeman/internal/domain"
	"freeman/internal/store"
)

// GenerateRequestCode renders item as a runnable command (see
// internal/codegen for the formats), resolving {{var}} against
// environmentID the same way ExecuteRequest does. item is passed
// directly rather than loaded by ID so the editor can preview unsaved
// edits.
func (a *App) GenerateRequestCode(item domain.Item, environmentID, format string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return "", err
	}
	vars := map[string]string{}
	if environmentID != "" {
		env, err := store.LoadEnvironment(a.ws.EnvironmentPaths[environmentID])
		if err != nil {
			return "", err
		}
		vars = env.ResolvedVariables()
	}
	return codegen.Generate(item, vars, codegen.Format(format))
}
