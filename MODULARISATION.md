# Freeman — modules, toggles and plugins

A plan for splitting Freeman into parts a user can turn on and off, and
for whether any of them should become plugins. Written 2026-09-07
against `main` at the merge of `codegen-python-javascript`.

---

## The short answer

**Build a capability registry and config-file toggles. Do not build a
plugin runtime yet.**

Freeman is ~7,200 lines of Go across 13 packages and ~4,900 lines of
frontend. That is small enough that a plugin runtime would cost more than
it returns: process boundaries, an ABI, versioning, and a security model,
all to make optional a set of features that currently fit in one binary
without strain.

What is worth doing now, in order:

| Stage | What | Cost | Why now |
|---|---|---|---|
| 0 | Invert `core → codegen` behind an interface | half a day | Fixes a real design smell and is the pattern everything else follows |
| 1 | Capability registry + `features.yaml` + Settings toggles | 2–3 days | This is what "enable/disable" actually means for a desktop app |
| 2 | Build tags for heavy optional dependencies | 1 day, after 1 | A minimal binary, a smaller attack surface |
| 3 | Third-party extensions | weeks | Only when someone asks. See "If plugins ever happen" |

Stage 1 is the deliverable. Stages 2 and 3 are cheap and speculative
respectively.

---

## Where the seams already are

The Go import graph is already a clean layering, with one exception:

```
domain          →  (nothing)
store           →  domain
httpengine      →  domain
codegen         →  domain httpengine
core            →  codegen domain httpengine store      ← the exception
httpapi         →  core domain headercatalog theme
wailsapp        →  appdata core domain headercatalog httpengine theme

cmd/freeman        →  agentdocs httpapi wailsapp
cmd/freeman-server →  core httpapi
```

`core` importing `codegen` is the one edge pointing the wrong way. The
orchestrator knows the name of an optional feature; `core/codegen.go` is
30 lines that resolve variables and call `codegen.Generate`. Every other
optional feature would add another such edge, and after four of them
`core` is no longer separable from anything.

Three seams already exist and are worth naming, because the plan reuses
all three rather than inventing new mechanisms:

- **`$backend`** — `backend.contract.ts` asserts at compile time that
  `backend.wails.ts` and `backend.http.ts` have the same shape, and Vite
  picks one per build. Freeman already ships two capability variants of
  the frontend; the desktop one has native file dialogs and the control
  API, the container one does not.
- **Workspace YAML config** — `theme.yaml` and `headers.yaml` are read
  from the workspace, parsed in a small package, exposed through both
  `wailsapp` and `httpapi`. `features.yaml` is the same pattern again.
- **The format switch in `codegen.Generate`** — four cases, each a pure
  `(Item, vars) → string`. This is already a registry written as a
  switch.

---

## What should and shouldn't be modular

Honest verdicts. "Module" means a compile-time unit that can be excluded
and a runtime toggle; "plugin" means something a third party could write.

| Part | Module? | Plugin? | Notes |
|---|---|---|---|
| **Code generator** (`internal/codegen`, ~1,700 lines) | **Yes** | **Yes, eventually** | The strongest candidate: pure functions, no back-dependency, and an unbounded list of targets nobody will ever finish. Per-format toggles are also natural — most users want two of the four. |
| **Control API** (`cmd/freeman/main.go`, `internal/httpapi`) | **Yes** | No | Purely a matter of not starting the listener. Some users will want it off for security; see the wrinkle below. |
| **Response viewers** (pretty/raw/hex, image, XML) | Yes | **Yes** | A `contentType → renderer` registry is a clean seam and the obvious place for protobuf, msgpack, or a CSV table view. Frontend-side. |
| **Auth types** (`httpengine.applyAuth`) | Partly | Yes | Another switch that wants to be a registry. AWS SigV4, digest, NTLM are the real-world asks. Keep `none`/`bearer`/`basic` always in. |
| **Scripting** (goja, pre-request/test scripts — not built) | **Yes** | — | The heaviest dependency the roadmap commits to. Build tag from day one. |
| **Import/export** (Postman, OpenAPI, curl — not built) | Yes | **Yes** | No core coupling at all; a pure format→`domain.Item` transform. |
| **Response cache** (`wailsapp`) | Yes | No | Self-contained, and some users won't want responses on disk. |
| **Body modes** | Partly | No | GraphQL is on the roadmap and would slot in, but the editor UI for each mode is bespoke. Registry buys little. |
| **Theme, header catalog** | No | No | Already data-driven via YAML. Adding a registry on top is ceremony. |
| `domain`, `store`, `httpengine` send path | **No** | **No** | This is the application. Making it pluggable is how a small tool becomes a framework nobody wants. |
| Request editor / response pane shell | No | No | Same. |

**Sub-applications / separate processes: no.** The suggestion is
reasonable in the abstract and wrong at this size. IPC, lifecycle,
crash handling and version skew are real costs; the isolation they buy
is worth nothing while every module is first-party Go in one repo.

---

## The constraint that shapes all of this

`CLAUDE.md` states a standing rule: every interactive UI element gets a
matching `ui:action`, documented in `uiActions`, mirrored in
`GET /api/ui/state`, and exercised by `scripts/control_api/`.
`checks/consistency.py` enforces it mechanically by diffing four lists.
Today that is 55 actions, 20 routes, and 248 checks that all pass.

That property is the most valuable thing about this codebase, and a
module system is exactly the kind of change that quietly destroys it —
"the plugin registers its own actions" is how a curated, diffable
catalogue becomes an unknowable one.

**Rules a module system must follow:**

1. **A module contributes to the catalogue; it does not bypass it.** Its
   `ui:action`s and routes are declared in its registration and merged
   into `uiActions`/`apiEndpoints`. The help modal and `/api/agent` keep
   rendering one list.
2. **`consistency.py` must still work.** It reads *source*, not the
   running app. With a registry it has to learn to read the registry's
   declarations — this is real work and belongs in Stage 1, not after.
3. **A disabled module's actions are still documented**, marked
   disabled, and return a clear error rather than 404. An agent reading
   `/api/agent` should be told the Code tab exists and is off, not that
   it never existed.
4. **The suite tests two configurations, not the powerset.** Everything
   on (today's run) and a minimal build. `2^N` is not a test plan.

### The control-API wrinkle

The control API is the one module whose toggle cannot sensibly have a
`ui:action`: firing it would be the last action that listener ever
accepts. It should be `features.yaml` plus a Settings checkbox, read at
startup, and it needs an entry in `consistency.py`'s `UNDRIVEABLE` map
with exactly that reason.

### The data-safety rule

Turning a module off must never lose data. A request saved with
`auth.type: oauth2` opened while the OAuth2 module is disabled must:
keep the field on disk untouched, show it read-only in the editor, and
refuse to send with a clear message. Silently rewriting it to `none` on
the next save is the failure mode to design against.

---

## Stage 0 — invert the `codegen` dependency

One commit, and the pattern for everything after it.

```go
// internal/core
type CodeGenerator interface {
    Format() string   // "bash"
    Label() string    // "bash"
    Render(item domain.Item, vars map[string]string) (string, error)
}

func (a *App) RegisterCodeGenerator(g CodeGenerator)
```

`internal/codegen` exposes its four generators; `cmd/freeman` and
`cmd/freeman-server` register them. `core` stops importing `codegen`.
`GenerateRequestCode` looks the format up and returns a typed error for
an unknown or disabled one — which `httpapi` already knows how to
translate.

The frontend's `codeFormats` table stops being a hardcoded list and
comes from the backend, which the Code tab already re-reads on every
change. `selectCodeFormat` already validates against that table (fixed
in `a016cc2`), so it needs no change.

**Test:** an existing-behaviour test that the four formats still render,
plus one that an unregistered format is a clean error rather than a
panic.

---

## Stage 1 — capabilities

### `internal/feature`

A small package, no dependencies, following `internal/theme`'s shape:

```go
type Feature struct {
    Name        string   // "codegen", "controlApi", "responseCache"
    Title       string   // shown in Settings
    Description string   // shown in the InfoTip
    Default     bool
    RequiresRestart bool // controlApi does
}

type Set struct{ ... }
func (s *Set) Enabled(name string) bool
```

Backed by `features.yaml` in the workspace — same read/parse/default
shape as `theme.yaml`, same wiring path through `wailsapp` and
`httpapi` (see the config-file pattern the repo already follows).

### First set of toggles

Deliberately small, and each one earns its place by removing something a
user can see:

- `codegen` — the whole Code tab, plus a nested per-format list.
- `controlApi` — the `:8090` listener. Restart required.
- `responseCache` — `.cache/responses`.
- `requestOptions` — the Options tab, for people who want the simple
  client and not the TLS/cookie/redirect controls.

### Control-API surface (per the standing rule)

- `setFeatureEnabled { name, enabled }` — a new `ui:action`.
- `features` — a new block in `GET /api/ui/state`.
- `GET /api/features` — the catalogue with titles and defaults.
- `scripts/control_api/checks/features.py` — a new module driving them.
- `consistency.py` learns the registry; `controlApi`'s toggle goes in
  `UNDRIVEABLE`.

### Frontend

A `Features` tab in the Settings modal, using the disclosure-row pattern
the Collections and Environments tabs already share, with an `InfoTip`
per row carrying the description. Panels read the feature set from a
store rather than each checking a flag.

**This is also the moment to split `App.svelte`.** At 1,652 lines it is
the god component: it owns the draft, every callback, `dispatchUIAction`
and `reportUIState`. A feature registry gives a natural decomposition —
one module per panel, each exporting its actions and its slice of the
reported state — and that refactor is worth more than the toggles are.

---

## Stage 2 — build tags

Once the registry exists this is nearly free. Each optional package gets
`//go:build !minimal` on its registration file; `cmd/freeman` builds
with or without. The frontend equivalent already exists: Vite `define`
flags plus tree-shaking, the same mechanism that picks `$backend`.

Worth it for: goja (large), extra codegen targets, anything that pulls a
new module into `go.mod`. Not worth it for anything already in the
binary and small.

Add a `build-container.ps1 -Minimal` and a CI job that builds it, or the
tag will rot within a month.

---

## If plugins ever happen

Only after Stage 1 has shipped and someone has actually asked.

**Ruled out: Go's `plugin` package.** It has no Windows support at all,
and Windows is this app's primary platform. It also requires exact
toolchain and dependency matching between host and plugin. Not viable —
this is not a "we'd rather not", it is "it does not work here".

**Three that would work,** in the order I would consider them:

1. **JavaScript on goja.** The roadmap already commits to embedding goja
   for pre-request and test scripts. Once that dependency is paid for, a
   plugin API is nearly free — and the three best plugin candidates
   (codegen targets, response viewers, importers) are all string→string
   or object→object transforms that suit a scripting host exactly. No
   new runtime, no new build story, no cgo. **This is the one to pick.**
2. **WASM via wazero.** Pure Go, no cgo, works on Windows, genuinely
   sandboxed, plugins in any language. Costs a host ABI and a much less
   pleasant authoring experience. Right answer if untrusted third-party
   plugins ever matter.
3. **Subprocess + JSON-RPC over stdio.** The shape MCP uses. Strongest
   isolation, language-agnostic, and the heaviest: lifecycle, IPC schema,
   versioning, and a security model of its own.

**Whichever is chosen: plugins contribute data and transforms, not UI
components.** Letting third parties inject Svelte into the window means
bundling, sandboxing and a component API to keep stable forever. A
codegen target or a response viewer is a function; that is enough for
the cases that actually exist.

---

## Risks

- **The catalogue goes dynamic.** `uiActions`, `apiEndpoints`,
  `/api/agent` and `consistency.py` all assume a static, source-readable
  list. Making it a registry is the single largest piece of work here
  and the easiest to get wrong. Budget for it explicitly.
- **Toggle combinatorics.** Test the default set and a minimal set. Do
  not attempt the powerset, and do not let the toggle count grow without
  a reason per toggle.
- **A registry is not free.** Indirection makes code harder to read.
  Every feature that gets one should be a feature someone would plausibly
  turn off — which is why the list above is four items and not fourteen.
- **Feature flags rot.** A toggle nobody has turned off in a year is
  dead weight; delete it. A toggle that is off in nobody's build is
  untested code.

---

## The first commit

Stage 0, on its own branch:

1. `CodeGenerator` interface in `internal/core`, plus a registry on
   `core.App`.
2. `internal/codegen` exports its four generators; both `cmd/` binaries
   register them.
3. `core` no longer imports `codegen` — assert it in a test.
4. `codeFormats` comes from the backend rather than a hardcoded frontend
   table; `GET /api/ui/state` reports the registered set.
5. Suite unchanged and still green at 248.

It is a day's work, it improves the code whether or not any of the rest
happens, and it is the shape every later module follows.
