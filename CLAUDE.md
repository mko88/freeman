# Working rules for this repo

## Every interactive feature needs a Control API action

Freeman has a "control API" (loopback HTTP on `:8090` on desktop, driving
the Wails event bus — see `internal/wailsapp.DispatchUIAction`,
`App.svelte`'s `dispatchUIAction`, and `cmd/freeman/main.go`'s
`POST /api/ui/action`) so external scripts, test harnesses, and AI agents
can drive the GUI itself, not just its data operations.

**Rule: whenever a UI element the user can interact with is added or
changed, add (or update) a matching `ui:action` so the same thing can be
triggered through the control API.** This includes things like: a new
button, a new toggle/modal, a new field, a new tab, a new per-row action
(add/remove/set). It's not limited to brand-new screens — editing an
existing interactive element (e.g. adding a field to a form) means
updating its action's payload too.

Concretely, each new interactive element needs:
1. A `case` in `App.svelte`'s `dispatchUIAction` that performs the same
   state change a click/keystroke would.
2. A row in `App.svelte`'s `uiActions` array (shown in the in-app help
   modal) documenting the action name, payload shape, and what it does.
3. If it's a brand-new top-level capability (not just a UI action) rather
   than something reachable via `ui:action`, also add a route to
   `internal/httpapi/handler.go` and a row in `apiEndpoints`.

Prefer reusing an existing action's shape/pattern (e.g. `{ index }` or
`{ key }` for row targeting — see `removeRequestHeader`/`removeEnvironmentVariable`) over
inventing a new convention. The action list is curated by design — there's
no generic "click this selector" escape hatch, so a genuinely new kind of
interaction needs its own named action, not a workaround.

**Every settable field needs a getter too.** `GET /api/ui/state` (see
`internal/wailsapp.ReportUIState`/`UIState`, `App.svelte`'s
`reportUIState`) mirrors the whole editor draft — every `setRequestField`/
`setEnvironmentVariable`-style field, the header/form-field rows, the
open environment, and the result of the last `saveRequest`/`sendRequest`
— as one JSON object, so a script can read back what an action did
instead of screenshotting the window. When a new settable field is added
per the rule above, add it to `reportUIState`'s state object too so the
getter stays complete.

**Naming: spell out the target explicitly.** An action that operates on
the request currently in the editor gets `Request` in its name
(`saveRequest`, `addRequestHeader`, `setRequestField`); one that operates
on the environment currently in the editor gets `Environment`
(`saveEnvironment`, `addEnvironmentVariable`). Not a bare verb
(`save`, `addHeader`) — the list should read unambiguously on its own,
without needing the payload shape to disambiguate what it acts on.
Actions with no such ambiguity (`selectCollection`, `newRequest`,
`toggleHelp`, `openWorkspace`) don't need a target word added.

When finishing such a change, verify the new action actually works by
driving it through `POST http://127.0.0.1:8090/api/ui/action` (or the
relevant `/api/*` route), not just by clicking the UI. Send
`Content-Type: application/json` — `internal/httpapi.GuardSameOrigin`
refuses any request with a body that doesn't, and any request a browser
marks as cross-origin, so that a web page the user has open can't drive
the app behind their back. Don't relax that guard to make a client
easier to write; fix the client.

## Control API regression script

`scripts/test_control_api.py` (stdlib-only, run with `py
scripts/test_control_api.py`) drives every documented control-API route
and `ui:action` end to end against a running desktop build, verifying
each step's effect over HTTP/on disk. It leaves no trace: everything it
creates is deleted/removed again in a `finally` before it exits, and it
also sweeps for and removes any leftovers from a previous interrupted
run before it starts — the workspace should look identical before and
after any run, or any number of runs. `--delay` (default 0.6s) paces it
for watching the app window live; `--delay 0` runs it fast. It also
carries a standing regression check for a real concurrency bug found
2026-09-04 (rapid-fire `ui:action` calls racing; see git history / the
script's own comments for the fix in `App.svelte` and
`internal/store/format.go`).

The suite is a package, not one file: `scripts/test_control_api.py` is
the entry point (argument parsing and the three-phase run order) and
`scripts/control_api/` holds the rest — `client.py`, `report.py`,
`fixtures.py`, `server.py`, and one module per group of checks under
`checks/`.

Every request the suite makes Freeman send goes to a server it starts
itself (`scripts/control_api/server.py`): three loopback listeners on
OS-assigned ports — plain, TLS with a self-signed certificate, and TLS
demanding a client certificate — that report back what they actually
received. It answers httpbin's shapes on the endpoints that predate it
(`args`/`headers`/`data`/`json`/`form`/`files`), so nothing needs a
network. **A new request capability needs an endpoint here that can
prove it went out** — one that refuses the request until the feature
works, rather than one that returns 200 either way. `checks/
variations.py` is where the axes a request can differ along get
exercised: auth types, body modes, status codes, and every Options-tab
setting. The certificate pairs and the brotli sample the server needs
are committed under `scripts/control_api/testdata/`, with that
directory's README explaining how to regenerate them.

The same server runs standalone, for driving the app by hand:
`pwsh scripts/Start-TestServer.ps1` puts it on fixed ports (8100/8101/
8102) and detaches, `pwsh scripts/Stop-TestServer.ps1` stops it, and
`py scripts/seed_test_requests.py` fills a "Test Server" collection with
one saved request per endpoint and option worth demonstrating. Those
write to the real workspace on purpose, and never overwrite a request
that's already there.

**Whenever a `ui:action` is added, removed, or its payload shape
changes, update the matching module under `scripts/control_api/checks/`
in the same change** — it's the regression suite for the rule above, not
a one-off.

`checks/consistency.py` enforces that mechanically rather than trusting
anyone to remember it: it diffs the `uiActions` help table against
`dispatchUIAction`'s cases, both against the actions these checks
actually fire, and `apiEndpoints` against the routes registered in Go.
It reads source only, so `py scripts/test_control_api.py
--consistency-only` runs it with no app open. An action that genuinely
can't be driven headlessly goes in that module's `UNDRIVEABLE` map with
a reason — don't widen it to silence a check you simply haven't written.

## No changelog comments in code

**Rule: don't leave comments that narrate a change's history** — "retired
2026-09-04", "no longer X", "used to be Y", "removed in favor of Z", a
dated note explaining why a field/case/branch was deleted. That belongs
in the commit message and git history, not the source. A comment should
describe the code as it is now; if something isn't there anymore, it
needs no comment at all, not an epitaph. This doesn't apply to comments
documenting a non-obvious *constraint* the current code exists to
satisfy (e.g. why a reactive statement is written a particular way to
avoid a real bug) — that's about the present code being correct, not
about what used to be there.

## Release notes are short

**Rule: two sections, `## Changes` and `## Bug fixes`, and nothing
else.** No Downloads list — the assets are on the page already. No
account of what was verified. No known gaps — that is what `TODO.md` is
for.

**One line each, saying what changed rather than how it was found or
fixed.** The investigation belongs in the commit message, which still
has it:

    - Request options set as workspace defaults were ignored.

not a paragraph on which layer answered with the wrong defaults and why
the editor omits an options block in the first place.

Spend length only where the reader has to *do* something. A breaking
change goes first, marked, and may take a paragraph with the before and
after — everything else is a line.

The first release of anything is the exception: there is nothing to have
changed from, so it gets a sentence saying what the thing is and one list
of what it does.
