# Freeman — modules, toggles and plugins

A plan for splitting Freeman into parts a user can turn on and off, and
for whether any of them should become plugins. Written 2026-09-07
against `main` at the merge of `codegen-python-javascript`.

**Goal, clarified after the first draft:** the target is third-party
plugins that users download and install separately — not just built-in
features with a checkbox. That moves the centre of gravity from Stage 1
to Stage 3.

**Read it in this order.** The two sections at the end were written
after the goal was clarified and carry the actual argument; everything
before them is the survey they rest on.

1. *Deciding the core* (last section) — what the host is, what a plugin
   is given, and what it may ask for. The design that has to come first,
   because everything core exposes becomes API.
2. *Worked example* (second to last) — what one plugin point would
   actually cost, measured against the code.
3. The rest, for the import graph, the seams that already exist, and the
   staged plan for plain enable/disable, which is still worth doing on
   its own.

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
| 3 | Third-party downloadable plugins | ~12–14 days for the first one | The stated goal. See the worked example at the end |

Stages 0–2 are worth doing on their own merits. Stage 3 is the real
target, and the worked example at the end costs it properly: about two
of those days are the plugin mechanism and nine are platform, which
changes what the decision is actually about.

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
Today that is 55 actions, 20 routes, and 249 checks that all pass.

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

## Choosing the plugin mechanism

Costed properly in the worked example below; this is the shortlist and
what is ruled out.

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

---

## Worked example: what would a downloadable codegen plugin cost?

Costed against the real code, because the answer is not what the shape
of the problem suggests.

### Codegen is the easy case, and it is easier than it looks

`internal/codegen/codegen.go` is 978 lines. The four renderers are 400
of them:

| Renderer | Lines |
|---|---|
| `renderBash` | 81 |
| `renderPowerShell` | 101 |
| `renderPython` | 114 |
| `renderJavaScript` | 104 |

The other ~578 lines are shared and **stay in the host**: `build()`
resolving `{{vars}}`, folding query params into the URL, turning `Auth`
into a header, normalising the five body modes, deriving the effective
timeout, and the per-language quoting helpers.

So a plugin author writes ~100 lines, not ~1,000. And the contract they
write against is small: **19 fields** across `request`, `reqBody` and
`oauthGrant` — method, url, headers, the body in normalised form, and
the transport options. In, a struct. Out, a string.

No I/O. No callbacks into the host. No state between calls. No UI. Of
everything in this app, this is the most plugin-shaped thing there is —
which is exactly why it is the right one to cost, and why the number
below is a *floor* for any other plugin point.

### The mechanism is the cheap part

| Mechanism | Host code | Author needs | Per-call cost | Verdict |
|---|---|---|---|---|
| **Subprocess + JSON on stdio** | ~250 lines | any language | 20–60 ms process spawn on Windows, plus interpreter start | Cheapest to build, worst to live with — see below |
| **JavaScript on goja** | ~500 lines | JS, nothing installed | microseconds | **Recommended** |
| **WASM on wazero** | ~800 lines | Rust/TinyGo/AssemblyScript toolchain | microseconds | Only if untrusted plugins must be sandboxed |

**The subprocess option is killed by a detail in the frontend.**
`App.svelte`'s `codeKey` reactive regenerates the snippet on *every
keystroke* while the Code tab is open — there is no debounce, and
`regenerateCode` carries a sequence guard precisely because calls
overlap. In-process that is free. A process spawn per keystroke is
40–100 ms of lag on every character typed into the URL bar. It could be
fixed with debouncing or a long-lived worker process, but then the
"cheapest to build" advantage is gone.

goja wins on three counts: it is in-process so the keystroke path stays
free, the author needs nothing installed, and the roadmap already
commits to embedding it for pre-request and test scripts (`TODO.md`).
Paid once, used twice.

### The costs that are not the mechanism

This is the part that decides the answer.

| Work | Estimate | Why |
|---|---|---|
| Stage 0 — invert `core → codegen` behind an interface | 1 day | Prerequisite either way |
| Dynamic format list, end to end | 2 days | `codeFormats` is a hardcoded TS array feeding both the tab row and `selectCodeFormat`'s validation. It has to come from the backend, and reach `GET /api/ui/state`, the help modal and `/api/agent` |
| `consistency.py` rework | 1–2 days | It reads *source* to diff the catalogue. With plugins, the action stays static but its accepted payload values become dynamic — it needs a rule for "static action, runtime-reported values" |
| Contract versioning + author docs | 1 day, then ongoing | The 19 fields become public API. `cookies` was added to them this week; that freedom ends |
| Failure isolation | 1 day | Timeout, panic recovery, error surfaced in the tab. A plugin that hangs must not hang a path that runs per keystroke |
| Discovery, manifest, first-run consent | 2 days | A plugin is code from the internet running with the user's privileges |
| Tests: host, plus a fixture plugin in the suite | 2 days | The load path needs to be a regression check, not a demo |
| The goja host itself | 2 days | Marshalling, the call, the sandbox |
| **Total** | **≈ 12–14 days** | |

### The number that matters

**The plugin mechanism is 2 of those days. About 9 are platform.**

Discovery, consent, versioning, failure isolation, the dynamic
catalogue and the `consistency.py` rework are paid once and are not
about code generation at all. Once they exist, a *second* plugin point —
response viewers, importers — costs roughly 2–3 days each.

That inverts the usual reasoning:

- **Making codegen pluggable, alone, is poor value.** Twelve days to let
  someone add a Ruby snippet, when adding one in-tree is a 100-line pull
  request against a file that already has four examples in it.
- **Making codegen the first of three or four plugin points is good
  value.** The platform cost amortises, and codegen is the right one to
  build it against because it is the simplest possible consumer.

So the decision is not "should codegen be a plugin". It is "is Freeman
becoming a platform". If yes, start with codegen. If no, keep taking
pull requests.

### The one thing that cannot be bought cheaply

**Syntax highlighting for a language Freeman does not already bundle.**

`CodeEditor.svelte` maps a format to a CodeMirror grammar from
`@codemirror/legacy-modes`, chosen at build time and bundled into the
Wails binary. A plugin declaring `format: "ruby"` gets no grammar.
Loading one at runtime means pulling an npm package into an embedded,
offline asset bundle — not viable.

The honest options are: plugins pick a `language` from the bundled set
(currently shell, powershell, python, javascript, json, xml), or their
output renders as plain text. Neither is terrible. It just needs to be a
documented limit of the plugin API from day one rather than a surprise
for the first author who picks Ruby.

### What I would cut

- **Don't convert the built-in four to plugins.** They stay compiled:
  they are the reference implementation, they are pinned byte-for-byte
  by the Go tests and the control-API suite, and running them through an
  interpreter buys nothing.
- **Don't let plugins ship UI.** A transform is enough for every case
  that actually exists, and a component API is forever.
- **Don't build a registry or a marketplace.** A folder, a manifest, and
  "unzip it here" is enough until there are ten plugins.

### If this goes ahead, the order is

1. Stage 0 — the `CodeGenerator` interface. Useful alone.
2. The dynamic format list end to end, with the built-in four still
   compiled in. Nothing is pluggable yet; the *catalogue* becomes
   dynamic, which is the hard half.
3. `consistency.py` learns dynamic payload values.
4. Only then: the goja host, discovery, consent, and a fixture plugin in
   the suite.

Steps 2 and 3 are the risky ones, they are where the standing
control-API guarantee is either preserved or quietly lost, and they are
worth doing on their own even if step 4 never happens.

---

## Deciding the core: what plugins are given, and what they may ask for

The previous section costs one plugin point. This one is the design that
has to come first, because everything core exposes becomes API that
cannot change freely afterwards.

### What "core" means

Core is not "the important bits". **Core is whatever the plugin contract
is written in terms of** — the moment a plugin can see a type, that type
is frozen for a major version.

| In core | Why |
|---|---|
| `domain` | The data model. Everything is expressed in it |
| `store` | The workspace is the product: plain JSON on disk |
| `httpengine` | Building and sending. The one thing that must never be extensible |
| `core` | Workspace, collection and environment lifecycle |
| Editor + response pane shell | The frame extensions hang off |
| **The control API** | The app's defining property is being drivable headlessly. A plugin system that isn't drivable breaks it |

| Not core | Becomes an extension point |
|---|---|
| Code generation | The worked example above |
| Response viewers and body formatters | `contentType → renderer` |
| Auth schemes above a floor (`none`/`bearer`/`basic`) | SigV4, digest, NTLM |
| Import/export | Postman, OpenAPI, curl |
| Scripting | Its own thing; overlaps the roadmap |

### Four kinds of extension point, and only one of them is cheap

The contract follows from the kind, so the kinds have to be named before
the contract can be designed.

| Kind | Examples | Contract | Cost |
|---|---|---|---|
| **Transformer** (pure) | codegen, importer, exporter, response viewer | value in → value out. No state, no side effects | **Low** |
| **Request participant** | auth scheme, pre-request hook | May mutate the outgoing request; ordered; may fail the send | Medium |
| **Observer** | logging, metrics, run history | Fire and forget; may never block or fail a send | Low–medium |
| **UI contributor** | a new tab, a new pane | A component API, kept stable forever | **High — don't** |

**v1 should be transformers only.** Everything actually being asked for
— codegen first, viewers and importers next — is a transformer. The
other three kinds each need their own design pass, and none of them is
needed to prove the idea.

### The contract is a projection of the domain, not the domain

The single most important decision here, and the codebase has already
made it once by accident.

`internal/codegen` does not hand its renderers a `domain.Item`. It builds
an internal `request` struct — its own comment calls it the "neutral
intermediate form" — with `{{vars}}` already substituted, query params
already folded into the URL, `Auth` already collapsed into a header, and
the five body modes already normalised into one shape. That is 19 fields,
and it is why a renderer is ~100 lines rather than ~400.

**Publish that, versioned, as the plugin contract. Never publish
`domain`.** Two reasons:

1. `domain` stays free to change. `Options.StoreCookies` was added to it
   this week; that freedom ends the day a plugin can read it.
2. Plugins get less to do and fewer ways to be wrong. Substitution, auth
   folding and body normalisation stay in one place, tested once.

**Two projections, not one**, because different kinds want opposite
things:

| Projection | Given to | Contains |
|---|---|---|
| `ResolvedRequest` | codegen, request participants | What is about to go on the wire: variables substituted, auth as headers, body normalised |
| `SavedRequest` | exporters, importers | What is on disk: `{{vars}}` intact, folders, descriptions, scripts |

An exporter that received the resolved form would bake secrets into a
Postman file. That is a bug the type system should make impossible, not
a warning in the docs.

### Push, don't pull

The natural question — "how does a plugin ask the app for state?" —
mostly answers itself: it shouldn't have to, because the host puts what
it needs into the call.

| What a plugin might want | How it arrives |
|---|---|
| The request being rendered | **Push** |
| Environment variables, raw and resolved | **Push** — it's a small table |
| The collection being exported | **Push** |
| The response being viewed: bytes, content type, headers, status, timing | **Push** |
| Its own settings | **Push** |
| The palette, for a viewer that wants to match | **Push** — tiny |
| Host version, API version, platform | **Push** |
| The *previous* response for this request | Pull — unbounded, rarely wanted |
| Some other collection's contents | Pull — and needs permission |

Almost everything survives the push test, and push is better on four
counts that matter here:

- **Deterministic.** Same input, same output. A plugin becomes testable
  against a JSON fixture with no host running at all — which is how the
  suite would test its fixture plugin.
- **No re-entrancy.** Codegen runs on every keystroke. A plugin calling
  back into the host, while the host is inside a render, is a deadlock
  waiting for a slow day.
- **No permission model needed** for the common case. Nothing is
  reachable that wasn't handed over.
- **Cacheable.** Same input means the host can skip the call entirely.

So **v1's host API is one function: `log`.** Possibly a second,
`readAsset`, scoped to the plugin's own folder, for template files. Pull
access to app state waits until something concrete needs it — and when it
does, it arrives as a declared permission in the manifest, shown at
install time.

### Secrets: the part that cannot be sandboxed away

A codegen plugin is handed a **resolved** request. The bearer token is
sitting in the headers, because rendering it is the entire job.

No sandbox changes that. A WASM plugin with no I/O still sees the token;
it just can't phone home with it — which is worth something, but it is
not "plugins can't see your secrets".

Two things follow:

1. **The projection marks provenance.** Fields whose value came from a
   `secret: true` environment variable are flagged, so a plugin can choose
   to emit `$API_TOKEN` instead of the literal — and so the host can say,
   at install time, *"this plugin will see the resolved value of:
   apiToken, clientSecret"* rather than a vague warning.
2. **Consent names the disclosure, not the sandbox.** "This plugin runs
   code on your machine and can read the secrets of any request you
   generate code for" is the honest sentence. Anything softer is
   marketing.

Exporters get `SavedRequest` and never see resolved values at all, which
is the other half of why there are two projections.

### Plugins and each other

**In v1: they don't see each other.** Composition happens in the host.
Inter-plugin calls bring load order, dependency cycles, version matching
between plugins, and a failure mode where plugin A breaks because plugin
B updated — for a benefit nobody has asked for yet.

Two cheap decisions now keep the door open:

- **Every extension is addressable by a stable id**: `codegen:bash`,
  `viewer:application/json`, `import:postman`. Namespaced by kind, so ids
  can't collide across kinds.
- **The manifest accepts `requires: []`** from day one — validated
  against installed ids, and otherwise unused.

If lookup ever lands, it is `host.invoke("codegen:bash", req)`: mediated
by the host, permissioned in the manifest, and cycle-checked at load.
Plugins still never hold a reference to each other.

### Versioning the contract

- The manifest declares `apiVersion: 1`. The host refuses a higher major
  and warns on an unknown minor.
- Within a major: add fields freely, never rename or remove one.
- **The projection schema is committed to the repo** and a Go test
  asserts the projection still matches it. A change to the plugin
  contract then shows up as a diff in review — the same mechanical
  enforcement `consistency.py` already gives the control API, which is
  the house style for exactly this kind of promise.

### What core must guarantee in return

1. **A plugin cannot break the send path.** Transformers are not on it.
   That is most of why v1 is transformers only.
2. **Timeout and panic recovery per call**, with the failure surfaced
   where the output would have been. A hanging plugin must not hang a
   path that runs per keystroke.
3. **A failed plugin is reported as present-but-broken**, never as a
   missing feature. Same rule as a disabled module.
4. **Everything a plugin adds still appears in `/api/agent` and the help
   catalogue.** No exceptions — this is the standing rule, and plugins
   are exactly where it would be tempting to make one.

### The minimal v1, stated as a target

- One kind: transformer.
- **Two extension points, not one**: `codegen` and `viewer`. Designing a
  plugin API against a single consumer produces an API shaped like that
  consumer; the second point is what proves the contract generalises, and
  it is cheap once the platform exists.
- Push-only. Host API: `log`.
- Two projections: `ResolvedRequest`, `SavedRequest`. Neither is `domain`.
- No inter-plugin visibility. Stable ids and an unused `requires` field.
- A folder and a manifest. No registry, no marketplace.
