# Freeman — Feature Roadmap

Overview of what's implemented and what to build next. Status as of
2026-09-04.

Legend: ✅ done · 🟡 partial · ⬜ not started

---

## Feature matrix

| Feature | Status | Where it stands / where the seam is |
|---|---|---|
| RESTful API support | 🟡 | `internal/httpengine.Execute`: any method, `{{var}}` in URL/params/headers, **raw** body + Content-Type, captures status/headers/body/time/size, 30s timeout. |
| Environments | ✅ | Multiple envs; variables with enabled/secret flags; `{{var}}` substitution; `.local.json` overlay for secrets; in-app editor modal. |
| GraphQL API support | ⬜ | Needs a query editor + variables pane + (optional) schema introspection. Could reuse the raw-body path for transport. |
| SOAP API support | ⬜ | Needs raw-XML body mode surfaced + optional WSDL import. Low priority. |
| Request chaining | ⬜ | Domain already has `Item.PreRequestScript` / `Item.TestScript`, but nothing runs them — `core.ExecuteRequest` has no scripting seam. Needs: a `script.Engine` (goja), response→variable extraction, then feed vars forward. |
| Random data | ⬜ | Extend `httpengine.Substitute` (or a pre-pass) with dynamic tokens: `{{$guid}}`, `{{$timestamp}}`, `{{$randomInt}}`, faker-style. |
| API testing | ⬜ | `Item.TestScript` present but not executed. Needs a `script.Engine` (goja) + a test-results pane — the pane matters, because a failed assertion is not a failed request and needs somewhere of its own to surface. |
| API monitoring | ⬜ | Needs a scheduler + run history + alerting. Largest new surface; depends on testing landing first. |
| CLI | 🟡 | No `freeman run …` binary. `cmd/freeman-server` (HTTP/container) + desktop control API on `:8090` cover scripting. A `cmd/freeman-cli` headless runner was always planned. |
| Team collaboration | 🟡 | Collections + environments are plain git-friendly JSON in a workspace folder (secrets in `.local.json` stay out of VCS). No in-app sync/sharing/comments. |

---

## Control-API coverage audit (2026-09-04)

Verified every interactive element currently in the UI has a working
`ui:action` (see `CLAUDE.md`'s standing rule) by driving each one through
`POST /api/ui/action` and checking the result on disk / on screen —
including filling a request's body end-to-end (`setRequestField bodyRaw`,
`saveRequest`, `sendRequest`) and a full env-variable add/update/
remove/save cycle. Found and fixed one real bug in the process: rapid
back-to-back `ui:action` calls (no delay between them — the realistic
case for a script or agent) could race, because `dispatchUIAction` wasn't
awaited and several actions (`saveRequest`, `sendRequest`, `saveEnvironment`,
`selectEnvironment`, `selectCollection`, `openWorkspace`) do an async
round trip. A `removeEnvironmentVariable` sandwiched between two `saveEnvironment`
calls got silently clobbered. Fixed by serializing `ui:action` events
through one promise chain in `App.svelte` so each action's async work
finishes before the next one starts. Re-verified the same sequence
(zero delay) now applies correctly and in order.

**Gap found, not a control-API problem:** there's no way to delete a
saved request, collection, or environment anywhere — not in the UI, so
correctly also not in the control API. Worth adding alongside the Params
tab / nested folders work in §1–2 below.

**Update 2026-09-04:** request deletion shipped — `domain.Collection.RemoveItem`
(walks folders too), `core.App.DeleteRequest`, `DELETE
/api/collections/{id}/requests/{itemId}`, a sidebar × button (asks for
confirmation) and a `deleteRequest` ui:action (doesn't — a script/agent
has already decided). Collection/environment deletion is still open.

**Update 2026-09-04 (later the same day):** renamed several `ui:action`
names to spell out their target explicitly, per the naming rule now in
`CLAUDE.md` — `toggleEnvEditor`→`toggleEnvironmentEditor`,
`selectItem`→`selectRequest`, `deleteItem`→`deleteRequest`,
`save`→`saveRequest`, `send`→`sendRequest`, `selectTab`→`selectRequestTab`,
`setField`→`setRequestField`, `addHeader`/`setHeader`/`removeHeader`→
`add`/`set`/`removeRequestHeader`, `addVariable`/`setVariable`/`removeVariable`→
`add`/`set`/`removeEnvironmentVariable`.

**Update 2026-09-04 (later still):** added the read-side counterpart to
`POST /api/ui/action` — `GET /api/ui/state` (`internal/wailsapp.ReportUIState`/
`UIState`, `App.svelte`'s `reportUIState`, called at the end of every
`dispatchUIAction`). Returns the whole editor draft as JSON (every
`setRequestField`/`setEnvironmentVariable`-style field, header/form-field
rows, the open environment, and — the main point — the result of the
last `saveRequest`/`sendRequest`), so a script can read back what an
action did instead of screenshotting the window. `CLAUDE.md`'s standing
rule now also requires a getter for every settable field.

**Update 2026-09-05 (security):** a code review found the control API was
reachable from any web page the user had open. Loopback keeps other
machines out, but not a browser: a cross-origin "simple request" needs no
CORS preflight, and while the reply is unreadable the write still lands —
enough to point a saved request at an attacker's server, put
`{{apiToken}}` in its body and send it, since Freeman substitutes secret
values itself. Closed by `internal/httpapi.GuardSameOrigin` (refuses a
foreign `Origin`/`Sec-Fetch-Site`, and requires
`Content-Type: application/json` on any request with a body, which forces
a preflight that then fails). Applied inside `httpapi.NewHandler` *and*
around `cmd/freeman`'s outer control mux, since that one owns the
`/api/ui/*` routes. Covered by `internal/httpapi/guard_test.go` and
`test_api_guard` in `scripts/test_control_api.py`.

Same review: `cmd/freeman-server` defaulted to `:8080` — all interfaces,
no auth, full workspace read/write (secrets included) plus arbitrary
outbound HTTP via `POST /api/execute`, i.e. a disclosure and an SSRF
pivot for anyone who could route to it. Now defaults to
`127.0.0.1:8080`; the container image still sets `FREEMAN_LISTEN=:8080`
(a published Docker port can't reach a loopback-bound process inside),
and `docker-compose.yml` publishes it as `127.0.0.1:8080:8080` so
widening it is a deliberate act.

**Update 2026-09-05 (robustness, same review):** `httpengine.Execute` read
response bodies with an unbounded `io.ReadAll` and then decompressed them
into a second unbounded buffer — `LargeResponseThreshold` only decided
what to do once the whole thing was already resident, so a few hundred KB
of gzip or brotli could inflate to gigabytes and kill the app. Both reads
now go through `readCapped` against `httpengine.MaxResponseBytes` (64 MiB,
a var so it can be tuned), and `Response.Capped` says the body holds only
what was read — distinct from `Truncated`, where the whole body exists
and merely lives in `BodyFile`. The response pane marks a capped body next
to its size. The hardcoded 30s client timeout became
`httpengine.RequestTimeout`, applied as a context deadline so it composes
with the caller's context instead of shadowing it. Verified live: a 100 KB
gzip response that decodes to 100 MiB now comes back capped at 64 MiB
instead of taking the app down.

Same review: `itemID` reached the filesystem unchecked in
`internal/wailsapp/responsecache.go` — `filepath.Join` *cleans* a `../`
rather than refusing it, so a crafted ID could read, open externally or
delete files outside the cache, and glob metacharacters in an ID could
match other items' files. All path building now goes through one
`cachePath` helper behind an `^[A-Za-z0-9_-]+$` check. That package also
got its first tests (0% → 46.8%), covering the cache round trip, the
extension swap on a changed Content-Type, `trimLargeBody` either side of
the threshold, and the traversal refusals.

**Update 2026-09-05 (test suite):** `scripts/test_control_api.py` had
grown to ~1,700 lines in one file, so it's a package now — that file is
just the entry point (argument parsing, the three-phase run order) and
`scripts/control_api/` holds `client.py`, `report.py`, `fixtures.py`, and
one module per group of checks under `checks/`. Still stdlib-only, still
`py scripts/test_control_api.py`.

The four-places-at-once rule (dispatcher, help table, state mirror,
suite) is now enforced by `checks/consistency.py` instead of being
remembered: it diffs `uiActions` against `dispatchUIAction`'s cases, both
against the actions the checks actually fire, and `apiEndpoints` against
the routes registered in Go. It reads source only, so
`--consistency-only` runs it with no app open — which also makes it the
one part of this suite CI can run. It immediately found the drift the
code review had spotted by hand: `setRequestHeader`,
`setRequestFormField` and `copyResponseCachePath` were documented but
never driven. All three are covered now; the only exemptions are the two
actions that launch an external editor or file manager, listed with
reasons in that module's `UNDRIVEABLE` map.

**Update 2026-09-06 (frontend split):** `App.svelte` was 3,090 lines —
one component holding the whole UI, with 1,095 lines of CSS in it. Split
over five steps, each merged separately with the control-API suite run
in between:

1. Design-system primitives (modal shell, form controls, `.kv-table`,
   tab strips, `.prose`) moved to `style.css`. Svelte scopes a
   component's `<style>` to its own markup, so this had to happen before
   any markup could move — a child component would otherwise render
   unstyled. `HelpModal.svelte` followed, taking the `apiEndpoints`/
   `uiActions` tables with it; `lib/format.ts` took `methodColor`.
2. `lib/responseFormat.ts` (kind sniffing, pretty-printers,
   highlighters) and `SettingsModal.svelte`.
3. `ResponsePane.svelte`.
4. The eleven `draft*` variables became one `draft` object in
   `lib/requestDraft.ts`, whose keys are deliberately the names
   `GET /api/ui/state` reports and `setRequestField` accepts — so
   `reportUIState`'s ten hand-written lines are now `...draft` and the
   mirror can't drift from what it mirrors. Then
   `RequestEditor.svelte`.
5. `RequestList.svelte` and `StatusBar.svelte`.

App.svelte is now 1,257 lines (1,021 script, 111 markup, 124 style) and
holds the coordination layer that has to be one thing: backend calls,
the `ui:action` dispatcher, the state mirror, and the splitter drag
maths. Mutating functions stayed with it throughout — the control API
drives the same operations, so a scripted action and a clicked one take
the same path; components get them as grouped callback props
(`env`, `rows`, `cache`). Every step verified no CSS rule was dropped by
diffing the parsed selector sets.

**Update 2026-09-06 (the rest of the review):**
`src/backend.contract.ts` now asserts, at type-check time, that the two
`$backend` implementations have the same exported shape. tsconfig can
only resolve `$backend` to the Wails variant, so before this the HTTP one
was unchecked — adding an export to one and forgetting the other failed
at runtime, in whichever build you hadn't tested. Verified by breaking it
both ways: a missing export and a changed signature each fail
`npm run check`, naming the export.

`internal/script` and `internal/importer` deleted. Both were interfaces
with no implementation and no caller, and this file claimed `NoopEngine`
was wired when nothing wired it. Wiring it wasn't the one-liner it
looked like either: `RunTest` returns "an assertion failed", and until
there's a test-results pane that error has nowhere to go that doesn't
make a good response read as a failed request. The plan stays here in
prose; git history keeps the interfaces.

Still open from that review: CI, and no root README.

CI was written and then dropped before merging, because three Go tests
execute real requests against httpbin.org — `core.TestExecuteRequestCommonMethods` (all five common methods), `core.TestVerticalSlice`, and
`httpapi.TestVerticalSliceOverHTTP`. They're fast (~1.4s) and valuable
locally, but on a shared runner they make the build red whenever httpbin
is down or rate-limiting, which is how a team learns to ignore CI.
Guard them with `testing.Short()` and run `go test -short -race ./...`
in CI, and the rest is straightforward: no apt dependencies are needed,
because the only cgo in the whole dependency graph is wails'
`signal_linux.go` and it includes libc headers only — no GTK, no WebKit.
Node must be pinned to 22 (same reason `.devcontainer/setup.sh` pins
it). The suite's `--consistency-only` section is the part that runs
without a GUI; the other 156 checks drive a real webview and have to
stay local.

---

## Also already built (not on the survey list)

- Plain-file, hand-editable, git-friendly storage format (`internal/store`).
- One codebase → desktop (Wails) **and** container server (`cmd/freeman-server`), frontend swapped via the `$backend` alias.
- Control API / Wails event bus (`POST /api/ui/action`) — external scripts & AI agents can drive both data ops and the UI itself; in-app help modal + status bar.
- YAML-configurable theming (`theme.yaml`, `internal/theme`) with a built-in dark palette.
- YAML-configurable header autocomplete catalog (`headers.yaml`, `internal/headercatalog`).

---

## Suggested build order

### 1. Finish the REST client (small, high value)
- [x] Delete a saved request — sidebar × button + `deleteRequest` ui:action + `DELETE /api/collections/{id}/requests/{itemId}`. Shipped 2026-09-04.
- [x] Delete a saved collection/environment — both are managed from the settings window's Collections/Environments tabs (a row per thing, renamed in place, deleted where it sits), with the top bar picking which is open. `CreateCollection`/`RenameCollection`/`DeleteCollection` + `POST`/`PATCH`/`DELETE /api/collections`; `newCollection`/`renameCollection`/`deleteCollection`/`renameEnvironment`/`expandEnvironment` ui:actions. Shipped 2026-09-07.
- [x] Options tab: per-request transport switches — `domain.Options` (follow redirects, max redirects, share the cookie jar, timeout, skip TLS verify, client certificate), applied in `httpengine.clientFor`/`transportFor`. Not following returns the 3xx itself, which is the only way to assert on a redirect's status or `Location`; the shared jar makes a sign-in-then-call flow work across separate requests, and turning it off isolates a request from that session. `setRequestOption` ui:action, `options` in `GET /api/ui/state`, and `internal/codegen` now emits `-L`/`--max-redirs` and `-MaximumRedirection` so a generated script still sends what Send sends. Shipped 2026-09-07.
- [x] Params tab in the request editor — a Params tab (before Headers/Body) editing `domain.Item.Params` as a key/value/enabled list, appended to the URL at execution time by `httpengine.buildURL` (no bidirectional URL-string sync). `addRequestParam`/`setRequestParam`/`removeRequestParam` ui:actions, `params` in `GET /api/ui/state`, tab-count badges, `scripts/test_control_api.py` coverage (editor round-trip + an end-to-end check that a param reaches httpbin's echoed `args`). Shipped 2026-09-05.
- [x] `form-data` and `x-www-form-urlencoded` body modes — `domain.Body.FormFields`, `httpengine.Execute` (multipart via `mime/multipart`, urlencoded via `url.Values`), a body-mode picker (real radio buttons, lowercase kebab-case labels matching the mode values) + shared key/value table in the Headers/Body editor, `addRequestFormField`/`setRequestFormField`/`removeRequestFormField` ui:actions. Shipped 2026-09-04.
- [x] Test coverage for all 7 HTTP methods × all 4 body modes — `internal/httpengine.TestExecuteAllMethodsAndBodyTypes` (table-driven, deterministic, no network: GET/HEAD/OPTIONS with no body, POST+raw, PUT+form-data, PATCH+urlencoded, DELETE with no body). `scripts/test_control_api.py`'s new "HTTP methods" section adds the thinner end-to-end slice — GET/PUT/PATCH/DELETE through the real UI + control API + real network (POST already covered by the execute section). Shipped 2026-09-04.
- [x] File uploads — a form-data row can be typed `file` (native `SelectFile` picker or a control-API `filePath`), sent as a real multipart file part with a detected Content-Type (extension first, then sniffed, `internal/httpengine`'s `writeFormFile`/`detectContentType`); plus a standalone `binary` body mode (`Body.BinaryFilePath`) that sends a whole file as the request body. `addRequestFormField`/`setRequestFormField` gained `type`/`filePath`; `setRequestField` gained `binaryFilePath`. Go tests (`TestExecuteFormDataFileField`, `TestExecuteBinaryBody`) plus a self-contained `scripts/test_control_api.py` section covering both against real httpbin. Shipped 2026-09-04.
- [x] Auth tab: `domain.Item.Auth` (type none/bearer/basic/apikey), `httpengine.applyAuth` builds the header at execute time (after `{{var}}` substitution; overrides a hand-written `Authorization` row). Request-editor tab between Headers and Body with a live header preview; `setRequestAuth` ui:action, `auth` in `GET /api/ui/state`, `scripts/test_control_api.py` coverage. Also fixed a pre-existing `core.App` data race (added `App.mu`) the rename-on-save path made reproducible. Shipped 2026-09-05.
- [x] Code tab: renders the draft request as a runnable script — `internal/codegen` (bash around curl, PowerShell around Invoke-RestMethod), mirroring `httpengine.Execute`'s substitution/headers/auth/body handling. `POST /api/codegen`, `selectCodeFormat`/`copyRequestCode` ui:actions, `codeFormat`/`code` in `GET /api/ui/state`, Go + control-API test coverage. Shipped 2026-09-05; the two one-liner formats were dropped 2026-09-06 — the script forms paste and run just as well, and keep the body readable.
- [ ] Response niceties: pretty-print/format JSON (done), show response headers (done), copy-as-curl (done via the Code tab), copy response as-is.

### 2. Nested folders
- [ ] Sidebar tree UI (`domain.Item` is already a folder/request union with `Items []Item`; `Collection.UpsertItem`/`FindItem` walk the tree). Currently the sidebar renders a flat list.

### 3. Scripting engine → unlocks chaining + testing
- [ ] Add a `goja`-backed `script.Engine` (an interface sketch was deleted 2026-09-06 as dead code — see git history if the shape is still useful).
- [ ] Wire `Engine.RunPreRequest` / `RunTest` into `core.ExecuteRequest`, and decide where a failed assertion surfaces — it must not read as a failed request.
- [ ] Minimal `pm`-style API: `pm.response`, `pm.environment.set`, `pm.test(...)`, assertions.
- [ ] Test-results pane in the response area.
- [ ] **Request chaining** then falls out: a test/pre-request script sets a variable from a prior response.

### 4. Random / dynamic data
- [ ] Dynamic `{{$...}}` tokens resolved before/inside `Substitute`.

### 5. Importers
- [ ] Postman Collection v2.1 → `domain.Collection`.
- [ ] OpenAPI 3 → `domain.Collection`.
- [ ] cURL paste → single request.

### 6. Headless CLI (`cmd/freeman-cli`)
- [ ] `freeman run <collection> [--env <name>] [--folder <path>]` executing requests + test scripts, non-zero exit on failure, JSON/JUnit output. Reuses `internal/core` directly (no Wails, no HTTP).

### 7. Monitoring (largest; after CLI + testing)
- [ ] Scheduled runs of a collection/folder against an environment.
- [ ] Run history + pass/fail trend.
- [ ] Alerting hook (webhook / email).

### 8. GraphQL
- [ ] Query + variables editor; send over the existing transport.
- [ ] (Optional) schema introspection + autocomplete.

### 9. SOAP (low priority)
- [ ] Surface a raw-XML body mode with envelope scaffold.
- [ ] (Optional) WSDL import.
