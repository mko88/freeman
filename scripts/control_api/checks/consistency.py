"""Makes CLAUDE.md's standing rule executable instead of remembered.

A new interactive element is supposed to land in four places at once: a
`case` in App.svelte's `dispatchUIAction`, a row in its `uiActions` help
table, a mirrored field in `reportUIState`, and coverage in this suite.
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
}


def _read(root: Path, rel: str) -> str:
    return (root / rel).read_text(encoding="utf-8")


def _documented_actions(app_svelte: str) -> set[str]:
    """Action names from the `uiActions` help table."""
    table = _slice(app_svelte, "const uiActions = [", "\n  ]")
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


def _documented_routes(app_svelte: str) -> set[str]:
    """"<METHOD> <path>" pairs from the `apiEndpoints` help table."""
    table = _slice(app_svelte, "const apiEndpoints = [", "\n  ]")
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

    app_svelte = _read(repo_root, "cmd/freeman/frontend/src/App.svelte")
    documented = _documented_actions(app_svelte)
    dispatched = _dispatched_actions(app_svelte)
    driven = _driven_actions(Path(__file__).parent)

    r.step("parsing App.svelte's uiActions / dispatchUIAction and this package's checks")

    # A parse that quietly returns nothing would make every diff below
    # look clean, so prove the parser still found the tables first.
    if len(documented) < 20 or len(dispatched) < 20:
        r.check(
            "the parsers in consistency.py still find the uiActions/dispatchUIAction tables",
            False,
            f"documented={len(documented)} dispatched={len(dispatched)} — App.svelte's shape changed, update consistency.py",
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

    # The allow-list is only allowed to name real actions — otherwise a
    # renamed action would silently stay exempt forever.
    stale_exemptions = sorted(set(UNDRIVEABLE) - documented)
    r.check(
        "consistency.py's UNDRIVEABLE list has no stale entries",
        not stale_exemptions,
        f"exempted but no longer an action: {stale_exemptions}",
    )

    # --- apiEndpoints vs the routes Go actually registers ---
    documented_routes = _documented_routes(app_svelte)
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
