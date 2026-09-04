package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"freeman/internal/domain"
)

// LoadEnvironment reads an environment's tracked file and, if a sibling
// <name>.local.json override exists, merges its variable values in by
// Key (local wins). This is how secret variable values — which are never
// written to the tracked file — make it back into a loaded Environment.
func LoadEnvironment(path string) (*domain.Environment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e domain.Environment
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}

	localData, err := os.ReadFile(localPathFor(path))
	if err != nil {
		if os.IsNotExist(err) {
			return &e, nil
		}
		return nil, err
	}

	var local domain.Environment
	if err := json.Unmarshal(localData, &local); err != nil {
		return nil, err
	}
	localValues := make(map[string]string, len(local.Variables))
	for _, v := range local.Variables {
		localValues[v.Key] = v.Value
	}
	for i, v := range e.Variables {
		if val, ok := localValues[v.Key]; ok {
			e.Variables[i].Value = val
		}
	}

	return &e, nil
}

// SaveEnvironment writes the tracked file with secret variable values
// blanked out, and writes any non-empty secret values to the sibling
// <name>.local.json override so they never land in the tracked file.
func SaveEnvironment(path string, e *domain.Environment) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tracked := *e
	tracked.Variables = make([]domain.Variable, len(e.Variables))
	copy(tracked.Variables, e.Variables)

	var localVars []domain.Variable
	for i, v := range tracked.Variables {
		if !v.Secret {
			continue
		}
		if v.Value != "" {
			localVars = append(localVars, domain.Variable{Key: v.Key, Value: v.Value, Enabled: v.Enabled, Secret: true})
		}
		tracked.Variables[i].Value = ""
	}

	if err := writeJSON(path, &tracked); err != nil {
		return err
	}

	if len(localVars) > 0 {
		local := domain.Environment{FormatVersion: e.FormatVersion, ID: e.ID, Name: e.Name, Variables: localVars}
		if err := writeJSON(localPathFor(path), &local); err != nil {
			return err
		}
	}

	return nil
}

func localPathFor(path string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + ".local" + ext
}
