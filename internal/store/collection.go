package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"freeman/internal/domain"
)

// LoadCollection reads and parses a collection.json file.
func LoadCollection(path string) (*domain.Collection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c domain.Collection
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveCollection writes a collection.json file, creating its parent
// directory if needed.
func SaveCollection(path string, c *domain.Collection) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSON(path, c)
}
