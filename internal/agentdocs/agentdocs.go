// Package agentdocs renders the control API's own instructions, for
// GET /api/agent — the endpoint an AI agent hits first to learn how to
// drive Freeman.
//
// The route and action tables aren't written here. They live in the
// frontend (cmd/freeman/frontend/src/lib/controlApiCatalog.ts), which is
// also what the in-app help modal renders and what
// scripts/control_api/checks/consistency.py diffs against
// dispatchUIAction's cases and the routes Go registers. The frontend
// reports them on mount (wailsapp.App.ReportControlAPIDocs) and this
// package formats them, so there is one list, checked in one place,
// rather than a second copy in Go that could drift from the app it
// claims to describe.
//
// What is written here is the part Go owns: the base URL, the rules the
// same-origin guard enforces, and the act-then-read loop.
package agentdocs

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Endpoint and Action mirror controlApiCatalog.ts's two arrays.
type Endpoint struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Desc   string `json:"desc"`
}

type Action struct {
	Action  string `json:"action"`
	Payload string `json:"payload"`
	Desc    string `json:"desc"`
}

type Catalog struct {
	Endpoints []Endpoint `json:"apiEndpoints"`
	Actions   []Action   `json:"uiActions"`
}

// Parse reads what the frontend reported. An empty or unparseable
// catalog isn't an error: the document still explains how the API works,
// and says plainly that the lists are missing rather than implying the
// app has no actions.
func Parse(catalogJSON string) Catalog {
	var c Catalog
	if catalogJSON == "" {
		return c
	}
	_ = json.Unmarshal([]byte(catalogJSON), &c)
	return c
}

// Render writes the instructions as Markdown — the format an agent reads
// with no parsing, and a person can read in a terminal.
func Render(c Catalog, baseURL string) string {
	var b strings.Builder

	b.WriteString(`# Freeman control API

Freeman is a desktop HTTP client — a Postman alternative. This API drives
the running app itself, not a copy of its data: an action here moves the
same UI a person clicks, and the state you read back is what is on
screen. It exists so a script or an agent can operate the app and check
what happened without taking a screenshot.

You are talking to a local process over loopback. There is one running
app and one workspace open in it; everything you do is visible to whoever
is sitting in front of it, and persists to their disk.

`)

	fmt.Fprintf(&b, "## Base URL\n\n    %s\n\n", baseURL)

	b.WriteString(`## Rules the server enforces

- **Send ` + "`Content-Type: application/json`" + ` on every request with a body.**
  A body under any other content type is refused with 415. This is not
  pedantry: it is what stops a web page the user happens to have open
  from driving their app behind their back.
- Requests a browser marks as cross-origin are refused with 403. A
  plain scripted request (no ` + "`Origin`" + `, no ` + "`Sec-Fetch-Site`" + `) is fine.
- Errors come back as ` + "`{\"error\": \"...\"}`" + ` with a 4xx or 5xx status.

## How to use it

Two endpoints do the driving. Everything else is data access.

1. ` + "`POST /api/ui/action`" + ` with ` + "`{\"action\": \"...\", \"payload\": {...}}`" + ` performs
   one thing a person could do in the UI. It answers 204 as soon as the
   action is queued, or 400 with an error if the action isn't one of
   those below or its payload is one the action can't be performed with.
   The action's own fields go **inside** ` + "`payload`" + `, not beside
   ` + "`action`" + ` — that mistake used to be answered with a 204 and no
   effect.
2. ` + "`GET /api/ui/state`" + ` returns the whole editor as one JSON object —
   the unsaved draft, which tabs are open, and the last response.

Actions are applied in the order they arrive, but 204 means *accepted*,
not *finished*: several actions (` + "`saveRequest`" + `, ` + "`sendRequest`" + `,
` + "`openWorkspace`" + `, ` + "`selectCollection`" + `) do a round trip to disk or the
network. **After an action, poll ` + "`GET /api/ui/state`" + ` until it shows what
you expected, rather than sleeping or assuming.** That is the whole
working loop:

    # open the Body tab of the request currently selected
    curl -sS -X POST BASE/api/ui/action \
      -H 'Content-Type: application/json' \
      -d '{"action":"selectRequestTab","payload":{"tab":"body"}}'

    # send it, then read back what came out
    curl -sS -X POST BASE/api/ui/action \
      -H 'Content-Type: application/json' \
      -d '{"action":"sendRequest"}'
    curl -sS BASE/api/ui/state | jq '.response.statusCode, .response.sizeBytes'

There is no generic "click this selector" action, by design. The list
below is curated: if something is not in it, the UI cannot be made to do
it from here, and the honest answer is to say so rather than to
improvise.

`)

	b.WriteString("## Actions\n\n")
	if len(c.Actions) == 0 {
		b.WriteString("_Not reported yet — the app's window has not finished starting. Retry shortly._\n\n")
	} else {
		b.WriteString("| action | payload | what it does |\n| --- | --- | --- |\n")
		for _, a := range c.Actions {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", a.Action, cell(a.Payload), cell(a.Desc))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Data routes\n\n")
	if len(c.Endpoints) == 0 {
		b.WriteString("_Not reported yet — the app's window has not finished starting. Retry shortly._\n")
	} else {
		b.WriteString("| method | path | what it does |\n| --- | --- | --- |\n")
		for _, e := range c.Endpoints {
			fmt.Fprintf(&b, "| %s | `%s` | %s |\n", e.Method, e.Path, cell(e.Desc))
		}
	}

	return strings.ReplaceAll(b.String(), "BASE/", baseURL+"/")
}

// cell keeps a description on one row: a literal pipe would end the
// column early, and a newline would end the row.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.Join(strings.Fields(s), " ")
}

// Validate reports whether an action name and payload are ones the app
// can act on, for POST /api/ui/action to refuse before dispatching.
//
// It exists because dispatching is fire-and-forget: the event goes to the
// frontend and the route answers 204 without waiting, so an action the
// dispatcher ignored was indistinguishable from one it performed. A
// script — or an agent, which is the caller this API is for — got a
// success and no effect, with nothing to go on.
//
// The catalogue is the schema, rather than a second list written here,
// for the same reason the docs are: consistency.py already holds it to
// the dispatcher, so validation checked against it cannot describe an
// app that doesn't exist. The payload column's convention is the whole
// grammar — see requiredKeys.
//
// Fails open when the frontend hasn't reported a catalogue yet (before
// the window mounts): with nothing to check against, refusing everything
// would be worse than the silence this replaces.
func (c Catalog) Validate(action string, payload map[string]any) error {
	if len(c.Actions) == 0 {
		return nil
	}
	for _, a := range c.Actions {
		if a.Action != action {
			continue
		}
		alts := requiredKeys(a.Payload)
		if len(alts) == 0 {
			return nil
		}
		for _, required := range alts {
			if hasAll(payload, required) {
				return nil
			}
		}
		// Naming the wrapper explicitly: sending the fields beside
		// "action" instead of inside "payload" is the way this goes
		// wrong, and it looks like the action being broken.
		return fmt.Errorf(
			"action %q needs payload %s — the fields go inside a \"payload\" object, beside \"action\"",
			action, a.Payload,
		)
	}
	return fmt.Errorf("unknown action %q — GET /api/agent lists every action this app answers to", action)
}

// requiredKeys reads the catalogue's payload column as a schema. The
// column is written for a person, but consistently enough to be one:
//
//	—                      nothing required
//	{ id }                 id is required
//	{ name, id? }          name is required, id is not
//	{ index } | { key }    either an index or a key
//
// Anything with no braces at all (the em dash) requires nothing, which
// is also the safe answer for a row written in some shape this doesn't
// know: the check is here to catch a caller's mistake, not to become one.
func requiredKeys(doc string) [][]string {
	if !strings.Contains(doc, "{") {
		return nil
	}
	var alternatives [][]string
	for _, group := range strings.Split(doc, "|") {
		group = strings.TrimSpace(group)
		group = strings.TrimSuffix(strings.TrimPrefix(group, "{"), "}")
		var required []string
		for _, field := range strings.Split(group, ",") {
			field = strings.TrimSpace(field)
			if field == "" || strings.HasSuffix(field, "?") {
				continue
			}
			required = append(required, field)
		}
		alternatives = append(alternatives, required)
	}
	return alternatives
}

// hasAll reports whether every named key is present and not null. Present
// is the test, not truthy: false and "" are both things an action is
// meant to be sent — setRequestOption turns things off, filterRequests
// clears the filter with "".
func hasAll(payload map[string]any, required []string) bool {
	for _, key := range required {
		v, ok := payload[key]
		if !ok || v == nil {
			return false
		}
	}
	return true
}
