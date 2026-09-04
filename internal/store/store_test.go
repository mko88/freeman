package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"freeman/internal/domain"
)

func TestCollectionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "collections", "my-api", "collection.json")

	want := &domain.Collection{
		FormatVersion: "1",
		ID:            "c_abc123",
		Name:          "My API",
		Items: []domain.Item{
			{
				Type: domain.ItemTypeFolder,
				ID:   "f_111111",
				Name: "Auth",
				Items: []domain.Item{
					{
						Type:    domain.ItemTypeRequest,
						ID:      "r_222222",
						Name:    "Login",
						Method:  "POST",
						URL:     "{{baseUrl}}/auth/login",
						Headers: []domain.Header{{Key: "Content-Type", Value: "application/json", Enabled: true}},
						Body:    &domain.Body{Mode: domain.BodyModeRaw, Raw: `{"username":"{{username}}"}`},
					},
				},
			},
		},
	}

	if err := SaveCollection(path, want); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	got, err := LoadCollection(path)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round trip mismatch:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestEnvironmentRoundTripWithSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "environments", "dev.json")

	original := &domain.Environment{
		FormatVersion: "1",
		ID:            "e_abc123",
		Name:          "Development",
		Variables: []domain.Variable{
			{Key: "baseUrl", Value: "https://api.dev.example.com", Enabled: true, Secret: false},
			{Key: "apiKey", Value: "super-secret", Enabled: true, Secret: true},
		},
	}

	if err := SaveEnvironment(path, original); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}

	// The tracked file must not contain the secret value on disk.
	rawTracked, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tracked file: %v", err)
	}
	var tracked domain.Environment
	if err := json.Unmarshal(rawTracked, &tracked); err != nil {
		t.Fatalf("unmarshal tracked file: %v", err)
	}
	for _, v := range tracked.Variables {
		if v.Secret && v.Value != "" {
			t.Fatalf("secret variable %q has a value in the tracked file: %q", v.Key, v.Value)
		}
	}

	// Loading through the package merges the .local.json override back in,
	// so the resolved environment matches the original.
	got, err := LoadEnvironment(path)
	if err != nil {
		t.Fatalf("LoadEnvironment: %v", err)
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("round trip mismatch:\nwant %+v\ngot  %+v", original, got)
	}
}

// TestWriteJSONAtomicUnderConcurrentReads is a regression test for a race
// found via scripts/test_control_api.py: its rapid-fire check polls
// GET /api/environments/{id} (LoadEnvironment, on the control API's own
// goroutine) right after firing two back-to-back saveEnvironment actions
// (an App.SaveEnvironment RPC each, on Wails' goroutine) and hit
// `invalid character '"' after top-level value` — a reader observing the
// file mid-write. Hammers SaveEnvironment on one goroutine and
// LoadEnvironment on another, concurrently, and fails on any read error —
// writeJSON's temp-file-then-rename should make every read see either the
// old or the new file in full, never a partial one.
func TestWriteJSONAtomicUnderConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "environments", "dev.json")

	if err := SaveEnvironment(path, &domain.Environment{FormatVersion: "1", ID: "e_abc123", Name: "Development"}); err != nil {
		t.Fatalf("initial SaveEnvironment: %v", err)
	}

	const iterations = 200
	writesDone := make(chan struct{})
	errs := make(chan error, iterations*2)

	go func() {
		defer close(writesDone)
		for i := 0; i < iterations; i++ {
			e := &domain.Environment{
				FormatVersion: "1",
				ID:            "e_abc123",
				Name:          "Development",
				Variables:     []domain.Variable{{Key: "n", Value: fmt.Sprintf("%d", i), Enabled: true}},
			}
			if err := SaveEnvironment(path, e); err != nil {
				errs <- fmt.Errorf("SaveEnvironment #%d: %w", i, err)
				return
			}
		}
	}()

	readsDone := make(chan struct{})
	go func() {
		defer close(readsDone)
		for {
			select {
			case <-writesDone:
				return
			default:
			}
			if _, err := LoadEnvironment(path); err != nil {
				errs <- fmt.Errorf("concurrent LoadEnvironment: %w", err)
			}
		}
	}()

	<-writesDone
	<-readsDone
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
