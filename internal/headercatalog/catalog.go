// Package headercatalog supplies the list of common HTTP request-header
// names — and, per header, a list of common values — that the frontend's
// header editor offers as autocomplete suggestions. A built-in Default
// ships; a user can extend or override it by hand-editing headers.yaml in
// the config directory (appdata for the desktop app, the workspace root
// for the container server), mirroring how internal/theme is configured.
// There's no in-app editor — this is a config file by design.
package headercatalog

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Entry is one header the editor knows about: its canonical Name and,
// optionally, common Values to suggest for it. Values may be empty (the
// header is worth suggesting as a name, but has no small set of typical
// values, e.g. Cookie).
type Entry struct {
	Name   string   `yaml:"name" json:"name"`
	Values []string `yaml:"values,omitempty" json:"values,omitempty"`
}

// Default is the built-in catalog. Names use their conventional casing;
// matching against what the user has typed is case-insensitive (see
// Resolve). A trailing space in a value (e.g. "Bearer ") is intentional —
// it's a prefix the user completes.
var Default = []Entry{
	{Name: "Accept", Values: []string{"application/json", "application/xml", "text/plain", "text/html", "*/*"}},
	{Name: "Accept-Encoding", Values: []string{"gzip, deflate, br", "gzip", "identity"}},
	{Name: "Accept-Language", Values: []string{"en-US,en;q=0.9", "en"}},
	{Name: "Authorization", Values: []string{"Bearer ", "Basic "}},
	{Name: "Cache-Control", Values: []string{"no-cache", "no-store", "max-age=0"}},
	{Name: "Connection", Values: []string{"keep-alive", "close"}},
	{Name: "Content-Type", Values: []string{"application/json", "application/x-www-form-urlencoded", "multipart/form-data", "text/plain", "application/xml"}},
	{Name: "Cookie"},
	{Name: "Host"},
	{Name: "If-Match"},
	{Name: "If-None-Match"},
	{Name: "Origin"},
	{Name: "Prefer", Values: []string{"return=representation", "return=minimal", "respond-async"}},
	{Name: "Referer"},
	{Name: "User-Agent", Values: []string{"freeman"}},
	{Name: "X-Api-Key"},
	{Name: "X-Correlation-ID"},
	{Name: "X-Requested-With", Values: []string{"XMLHttpRequest"}},
}

const filename = "headers.yaml"

// Config is headers.yaml's shape. Headers is merged onto Default by name
// (case-insensitive): an entry whose name already exists in Default
// replaces that entry's value list in place; a new name is appended. An
// entry with no `values:` key leaves an existing entry's values untouched
// (and adds a valueless entry for a new name); an explicit `values: []`
// clears them.
type Config struct {
	Headers []Entry `yaml:"headers,omitempty"`
}

// Load returns headers.yaml's config from dir, or an empty Config if the
// file is absent or unparseable (Resolve then yields Default unchanged).
func Load(dir string) Config {
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		return Config{}
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}
	}
	return cfg
}

// Resolve merges cfg onto a copy of Default (see Config.Headers for the
// rules) and returns the result. Default is never mutated.
func Resolve(cfg Config) []Entry {
	out := make([]Entry, len(Default))
	for i, e := range Default {
		out[i] = Entry{Name: e.Name, Values: append([]string(nil), e.Values...)}
	}

	for _, override := range cfg.Headers {
		if override.Name == "" {
			continue
		}
		if i := indexOf(out, override.Name); i >= 0 {
			if override.Values != nil {
				out[i].Values = append([]string(nil), override.Values...)
			}
			continue
		}
		out = append(out, Entry{Name: override.Name, Values: append([]string(nil), override.Values...)})
	}
	return out
}

func indexOf(entries []Entry, name string) int {
	for i, e := range entries {
		if strings.EqualFold(e.Name, name) {
			return i
		}
	}
	return -1
}
