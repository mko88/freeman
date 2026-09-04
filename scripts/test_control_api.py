#!/usr/bin/env python3
"""Exercise every documented Freeman control-API action end to end.

Freeman's desktop build exposes a loopback HTTP "control API" (see
CLAUDE.md's standing rule: every interactive UI element gets a matching
`ui:action`). This script drives all of it — the read-only /api/* routes
and every action in App.svelte's `uiActions` table — and checks the
result on disk / over HTTP after each step, the same way a human would
click around and then look at what got saved.

It leaves no trace: everything it creates (a scratch request, a couple
of header rows, an environment variable, ...) is deleted/removed again
before it exits — in a `finally`, so a failed check or a crash mid-run
doesn't leave fixtures behind either — and it also sweeps for and
removes any leftovers from a *previous* interrupted run before it starts
(a "Pre-clean" section runs first). Run it back-to-back as many times as
you like; the workspace should look identical before and after.

KEEP THIS IN SYNC with App.svelte's `uiActions`/`apiEndpoints` tables —
when an action's payload shape changes, or a new one is added, update
the matching section here (see CLAUDE.md).

Usage:
    py scripts/test_control_api.py                    # ~0.6s between actions, watch it run
    py scripts/test_control_api.py --delay 1.5         # slower, easier to follow on screen
    py scripts/test_control_api.py --delay 0           # no pauses, fast/CI-style
    py scripts/test_control_api.py --base-url http://127.0.0.1:9090
    py scripts/test_control_api.py --skip-rapid-fire   # skip the ordering regression check

Requires Python 3.8+, standard library only — no pip install needed.
Freeman must already be running (desktop build) with a workspace open
(at least one collection and one environment).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from typing import Any, Optional

# ---------------------------------------------------------------------------
# Test data identifiers. Every run creates these fresh and removes them
# again (see cleanup() and each test function) — fixed names/keys just
# make a mid-run screenshot or a crash's leftovers recognizable as this
# script's, not because anything here is meant to persist.
# ---------------------------------------------------------------------------
TEST_REQUEST_NAME = "Python Control API Test"
TEST_HEADER_KEY = "X-Py-Test"
SCRATCH_HEADER_KEY = "X-Py-Scratch"
TEST_VAR_KEY = "pyTestBase"
TEST_VAR_VALUE = "https://httpbin.org"
SCRATCH_VAR_KEY = "pyScratchVar"
DELETE_TEST_NAME = "Python Delete Test (scratch)"


class ApiError(RuntimeError):
    pass


class ControlAPI:
    """Thin HTTP client for Freeman's control API. stdlib-only (urllib) so
    this script needs nothing beyond a base Python install."""

    def __init__(self, base_url: str, delay: float, verbose: bool = False):
        self.base_url = base_url.rstrip("/")
        self.delay = delay
        self.verbose = verbose

    def _request(self, method: str, path: str, body: Optional[dict] = None) -> tuple[int, Any]:
        url = f"{self.base_url}{path}"
        data = json.dumps(body).encode("utf-8") if body is not None else None
        headers = {"Content-Type": "application/json"} if data is not None else {}
        req = urllib.request.Request(url, data=data, method=method, headers=headers)
        if self.verbose:
            print(f"    -> {method} {path}" + (f" {json.dumps(body)}" if body else ""))
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                raw, status = resp.read(), resp.status
        except urllib.error.HTTPError as e:
            raw, status = e.read(), e.code
        except urllib.error.URLError as e:
            raise ApiError(
                f"Can't reach {url}: {e.reason}. Is Freeman running with the control API "
                "enabled? (desktop build only — see CLAUDE.md)"
            ) from e
        text = raw.decode("utf-8") if raw else ""
        parsed: Any = None
        if text:
            try:
                parsed = json.loads(text)
            except json.JSONDecodeError:
                parsed = text
        return status, parsed

    def get(self, path: str) -> Any:
        status, body = self._request("GET", path)
        if status >= 400:
            raise ApiError(f"GET {path} -> {status}: {body}")
        return body

    def post(self, path: str, body: Optional[dict] = None) -> tuple[int, Any]:
        return self._request("POST", path, body)

    def delete(self, path: str) -> tuple[int, Any]:
        return self._request("DELETE", path)

    def action(self, name: str, payload: Optional[dict] = None, wait: bool = True) -> None:
        """Fire one ui:action. Waits `self.delay` seconds afterward by
        default so a human watching the app window can follow along —
        pass wait=False for the rapid-fire regression check, which
        specifically wants zero delay between calls."""
        status, body = self.post("/api/ui/action", {"action": name, "payload": payload or {}})
        if status != 204:
            raise ApiError(f"ui:action {name} -> {status}: {body}")
        if wait and self.delay > 0:
            time.sleep(self.delay)


# ---------------------------------------------------------------------------
# Pass/fail reporting
# ---------------------------------------------------------------------------
class Report:
    def __init__(self, use_color: bool):
        self.passed = 0
        self.failed = 0
        self.use_color = use_color

    def _c(self, code: str, text: str) -> str:
        return f"\033[{code}m{text}\033[0m" if self.use_color else text

    def section(self, title: str) -> None:
        print("\n" + self._c("1;36", f"=== {title} ==="))

    def step(self, description: str) -> None:
        print(self._c("2", f"  > {description}"))

    def check(self, description: str, condition: bool, detail: str = "") -> None:
        if condition:
            self.passed += 1
            print(f"  {self._c('32', '[PASS]')} {description}")
        else:
            self.failed += 1
            suffix = f" — {detail}" if detail else ""
            print(f"  {self._c('31', '[FAIL]')} {description}{suffix}")

    def summary(self) -> int:
        total = self.passed + self.failed
        print("\n" + "-" * 60)
        if self.failed:
            print(self._c("1;31", f"{self.failed}/{total} checks FAILED"))
        else:
            print(self._c("1;32", f"All {total} checks passed"))
        return 1 if self.failed else 0


def poll(fetch, predicate, timeout: float = 6.0, interval: float = 0.1):
    """Call fetch() until predicate(result) is true or timeout elapses,
    returning the last result either way. Needed because POST
    /api/ui/action returns 204 as soon as the event is *emitted*, not
    once the frontend has applied it — an action like saveRequest or
    saveEnvironment does a real Go RPC plus a disk write, which can
    easily take longer than a human-watching --delay (especially
    --delay 0). An immediate verification GET without this would race
    the write instead of testing anything meaningful. The default
    timeout is generous (6s, not ~1s) because ui:action events are
    processed through one serialized queue in App.svelte — an action
    right after `sendRequest` can be stuck waiting behind a real network
    call to whatever API the request targets, not just local disk I/O."""
    deadline = time.time() + timeout
    result = fetch()
    while not predicate(result) and time.time() < deadline:
        time.sleep(interval)
        result = fetch()
    return result


def find_item(collection: dict, name: str) -> Optional[dict]:
    for item in collection.get("items", []):
        if item.get("name") == name:
            return item
    return None


def find_item_by_id(collection: dict, item_id: str) -> Optional[dict]:
    for item in collection.get("items", []):
        if item.get("id") == item_id:
            return item
    return None


def find_header_index(headers: Optional[list], key: str) -> Optional[int]:
    for i, h in enumerate(headers or []):
        if h.get("key") == key:
            return i
    return None


def find_variable_index(variables: Optional[list], key: str) -> Optional[int]:
    for i, v in enumerate(variables or []):
        if v.get("key") == key:
            return i
    return None


# ---------------------------------------------------------------------------
# Test sections
# ---------------------------------------------------------------------------
def precleanup(api: ControlAPI, r: Report, collection_id: str, environment_id: str) -> None:
    """Removes any fixture data left behind by a previous run that never
    reached its own cleanup() — the app was closed mid-run, a check
    raised before cleanup ran, Ctrl-C, etc. Every run starts from this,
    so the workspace can never accumulate this script's leftovers no
    matter how a previous run ended, and running it once is also how you
    clear out whatever an old run already left on disk."""
    r.section("Pre-clean (remove any leftovers from a previous run)")

    found_any = False
    collection = api.get(f"/api/collections/{collection_id}")
    for name in (TEST_REQUEST_NAME, DELETE_TEST_NAME):
        item = find_item(collection, name)
        while item is not None:
            found_any = True
            r.step(f"deleteRequest {{id: {item['id']}}}  (leftover {name!r})")
            api.action("deleteRequest", {"id": item["id"]})
            collection = poll(
                lambda: api.get(f"/api/collections/{collection_id}"),
                lambda c, n=name: find_item(c, n) is None,
            )
            item = find_item(collection, name)

    env = api.get(f"/api/environments/{environment_id}")
    if find_variable_index(env.get("variables"), TEST_VAR_KEY) is not None:
        found_any = True
        r.step(f"removeEnvironmentVariable {{key: {TEST_VAR_KEY!r}}}  (leftover), saveEnvironment")
        api.action("removeEnvironmentVariable", {"key": TEST_VAR_KEY})
        api.action("saveEnvironment")
        poll(
            lambda: api.get(f"/api/environments/{environment_id}"),
            lambda e: find_variable_index(e.get("variables"), TEST_VAR_KEY) is None,
        )

    if not found_any:
        r.step("nothing to clean up — workspace was already clear")


def cleanup(
    api: ControlAPI,
    r: Report,
    collection_id: Optional[str],
    item_id: Optional[str],
    environment_id: Optional[str],
) -> None:
    """Removes everything test_request_editor/test_environment_editor
    created, so the workspace ends up exactly as precleanup() found it.
    Called from main()'s `finally`, so it runs even if an earlier check
    failed or an action raised. test_delete_request cleans up its own
    scratch request itself, immediately, as part of what it's testing —
    nothing left for this function to do there."""
    if not ((item_id and collection_id) or environment_id):
        return
    r.section("Cleanup (leave the workspace exactly as found)")

    if item_id and collection_id:
        r.step(f"deleteRequest {{id: {item_id}}}")
        try:
            api.action("deleteRequest", {"id": item_id})
            collection = poll(
                lambda: api.get(f"/api/collections/{collection_id}"),
                lambda c: find_item_by_id(c, item_id) is None,
            )
            r.check("test request removed", find_item_by_id(collection, item_id) is None)
        except ApiError as e:
            r.check("test request removed", False, str(e))

    if environment_id:
        r.step(f"removeEnvironmentVariable {{key: {TEST_VAR_KEY!r}}}, saveEnvironment")
        try:
            api.action("removeEnvironmentVariable", {"key": TEST_VAR_KEY})
            api.action("saveEnvironment")
            env = poll(
                lambda: api.get(f"/api/environments/{environment_id}"),
                lambda e: find_variable_index(e.get("variables"), TEST_VAR_KEY) is None,
            )
            r.check(
                f"{TEST_VAR_KEY} variable removed",
                find_variable_index(env.get("variables"), TEST_VAR_KEY) is None,
            )
        except ApiError as e:
            r.check(f"{TEST_VAR_KEY} variable removed", False, str(e))


def test_read_only_routes(api: ControlAPI, r: Report) -> dict:
    r.section("Read-only routes")

    health = api.get("/api/health")
    r.check("GET /api/health", health == {"status": "ok"}, str(health))

    workspace = api.get("/api/workspace")
    r.check(
        "GET /api/workspace has a collection and an environment",
        bool(workspace.get("collections")) and bool(workspace.get("environments")),
        "open a workspace with at least one of each before running this script",
    )

    theme = api.get("/api/theme")
    r.check("GET /api/theme returns a palette", isinstance(theme, dict) and "accent" in theme, str(theme))

    headers_catalog = api.get("/api/headers")
    names = {e.get("name") for e in headers_catalog} if isinstance(headers_catalog, list) else set()
    r.check("GET /api/headers includes Content-Type", "Content-Type" in names, str(names))

    return workspace


def test_workspace_navigation(api: ControlAPI, r: Report, workspace: dict) -> tuple[str, str]:
    r.section("Workspace navigation (selectCollection / selectEnvironment)")

    collection_id = workspace["collections"][0]["id"]
    environment_id = workspace["environments"][0]["id"]

    r.step(f"selectCollection {{id: {collection_id}}}")
    api.action("selectCollection", {"id": collection_id})
    r.check("selectCollection accepted", True)

    r.step(f"selectEnvironment {{id: {environment_id}}}")
    api.action("selectEnvironment", {"id": environment_id})
    r.check("selectEnvironment accepted", True)

    return collection_id, environment_id


def test_request_editor(api: ControlAPI, r: Report, collection_id: str) -> str:
    """Creates TEST_REQUEST_NAME from scratch every time (no "does it
    already exist" check) — precleanup() already guarantees it doesn't.
    Returns its id, which main() hands to cleanup() to delete again."""
    r.section("Request editor (newRequest / setRequestField / tabs / headers)")

    r.step("newRequest")
    api.action("newRequest")

    r.step(f"setRequestField name = {TEST_REQUEST_NAME!r}")
    api.action("setRequestField", {"field": "name", "value": TEST_REQUEST_NAME})
    r.step("setRequestField method = 'POST'")
    api.action("setRequestField", {"field": "method", "value": "POST"})
    url = f"{{{{{TEST_VAR_KEY}}}}}/post"
    r.step(f"setRequestField url = {url!r}")
    api.action("setRequestField", {"field": "url", "value": url})

    r.step("selectRequestTab 'body'")
    api.action("selectRequestTab", {"tab": "body"})
    body_raw = '{"widget":"gizmo","qty":3,"source":"python-test-script"}'
    r.step("setRequestField bodyRaw = <json>")
    api.action("setRequestField", {"field": "bodyRaw", "value": body_raw})
    # No setRequestField bodyContentType: retired 2026-09-04 — Content-Type is a
    # regular header now (see addRequestHeader below), not a separate field.
    # draftBodyContentType still defaults to 'application/json' as
    # saveRequest's fallback, checked below without setting it here.

    r.step("selectRequestTab 'headers'")
    api.action("selectRequestTab", {"tab": "headers"})

    r.step(f"addRequestHeader {{key: {TEST_HEADER_KEY!r}, value: '1'}}")
    api.action("addRequestHeader", {"key": TEST_HEADER_KEY, "value": "1"})

    # Scratch header: prove addRequestHeader + removeRequestHeader-by-key both work,
    # without leaving anything behind.
    r.step(f"addRequestHeader {{key: {SCRATCH_HEADER_KEY!r}}}  (scratch, removed below)")
    api.action("addRequestHeader", {"key": SCRATCH_HEADER_KEY, "value": "temporary"})
    r.step(f"removeRequestHeader {{key: {SCRATCH_HEADER_KEY!r}}}")
    api.action("removeRequestHeader", {"key": SCRATCH_HEADER_KEY})

    r.step("saveRequest")
    api.action("saveRequest")

    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: find_item(c, TEST_REQUEST_NAME) is not None,
    )
    saved = find_item(collection, TEST_REQUEST_NAME)
    r.check("saveRequest persisted the request", saved is not None)
    if saved is None:
        return ""

    r.check("name round-tripped", saved.get("name") == TEST_REQUEST_NAME)
    r.check("method round-tripped", saved.get("method") == "POST")
    r.check("url round-tripped", saved.get("url") == url)
    r.check("body.raw round-tripped", (saved.get("body") or {}).get("raw") == body_raw)
    r.check(
        "body.rawContentType defaulted to 'application/json' (no longer separately settable)",
        (saved.get("body") or {}).get("rawContentType") == "application/json",
    )
    saved_headers = saved.get("headers") or []
    r.check(
        f"{TEST_HEADER_KEY} header present with value '1'",
        any(h.get("key") == TEST_HEADER_KEY and h.get("value") == "1" for h in saved_headers),
        str(saved_headers),
    )
    r.check(
        f"{SCRATCH_HEADER_KEY} header was removed, not left behind",
        find_header_index(saved_headers, SCRATCH_HEADER_KEY) is None,
        str(saved_headers),
    )

    return saved["id"]


def test_execute(api: ControlAPI, r: Report, collection_id: str, item_id: str, environment_id: str) -> None:
    r.section("Execute (sendRequest)")

    if not item_id:
        r.check("skipped — no saved request id from the previous section", False)
        return

    # Fire the same action a click on "Send" would, so it's visible in the
    # running app — but verify the result via POST /api/execute directly,
    # since the rendered response pane isn't readable over the control API.
    r.step("sendRequest  (watch the app: response pane should fill in)")
    api.action("sendRequest")

    r.step(f"POST /api/execute {{collectionId, itemId, environmentId}}  (verifying the same request)")
    status, resp = api.post(
        "/api/execute",
        {"collectionId": collection_id, "itemId": item_id, "environmentId": environment_id},
    )
    r.check("POST /api/execute -> 200", status == 200, f"status={status} body={resp}")
    if status == 200 and isinstance(resp, dict):
        r.check("response statusCode is 200 (httpbin reachable)", resp.get("statusCode") == 200, str(resp))
        body_text = resp.get("body", "")
        r.check(
            "{{pyTestBase}} was substituted into the URL before sending",
            "httpbin.org" in body_text or '"url"' in body_text,
            body_text[:200],
        )
        r.check("posted body round-tripped through httpbin", "python-test-script" in body_text, body_text[:200])


def test_delete_request(api: ControlAPI, r: Report, collection_id: str) -> None:
    """Fully self-contained: creates DELETE_TEST_NAME and deletes it
    again within this function, regardless of what main()'s cleanup()
    does — nothing here relies on, or needs, outside cleanup."""
    r.section("Delete a request (deleteRequest / DELETE /api/collections/{id}/requests/{itemId})")

    r.step(f"newRequest, setRequestField name={DELETE_TEST_NAME!r}, saveRequest  (a throwaway request, just to delete it)")
    api.action("newRequest")
    api.action("setRequestField", {"field": "name", "value": DELETE_TEST_NAME})
    api.action("saveRequest")

    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: find_item(c, DELETE_TEST_NAME) is not None,
    )
    scratch = find_item(collection, DELETE_TEST_NAME)
    r.check("scratch request was created", scratch is not None)
    if scratch is None:
        return
    item_id = scratch["id"]

    r.step(f"deleteRequest {{id: {item_id}}}  (watch: it should disappear from the sidebar)")
    api.action("deleteRequest", {"id": item_id})

    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: find_item(c, DELETE_TEST_NAME) is None,
    )
    r.check(
        "deleteRequest removed the request",
        find_item(collection, DELETE_TEST_NAME) is None,
        str(collection.get("items")),
    )

    r.step(f"DELETE /api/collections/{collection_id}/requests/{item_id}  (already gone — expect an error)")
    status, body = api.delete(f"/api/collections/{collection_id}/requests/{item_id}")
    r.check(
        "deleting an already-deleted request errors instead of silently succeeding",
        status >= 400,
        f"status={status} body={body}",
    )


def test_environment_editor(api: ControlAPI, r: Report, environment_id: str) -> None:
    """Adds TEST_VAR_KEY (left for test_execute's {{pyTestBase}} to use,
    then removed by main()'s cleanup() at the very end) and a fully
    self-contained SCRATCH_VAR_KEY (added, updated, and removed again
    right here, proving add/set/remove all work without relying on
    outside cleanup for it)."""
    r.section("Environment editor (toggleEnvironmentEditor / *Variable / saveEnvironment)")

    r.step("toggleEnvironmentEditor  (watch: the modal should open)")
    api.action("toggleEnvironmentEditor")

    r.step(f"addEnvironmentVariable {{key: {TEST_VAR_KEY!r}, value: {TEST_VAR_VALUE!r}}}")
    api.action("addEnvironmentVariable", {"key": TEST_VAR_KEY, "value": TEST_VAR_VALUE})

    r.step(f"addEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}, secret: true}}  (scratch)")
    api.action("addEnvironmentVariable", {"key": SCRATCH_VAR_KEY, "value": "temp", "secret": True})
    r.step(f"setEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}, value: 'updated'}}")
    api.action("setEnvironmentVariable", {"key": SCRATCH_VAR_KEY, "value": "updated"})
    r.step(f"removeEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}}}")
    api.action("removeEnvironmentVariable", {"key": SCRATCH_VAR_KEY})

    r.step("saveEnvironment")
    api.action("saveEnvironment")

    r.step("toggleEnvironmentEditor  (watch: the modal should close)")
    api.action("toggleEnvironmentEditor")

    env = poll(
        lambda: api.get(f"/api/environments/{environment_id}"),
        lambda e: find_variable_index(e.get("variables"), TEST_VAR_KEY) is not None,
    )
    variables = env.get("variables") or []
    r.check(
        f"{TEST_VAR_KEY} = {TEST_VAR_VALUE!r} persisted",
        any(v.get("key") == TEST_VAR_KEY and v.get("value") == TEST_VAR_VALUE for v in variables),
        str(variables),
    )
    r.check(
        f"{SCRATCH_VAR_KEY} was removed, not left behind",
        find_variable_index(variables, SCRATCH_VAR_KEY) is None,
        str(variables),
    )


def test_help_modal(api: ControlAPI, r: Report) -> None:
    r.section("Help modal")
    r.step("toggleHelp  (watch: the Control API help panel should open)")
    api.action("toggleHelp")
    r.step("toggleHelp  (watch: it should close again)")
    api.action("toggleHelp")
    r.check("toggleHelp open/close accepted", True)


def test_rapid_fire_regression(api: ControlAPI, r: Report, environment_id: str) -> None:
    """Regression check for a real bug found 2026-09-04: firing ui:action
    events back-to-back with no delay used to race, because several
    actions (saveRequest, sendRequest, saveEnvironment,
    selectEnvironment, selectCollection, openWorkspace) do an async round
    trip and dispatchUIAction wasn't awaited between events. A
    removeEnvironmentVariable sent right after a saveEnvironment could
    get silently clobbered when the first saveEnvironment call's stale
    `environment = saved` resolved later. Fixed by serializing ui:action
    events through one promise chain in App.svelte. This replays that
    exact shape at zero delay, regardless of --delay. Fully
    self-contained, same as the other scratch-key tests."""
    r.section("Regression: rapid-fire ui:action ordering (no delay)")

    key = "pyRaceVar"
    r.step(f"add/set/save/remove/save {key!r} back-to-back, no waiting between calls")
    api.action("addEnvironmentVariable", {"key": key, "value": "v1"}, wait=False)
    api.action("setEnvironmentVariable", {"key": key, "value": "v2"}, wait=False)
    api.action("saveEnvironment", wait=False)
    api.action("removeEnvironmentVariable", {"key": key}, wait=False)
    api.action("saveEnvironment", wait=False)

    env = poll(
        lambda: api.get(f"/api/environments/{environment_id}"),
        lambda e: find_variable_index(e.get("variables"), key) is None,
        timeout=3.0,
    )
    variables = env.get("variables") or []
    r.check(
        f"{key} ends up absent (rapid add-then-remove wasn't lost/reordered)",
        find_variable_index(variables, key) is None,
        str(variables),
    )


def main() -> int:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--base-url", default="http://127.0.0.1:8090", help="control API base URL")
    parser.add_argument(
        "--delay", type=float, default=0.6, help="seconds to pause after each action, so you can watch it (default: 0.6, use 0 to run fast)"
    )
    parser.add_argument("--skip-rapid-fire", action="store_true", help="skip the ordering regression check")
    parser.add_argument("-v", "--verbose", action="store_true", help="print every HTTP call")
    parser.add_argument("--no-color", action="store_true", help="disable ANSI colors")
    args = parser.parse_args()

    use_color = not args.no_color and sys.stdout.isatty() and not os.environ.get("NO_COLOR")
    api = ControlAPI(args.base_url, args.delay, args.verbose)
    r = Report(use_color)

    print(f"Freeman control API test — base URL {args.base_url}, delay {args.delay}s")

    collection_id: Optional[str] = None
    environment_id: Optional[str] = None
    item_id: Optional[str] = None
    exit_code = 0
    try:
        workspace = test_read_only_routes(api, r)
        collection_id, environment_id = test_workspace_navigation(api, r, workspace)
        precleanup(api, r, collection_id, environment_id)
        # Runs before test_request_editor/test_execute: it's what creates
        # TEST_VAR_KEY, which the test request's URL substitutes.
        test_environment_editor(api, r, environment_id)
        item_id = test_request_editor(api, r, collection_id)
        test_execute(api, r, collection_id, item_id, environment_id)
        test_delete_request(api, r, collection_id)
        test_help_modal(api, r)
        if not args.skip_rapid_fire:
            test_rapid_fire_regression(api, r, environment_id)
    except ApiError as e:
        print(f"\n[ERROR] {e}")
        exit_code = 2
    except KeyboardInterrupt:
        print("\nInterrupted.")
        exit_code = 130
    finally:
        cleanup(api, r, collection_id, item_id, environment_id)

    return exit_code or r.summary()


if __name__ == "__main__":
    sys.exit(main())
