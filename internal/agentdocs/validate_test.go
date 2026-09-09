package agentdocs

import "testing"

// A catalogue shaped like the real one: one action of each payload form
// the frontend actually writes (see controlApiCatalog.ts's payload
// column — every row is one of these four shapes).
func testCatalog() Catalog {
	return Catalog{Actions: []Action{
		{Action: "sendRequest", Payload: "—"},
		{Action: "selectRequest", Payload: "{ id }"},
		{Action: "renameEnvironment", Payload: "{ name, id? }"},
		{Action: "removeRequestHeader", Payload: "{ index } | { key }"},
		{Action: "addRequestHeader", Payload: "{ key?, value?, enabled? }"},
	}}
}

func TestValidateAcceptsWhatTheDispatcherCanActOn(t *testing.T) {
	c := testCatalog()
	cases := []struct {
		name    string
		action  string
		payload map[string]any
	}{
		{"no payload wanted, none given", "sendRequest", nil},
		{"no payload wanted, one given anyway", "sendRequest", map[string]any{"stray": 1}},
		{"the required key", "selectRequest", map[string]any{"id": "r_1"}},
		{"required present, optional absent", "renameEnvironment", map[string]any{"name": "staging"}},
		{"required and optional", "renameEnvironment", map[string]any{"name": "staging", "id": "e_1"}},
		{"the first alternative", "removeRequestHeader", map[string]any{"index": 0}},
		{"the second alternative", "removeRequestHeader", map[string]any{"key": "Accept"}},
		{"all-optional, nothing given", "addRequestHeader", nil},
		// Present, not truthy: an action that turns something off or
		// clears a filter sends exactly these.
		{"a false value is a value", "addRequestHeader", map[string]any{"enabled": false}},
		{"an empty string is a value", "selectRequest", map[string]any{"id": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := c.Validate(tc.action, tc.payload); err != nil {
				t.Fatalf("Validate(%q, %v) = %v, want nil", tc.action, tc.payload, err)
			}
		})
	}
}

func TestValidateRefusesWhatItCannot(t *testing.T) {
	c := testCatalog()
	cases := []struct {
		name    string
		action  string
		payload map[string]any
	}{
		{"an action nobody has", "noSuchAction", nil},
		{"a required key missing", "selectRequest", nil},
		// The actual mistake this check exists for: the fields sent
		// beside "action" instead of inside "payload", which reaches the
		// route as no payload at all.
		{"the payload sent unwrapped", "selectRequest", map[string]any{}},
		{"a null where a value was needed", "selectRequest", map[string]any{"id": nil}},
		{"neither alternative", "removeRequestHeader", map[string]any{"name": "Accept"}},
		{"only the optional half", "renameEnvironment", map[string]any{"id": "e_1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := c.Validate(tc.action, tc.payload); err == nil {
				t.Fatalf("Validate(%q, %v) = nil, want an error", tc.action, tc.payload)
			}
		})
	}
}

// Before the window mounts there is no catalogue, and refusing every
// action would be a worse failure than the silence this replaces.
func TestValidateFailsOpenWithNoCatalogue(t *testing.T) {
	// Parenthesised because Go cannot parse a composite literal at the
	// head of an if statement.
	empty := Catalog{}
	if err := empty.Validate("noSuchAction", nil); err != nil {
		t.Fatalf("an unreported catalogue should validate nothing, got %v", err)
	}
}

// The payload column is written for a person; this is the reading of it
// that the check depends on.
func TestRequiredKeys(t *testing.T) {
	cases := map[string][][]string{
		"—":                          nil,
		"{ id }":                     {{"id"}},
		"{ name, id? }":              {{"name"}},
		"{ field, value }":           {{"field", "value"}},
		"{ key?, value?, enabled? }": {nil},
		"{ index } | { key }":        {{"index"}, {"key"}},
		"{ index, key?, enabled? }":  {{"index"}},
	}
	for doc, want := range cases {
		got := requiredKeys(doc)
		if len(got) != len(want) {
			t.Fatalf("requiredKeys(%q) = %v, want %v", doc, got, want)
		}
		for i := range want {
			if len(got[i]) != len(want[i]) {
				t.Fatalf("requiredKeys(%q)[%d] = %v, want %v", doc, i, got[i], want[i])
			}
			for j := range want[i] {
				if got[i][j] != want[i][j] {
					t.Fatalf("requiredKeys(%q)[%d] = %v, want %v", doc, i, got[i], want[i])
				}
			}
		}
	}
}
