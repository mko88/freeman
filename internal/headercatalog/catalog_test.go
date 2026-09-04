package headercatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func find(entries []Entry, name string) (Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

func TestLoadDefaultsWhenNoFile(t *testing.T) {
	got := Resolve(Load(t.TempDir()))
	if len(got) != len(Default) {
		t.Fatalf("expected %d entries, got %d", len(Default), len(got))
	}
	if _, ok := find(got, "Content-Type"); !ok {
		t.Fatalf("expected Content-Type in default catalog")
	}
}

func TestResolveDoesNotMutateDefault(t *testing.T) {
	origLen := len(Default)
	origAcceptValues := append([]string(nil), Default[0].Values...)

	Resolve(Config{Headers: []Entry{
		{Name: "Accept", Values: []string{"x"}},
		{Name: "X-New", Values: []string{"y"}},
	}})

	if len(Default) != origLen {
		t.Fatalf("Default length changed: %d -> %d", origLen, len(Default))
	}
	if got := Default[0].Values; len(got) != len(origAcceptValues) {
		t.Fatalf("Default[0].Values mutated: %v", got)
	}
}

func TestResolveMergesByName(t *testing.T) {
	dir := t.TempDir()
	yaml := `
headers:
  - name: accept            # case-insensitive match of the default "Accept"
    values: ["application/json"]
  - name: X-Tenant-ID
    values: ["acme", "globex"]
`
	if err := os.WriteFile(filepath.Join(dir, "headers.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Resolve(Load(dir))

	accept, ok := find(got, "Accept")
	if !ok {
		t.Fatal("Accept missing after merge")
	}
	if len(accept.Values) != 1 || accept.Values[0] != "application/json" {
		t.Fatalf("Accept values not replaced: %v", accept.Values)
	}

	tenant, ok := find(got, "X-Tenant-ID")
	if !ok {
		t.Fatal("X-Tenant-ID not appended")
	}
	if len(tenant.Values) != 2 || tenant.Values[1] != "globex" {
		t.Fatalf("X-Tenant-ID values wrong: %v", tenant.Values)
	}
}

func TestResolveNilValuesKeepsDefault(t *testing.T) {
	got := Resolve(Config{Headers: []Entry{{Name: "Content-Type"}}}) // no values: key

	ct, ok := find(got, "Content-Type")
	if !ok {
		t.Fatal("Content-Type missing")
	}
	if len(ct.Values) == 0 {
		t.Fatal("Content-Type values were cleared by a valueless override")
	}
}

func TestResolveEmptyValuesClears(t *testing.T) {
	dir := t.TempDir()
	yaml := "headers:\n  - name: Content-Type\n    values: []\n"
	if err := os.WriteFile(filepath.Join(dir, "headers.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	ct, ok := find(Resolve(Load(dir)), "Content-Type")
	if !ok {
		t.Fatal("Content-Type missing")
	}
	if len(ct.Values) != 0 {
		t.Fatalf("expected cleared values, got %v", ct.Values)
	}
}

func TestLoadMalformedFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "headers.yaml"), []byte("not: [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(Load(dir)); len(got) != len(Default) {
		t.Fatalf("expected fallback to Default (%d), got %d", len(Default), len(got))
	}
}
