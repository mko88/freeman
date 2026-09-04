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
| SOAP API support | ⬜ | Needs raw-XML body mode surfaced + optional WSDL import (→ `internal/importer`). Low priority. |
| Request chaining | ⬜ | Domain already has `Item.PreRequestScript` / `Item.TestScript`; `internal/script.Engine` interface exists, only `NoopEngine` wired. Needs: response→variable extraction, then feed vars forward. |
| Random data | ⬜ | Extend `httpengine.Substitute` (or a pre-pass) with dynamic tokens: `{{$guid}}`, `{{$timestamp}}`, `{{$randomInt}}`, faker-style. |
| API testing | ⬜ | `Item.TestScript` present but not executed. Needs a real `script.Engine` (goja) + a test-results pane. |
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
- [ ] Delete a saved collection/environment — same gap, not yet done for these two.
- [ ] Params tab in the request editor (domain `Item.Params` + executor already support query params; UI doesn't expose them).
- [ ] `form-data` and `x-www-form-urlencoded` body modes (`domain.BodyMode` constants reserved; `httpengine.Execute` only handles `raw`).
- [ ] Auth tab: Bearer / Basic helpers that just write the `Authorization` header.
- [ ] Response niceties: pretty-print/format JSON, show response headers, copy-as-curl.

### 2. Nested folders
- [ ] Sidebar tree UI (`domain.Item` is already a folder/request union with `Items []Item`; `Collection.UpsertItem`/`FindItem` walk the tree). Currently the sidebar renders a flat list.

### 3. Scripting engine → unlocks chaining + testing
- [ ] Replace `script.NoopEngine` with a `goja`-backed `script.Engine`.
- [ ] Wire `Engine.RunPreRequest` / `RunTest` into `core.ExecuteRequest` (call sites already shaped for it).
- [ ] Minimal `pm`-style API: `pm.response`, `pm.environment.set`, `pm.test(...)`, assertions.
- [ ] Test-results pane in the response area.
- [ ] **Request chaining** then falls out: a test/pre-request script sets a variable from a prior response.

### 4. Random / dynamic data
- [ ] Dynamic `{{$...}}` tokens resolved before/inside `Substitute`.

### 5. Importers (`internal/importer` — interface exists, no impls)
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
- [ ] (Optional) WSDL import via `internal/importer`.
