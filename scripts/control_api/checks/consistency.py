"""Makes CLAUDE.md's standing rule executable instead of remembered.

A new interactive element is supposed to land in four places at once: a
`case` in App.svelte's `dispatchUIAction`, a row in lib/controlApiCatalog.ts's
`uiActions` table, a mirrored field in `reportUIState`, and coverage in
this suite.
That held by hand for a long time, but "held by hand" is exactly the kind
of thing that quietly stops holding — a code review found two documented
actions the suite had never driven.

So this section reads the source rather than the running app: it diffs
the help table against the dispatcher, the dispatcher against what these
checks actually drive, and the documented `apiEndpoints` against the
routes registered in Go. It needs no ControlAPI and no open workspace,
which also makes it the one part of the suite that can run in CI.
"""

from __future__ import annotations

import ast
import re
from pathlib import Path

from .. import Report

# Actions this suite deliberately never fires, with the reason. Both
# hand control to another program on the user's desktop — a test run
# shouldn't scatter editor and file-manager windows across the screen.
# Anything else missing from the suite is a gap, not a decision.
UNDRIVEABLE = {
    "openResponseCacheExternally": "launches the OS-associated application",
    "openResponseCacheInFileExplorer": "launches the OS file manager",
    "minimizeWindow": "hides the window this run is being watched in, and only the taskbar brings it back",
    "closeWindow": "quits the app, taking the control API and the rest of the run with it",
}


def _read(root: Path, rel: str) -> str:
    return (root / rel).read_text(encoding="utf-8")


def _documented_actions(app_svelte: str) -> set[str]:
    """Action names from the `uiActions` catalogue (lib/controlApiCatalog.ts)."""
    table = _slice(app_svelte, "uiActions: UiAction[] = [", "\n]")
    return set(re.findall(r"action:\s*'([A-Za-z]+)'", table))


def _dispatched_actions(app_svelte: str) -> set[str]:
    """Case labels from dispatchUIAction's own switch.

    Anchored on the six-space indent of that switch's cases so the nested
    `switch (payload?.field)` inside setRequestField — whose cases are
    field names, not actions — doesn't get counted.
    """
    body = _slice(app_svelte, "async function dispatchUIAction", "\n  }\n")
    return set(re.findall(r"^      case '([A-Za-z]+)':", body, re.M))


def _driven_actions(checks_dir: Path) -> set[str]:
    """Actions any sibling check module actually fires."""
    driven: set[str] = set()
    for path in sorted(checks_dir.glob("*.py")):
        if path.name == Path(__file__).name:
            continue
        driven |= set(re.findall(r'\.action\(\s*"([A-Za-z]+)"', path.read_text(encoding="utf-8")))
    return driven


# The payload column read as a schema, the same reading Go applies at
# runtime (agentdocs.requiredKeys): a bare name is required, a trailing ?
# is optional, "|" separates alternatives, "field: 'name'" documents a
# key and the value it carries (only the key is checked), and no braces
# at all means nothing is required. Keep the two readings in step — they
# are what decides whether a call this suite makes is one the app will
# accept.
def _required_keys(doc: str) -> list[list[str]]:
    if "{" not in doc:
        return []
    alternatives = []
    for group in doc.split("|"):
        group = group.strip().lstrip("{").rstrip("}")
        required = []
        for field in group.split(","):
            field = field.strip()
            if not field or field.endswith("?"):
                continue
            required.append(field.split(":")[0].strip())
        alternatives.append([f for f in required if f])
    return alternatives


def _documented_payloads(app_svelte: str) -> dict:
    """Each action's payload column, from the `uiActions` catalogue.

    Paired entry by entry rather than by zipping two findall lists: a row
    whose payload contains an apostrophe is written with double quotes
    ("{ field: 'name', value }"), so a single-quote-only pattern returned
    two lists of different lengths — and this returning nothing made the
    check that uses it pass on everything.
    """
    table = _slice(app_svelte, "uiActions: UiAction[] = [", "\n]")
    found = {}
    for entry in re.finditer(r"""action:\s*['"]([A-Za-z]+)['"](.*?)(?=action:\s*['"]|\Z)""", table, re.S):
        payload = re.search(r"""payload:\s*(?:'([^']*)'|"([^"]*)")""", entry.group(2))
        if payload:
            found[entry.group(1)] = payload.group(1) if payload.group(1) is not None else payload.group(2)
    return found


def _fired_calls(checks_dir: Path) -> list[tuple[str, str, int, set, bool]]:
    """Every api.action(...) this suite makes, as
    (action, module, line, literal payload keys, whether they're literal).

    Read as a syntax tree rather than by regex because the interesting
    part is the payload's *keys*, and a dict literal spanning lines is
    not something a regex reads honestly. A payload that isn't a literal
    dict — a variable, a comprehension — is reported as unreadable and
    skipped rather than guessed at.
    """
    calls = []
    for path in sorted(checks_dir.glob("*.py")):
        if path.name == Path(__file__).name:
            continue
        tree = ast.parse(path.read_text(encoding="utf-8"))
        for node in ast.walk(tree):
            if not isinstance(node, ast.Call) or not isinstance(node.func, ast.Attribute):
                continue
            if node.func.attr != "action" or not node.args:
                continue
            name = node.args[0]
            if not isinstance(name, ast.Constant) or not isinstance(name.value, str):
                continue
            keys, literal = set(), True
            if len(node.args) > 1:
                payload = node.args[1]
                if isinstance(payload, ast.Dict):
                    for k in payload.keys:
                        if isinstance(k, ast.Constant) and isinstance(k.value, str):
                            keys.add(k.value)
                        else:
                            literal = False
                elif not (isinstance(payload, ast.Constant) and payload.value is None):
                    literal = False
            calls.append((name.value, path.name, node.lineno, keys, literal))
    return calls


def _documented_routes(app_svelte: str) -> set[str]:
    """"<METHOD> <path>" pairs from the `apiEndpoints` catalogue."""
    table = _slice(app_svelte, "apiEndpoints: ApiEndpoint[] = [", "\n]")
    methods = re.findall(r"method:\s*'([A-Z]+)'", table)
    paths = re.findall(r"path:\s*'([^']+)'", table)
    if len(methods) != len(paths):
        return set()
    return {f"{m} {p}" for m, p in zip(methods, paths)}


def _registered_routes(*go_sources: str) -> set[str]:
    """"<METHOD> <path>" pairs from Go's mux.HandleFunc registrations."""
    routes: set[str] = set()
    for src in go_sources:
        routes |= set(
            f"{m} {p}" for m, p in re.findall(r'mux\.Handle(?:Func)?\("([A-Z]+) (/api/[^"]*)"', src)
        )
    return routes


def _slice(text: str, start: str, end: str) -> str:
    i = text.find(start)
    if i < 0:
        return ""
    j = text.find(end, i)
    return text[i : j if j > 0 else len(text)]


def test_consistency(r: Report, repo_root: Path) -> None:
    # Printed text stays inside cp1252: a Windows console dies on
    # anything outside it, and a crash in the reporter would be a
    # spectacularly silly way for a consistency check to fail.
    r.section("Control-API consistency (help table vs dispatcher vs this suite vs Go routes)")

    # The two tables live in lib/controlApiCatalog.ts, which the help
    # modal renders and App.svelte reports to Go for GET /api/agent; the
    # dispatcher stays in App.svelte with the state it mutates.
    help_modal = _read(repo_root, "cmd/freeman/frontend/src/lib/controlApiCatalog.ts")
    app_svelte = _read(repo_root, "cmd/freeman/frontend/src/App.svelte")
    documented = _documented_actions(help_modal)
    dispatched = _dispatched_actions(app_svelte)
    driven = _driven_actions(Path(__file__).parent)

    r.step("parsing controlApiCatalog.ts's tables, App.svelte's dispatcher, and this package's checks")

    # A parse that quietly returns nothing would make every diff below
    # look clean, so prove the parser still found the tables first.
    if len(documented) < 20 or len(dispatched) < 20:
        r.check(
            "the parsers in consistency.py still find the uiActions table and the dispatcher",
            False,
            f"documented={len(documented)} dispatched={len(dispatched)} — the source moved or changed shape, update consistency.py",
        )
        return
    r.check(
        f"parsed {len(documented)} documented actions and {len(dispatched)} dispatcher cases",
        True,
    )

    missing_case = sorted(documented - dispatched)
    r.check(
        "every action in the help modal has a dispatchUIAction case",
        not missing_case,
        f"documented with no case: {missing_case}",
    )

    undocumented = sorted(dispatched - documented)
    r.check(
        "every dispatchUIAction case is listed in the help modal",
        not undocumented,
        f"handled but undocumented: {undocumented}",
    )

    should_drive = (documented | dispatched) - set(UNDRIVEABLE)
    never_driven = sorted(should_drive - driven)
    r.check(
        f"every driveable action is exercised somewhere in this suite ({len(should_drive)} of them)",
        not never_driven,
        f"documented but never fired by a check: {never_driven}",
    )

    # Every call this suite makes has to satisfy the payload column it is
    # documented with — the same reading POST /api/ui/action applies at
    # runtime (see agentdocs.Catalog.Validate), done here against the
    # source so a wrong call is a failed check rather than a 400 partway
    # through a two-minute run.
    #
    # This exists because such a call went unnoticed for the life of the
    # suite: setEnvironmentVariable was driven by key, which that action
    # does not accept — key is a field it writes, not a way to choose a
    # row — so it did nothing at all, and poll() returns its last reading
    # whether or not the predicate ever came true.
    payloads = _documented_payloads(help_modal)
    # A parser that finds nothing would make the check below pass on
    # everything, which is exactly how it failed the first time. Prove it
    # read the column before trusting what it says about it.
    if len(payloads) != len(documented):
        r.check(
            "consistency.py reads a payload column for every documented action",
            False,
            f"{len(payloads)} payloads for {len(documented)} actions — the table changed shape, update _documented_payloads",
        )
        return

    bad, unreadable = [], []
    for action, module, line, keys, literal in _fired_calls(Path(__file__).parent):
        doc = payloads.get(action)
        if doc is None:
            continue  # an undocumented action is the check above's business
        alternatives = _required_keys(doc)
        if not alternatives:
            continue
        if not literal:
            unreadable.append(f"{module}:{line} {action}")
            continue
        if not any(all(k in keys for k in required) for required in alternatives):
            bad.append(f"{module}:{line} {action} sent {sorted(keys) or 'no payload'}, needs {doc}")
    r.check(
        "every action this suite fires carries the payload it is documented to need",
        not bad,
        "; ".join(bad),
    )
    # Not a failure: a payload built at run time can't be read from
    # source. Named so the gap in this check is visible rather than
    # implied.
    if unreadable:
        r.check(f"({len(unreadable)} call(s) build their payload at run time, unchecked here)", True)

    # The allow-list is only allowed to name real actions — otherwise a
    # renamed action would silently stay exempt forever.
    stale_exemptions = sorted(set(UNDRIVEABLE) - documented)
    r.check(
        "consistency.py's UNDRIVEABLE list has no stale entries",
        not stale_exemptions,
        f"exempted but no longer an action: {stale_exemptions}",
    )

    # --- apiEndpoints vs the routes Go actually registers ---
    documented_routes = _documented_routes(help_modal)
    registered = _registered_routes(
        _read(repo_root, "internal/httpapi/handler.go"),
        _read(repo_root, "cmd/freeman/main.go"),
    )
    if len(documented_routes) < 5 or len(registered) < 5:
        r.check(
            "the route parsers in consistency.py still find both tables",
            False,
            f"documented={len(documented_routes)} registered={len(registered)} — update consistency.py",
        )
        return

    phantom = sorted(documented_routes - registered)
    r.check(
        f"every route in the help modal is registered in Go ({len(documented_routes)} of them)",
        not phantom,
        f"documented but not served: {phantom}",
    )

    undocumented_routes = sorted(registered - documented_routes)
    r.check(
        "every registered /api route is listed in the help modal",
        not undocumented_routes,
        f"served but undocumented: {undocumented_routes}",
    )
