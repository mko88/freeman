#!/usr/bin/env python3
"""Exercise every documented Freeman control-API action end to end.

Freeman's desktop build exposes a loopback HTTP "control API" (see
CLAUDE.md's standing rule: every interactive UI element gets a matching
`ui:action`). This script drives all of it — the read-only /api/* routes
and every action in App.svelte's `uiActions` table — and checks the
result on disk / over HTTP after each step, the same way a human would
click around and then look at what got saved.

It leaves no trace by running entirely inside a disposable workspace:
the very first thing main() does is point the running app at a
brand-new OS temp directory (openWorkspace), and the very last thing it
does — in a `finally`, so this happens no matter how the run ends — is
switch back to whatever workspace was open before and delete that temp
directory. Run it back-to-back as many times as you like. The one
exception is scripts/random-sample.bin, a committed random-bytes fixture
the file-upload test only ever reads — reused across runs, not
regenerated or deleted. (The response cache lives under the workspace,
at .cache/responses, so test_response_cache clearing it only touches
the disposable temp workspace, which is deleted anyway.) Switching to a
brand-new, empty workspace like
this also happens to be full regression coverage for a real bug: a
freshly auto-created collection's Items round-tripping as JSON null
instead of [] (see the check for it early in main(), and
internal/core.TestNewCollectionHasEmptyNotNilItems for the same thing at
the Go level).

Every test that needs a saved request follows the same three-phase
shape, in three separate passes rather than interleaved per test (see
REQUEST_TEST_NAMES): first every one of them is created — empty, just a
name — and saved; then, once every request exists, each test in turn
selects its own, populates whatever fields it needs, and executes it if
that's what it's testing; only once every test has run are all of those
requests deleted, one by one. Nothing is shared between tests and
nothing needs precise leftover-tracking across runs — the whole temp
workspace this run created them in is discarded regardless — but every
test still gets its own real saveRequest/deleteRequest round trip
against a real, distinct item.

Two sections aren't about a UI action at all. test_api_guard asserts the
API refuses the request shapes a web page can send cross-origin (see
internal/httpapi/guard.go); it sits early, right after the read-only
routes, because everything after it depends on the guard *not* getting
in a plain script's way. test_consistency reads source files rather than
the running app, diffing App.svelte's `uiActions` help table against
`dispatchUIAction`, against what these checks actually drive, and
`apiEndpoints` against the routes Go registers — so the four-places-at-
once rule below fails loudly instead of quietly rotting. It runs first,
and `--consistency-only` runs just it, with no app needed.

Layout: this file is the entry point — argument parsing and the
three-phase run order — and everything else lives in scripts/control_api/
(the HTTP client, the reporter and poll(), shared fixtures, and one
module per group of checks under checks/). It was one 1,700-line file
until that stopped being readable.

KEEP THE CHECKS IN SYNC with App.svelte's `uiActions`/`apiEndpoints`
tables — when an action's payload shape changes, or a new one is added,
update the matching module under scripts/control_api/checks/ (see
CLAUDE.md). test_consistency will tell you if you forget.

Usage:
    py scripts/test_control_api.py                    # ~0.6s between actions, watch it run
    py scripts/test_control_api.py --delay 1.5         # slower, easier to follow on screen
    py scripts/test_control_api.py --delay 0           # no pauses, fast/CI-style
    py scripts/test_control_api.py --consistency-only  # source-level checks only, no running app
    py scripts/test_control_api.py --base-url http://127.0.0.1:9090
    py scripts/test_control_api.py --skip-rapid-fire   # skip the ordering regression check
    py scripts/test_control_api.py --pause-before-revert  # hold on the temp workspace so you can look at it yourself

Requires Python 3.8+, standard library only — no pip install needed.
Freeman must already be running (desktop build) with a workspace open
(at least one collection and one environment).
"""

from __future__ import annotations

import argparse
import os
import shutil
import sys
import tempfile
from pathlib import Path
from typing import Optional

from control_api import ApiError, ControlAPI, Report, poll
from control_api.fixtures import REQUEST_TEST_NAMES, find_item, find_item_by_id
from control_api.checks.chrome import test_help_modal, test_layout_comfort
from control_api.checks.codegen import test_code_tab
from control_api.checks.consistency import test_consistency
from control_api.checks.environments import test_environment_editor
from control_api.checks.execute import test_execute, test_file_upload, test_http_methods
from control_api.checks.guard import test_api_guard
from control_api.checks.regression import test_rapid_fire_regression
from control_api.checks.request_editor import test_delete_request, test_request_editor
from control_api.checks.responses import (
    test_large_response_truncation,
    test_response_cache,
    test_ui_state_getter,
)
from control_api.checks.workspace import (
    test_read_only_routes,
    test_settings_window,
    test_workspace_navigation,
)

REPO_ROOT = Path(__file__).resolve().parent.parent


def main() -> int:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--base-url", default="http://127.0.0.1:8090", help="control API base URL")
    parser.add_argument(
        "--delay", type=float, default=0.6, help="seconds to pause after each action, so you can watch it (default: 0.6, use 0 to run fast)"
    )
    parser.add_argument("--skip-rapid-fire", action="store_true", help="skip the ordering regression check")
    parser.add_argument(
        "--consistency-only",
        action="store_true",
        help="run only the source-level consistency section (needs no running app — the one part of this suite CI can run)",
    )
    parser.add_argument(
        "--pause-before-revert",
        action="store_true",
        help="once every check has run, pause with the app pointed at the temp workspace so you can look at it yourself, before switching back to your real one",
    )
    parser.add_argument("-v", "--verbose", action="store_true", help="print every HTTP call")
    parser.add_argument("--no-color", action="store_true", help="disable ANSI colors")
    args = parser.parse_args()

    use_color = not args.no_color and sys.stdout.isatty() and not os.environ.get("NO_COLOR")
    api = ControlAPI(args.base_url, args.delay, args.verbose)
    r = Report(use_color)

    # Reads source files, not the running app — so it goes first (a
    # mismatch here is a bug in the change you just made, worth hearing
    # about before a two-minute end-to-end run) and can stand alone.
    test_consistency(r, REPO_ROOT)
    if args.consistency_only:
        return r.summary()

    print(f"\nFreeman control API test — base URL {args.base_url}, delay {args.delay}s")

    collection_id: Optional[str] = None
    environment_id: Optional[str] = None
    item_ids: dict[str, str] = {}
    exit_code = 0

    # Everything below runs inside a disposable OS temp directory, not
    # your real workspace/ — switched to right here, before any other
    # check, and switched back in the very last `finally`, however the
    # run ends (success, a failed check, an exception, Ctrl-C). This is
    # also full end-to-end regression coverage for a real bug: a
    # brand-new collection's Items round-tripping as JSON null instead
    # of [] — see the check for it just below, and
    # internal/core.TestNewCollectionHasEmptyNotNilItems for the same
    # thing at the Go level.
    original_root = api.get("/api/workspace").get("root")
    tmp_root = tempfile.mkdtemp(prefix="freeman-test-workspace-")
    try:
        r.section("Switching to a disposable temp workspace for this run")
        r.step(f"openWorkspace {{path: {tmp_root!r}}}  (a folder with nothing in it yet)")
        api.action("openWorkspace", {"path": tmp_root})
        # workspaceRoot flips synchronously at the very start of the
        # frontend's initWorkspace() cascade — well before
        # selectEnvironment/selectCollection (each its own async round
        # trip) finish settling selectedItemId/name/environment. Poll for
        # the whole cascade being done, not just the first field to
        # change, or this can observe a real but transient in-between
        # snapshot (temp root, but the old workspace's selected item).
        state = poll(
            api.state,
            lambda s: s.get("workspaceRoot") == tmp_root and s.get("selectedItemId") is None,
        )
        r.check(
            "state.workspaceRoot switched to the temp workspace",
            state.get("workspaceRoot") == tmp_root,
            str(state.get("workspaceRoot")),
        )
        r.check(
            "the app came up on a clean, unsaved 'New Request' instead of crashing",
            state.get("selectedItemId") is None and state.get("name") == "New Request",
            str(state),
        )

        workspace = test_read_only_routes(api, r)
        test_api_guard(api, r)
        collection_id, environment_id = test_workspace_navigation(api, r, workspace)

        collection = api.get(f"/api/collections/{collection_id}")
        r.check(
            "the new collection's items is an empty list, not null — the actual bug",
            collection.get("items") == [],
            str(collection),
        )

        r.section("Creating one empty scratch request per test")
        for key, name in REQUEST_TEST_NAMES.items():
            r.step(f"newRequest, setRequestField name={name!r}, saveRequest")
            api.action("newRequest")
            api.action("setRequestField", {"field": "name", "value": name})
            api.action("saveRequest")
            collection = poll(
                lambda: api.get(f"/api/collections/{collection_id}"),
                lambda c, n=name: find_item(c, n) is not None,
            )
            saved = find_item(collection, name)
            r.check(f"{name!r} created empty", saved is not None, str(collection))
            if saved is not None:
                item_ids[key] = saved["id"]

        # Runs before test_request_editor/test_execute: it's what creates
        # TEST_VAR_KEY, which the test request's URL substitutes.
        test_environment_editor(api, r, environment_id)
        test_request_editor(api, r, collection_id, item_ids.get("request_editor"))
        test_execute(api, r, collection_id, environment_id, item_ids.get("execute"))
        test_ui_state_getter(api, r, collection_id, item_ids.get("ui_state_getter"))
        test_http_methods(api, r, collection_id, environment_id, item_ids)
        test_file_upload(api, r, collection_id, environment_id, item_ids.get("file_upload"))
        test_large_response_truncation(api, r, collection_id, environment_id, item_ids.get("large_response"))
        test_response_cache(api, r, collection_id, environment_id, item_ids.get("response_cache"))
        test_code_tab(api, r, collection_id, environment_id, item_ids.get("code_tab"))
        test_delete_request(api, r, collection_id)
        test_settings_window(api, r)
        test_layout_comfort(api, r)
        test_help_modal(api, r)
        if not args.skip_rapid_fire:
            test_rapid_fire_regression(api, r, environment_id)

        r.section("Deleting every scratch request created for this run")
        for key, item_id in item_ids.items():
            r.step(f"deleteRequest {{id: {item_id}}}  ({key})")
            api.action("deleteRequest", {"id": item_id})
            coll = poll(
                lambda: api.get(f"/api/collections/{collection_id}"),
                lambda c, iid=item_id: find_item_by_id(c, iid) is None,
            )
            r.check(f"{key} scratch request deleted", find_item_by_id(coll, item_id) is None, str(coll))
    except ApiError as e:
        print(f"\n[ERROR] {e}")
        exit_code = 2
    except KeyboardInterrupt:
        print("\nInterrupted.")
        exit_code = 130
    finally:
        if args.pause_before_revert:
            # Never let anything here skip the revert below — an EOFError
            # (stdin isn't a real terminal, e.g. piped/CI) or Ctrl-C must
            # still fall through to restoring the real workspace and
            # removing tmp_root, not abort with the app left pointed at a
            # folder this function is about to delete.
            try:
                input(
                    f"\n    >>> paused with the app pointed at {tmp_root} — go look at it "
                    "(everything this run created is in there). "
                    "Press Enter here to restore your real workspace and continue... "
                )
            except (EOFError, KeyboardInterrupt):
                print("\n    (no interactive terminal to pause on — continuing immediately)")
        # Nested try/finally: rmtree must still happen even if the revert
        # itself fails (a network hiccup, the app having exited, ...) —
        # otherwise a failure right here is the one way this run could
        # leave both the app pointed at tmp_root *and* tmp_root still on
        # disk.
        try:
            r.section("Restoring your real workspace")
            r.step(f"openWorkspace {{path: {original_root!r}}}")
            api.action("openWorkspace", {"path": original_root})
            poll(api.state, lambda s: s.get("workspaceRoot") == original_root)
        except ApiError as e:
            print(f"\n[WARN] failed to restore the real workspace ({original_root}): {e}")
        finally:
            shutil.rmtree(tmp_root, ignore_errors=True)

    return exit_code or r.summary()


if __name__ == "__main__":
    sys.exit(main())
