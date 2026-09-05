package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"freeman/internal/domain"
)

// LoadCollection reads and parses a collection.json file. Normalizes a
// nil Items (e.g. a hand-edited file that omits the key) to an empty
// slice, so it always round-trips as JSON "[]" rather than "null" —
// every caller down the line (the frontend included) can then assume
// Items is a real, if possibly empty, list.
func LoadCollection(path string) (*domain.Collection, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	var c domain.Collection
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.Items == nil {
		c.Items = []domain.Item{}
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
