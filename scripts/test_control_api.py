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

KEEP THIS IN SYNC with App.svelte's `uiActions`/`apiEndpoints` tables —
when an action's payload shape changes, or a new one is added, update
the matching section here (see CLAUDE.md).

Usage:
    py scripts/test_control_api.py                    # ~0.6s between actions, watch it run
    py scripts/test_control_api.py --delay 1.5         # slower, easier to follow on screen
    py scripts/test_control_api.py --delay 0           # no pauses, fast/CI-style
    py scripts/test_control_api.py --base-url http://127.0.0.1:9090
    py scripts/test_control_api.py --skip-rapid-fire   # skip the ordering regression check
    py scripts/test_control_api.py --pause-before-revert  # hold on the temp workspace so you can look at it yourself

Requires Python 3.8+, standard library only — no pip install needed.
Freeman must already be running (desktop build) with a workspace open
(at least one collection and one environment).
"""

from __future__ import annotations

import argparse
import base64
import http.server
import json
import os
import shutil
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any, Optional

# ---------------------------------------------------------------------------
# Test data identifiers. Every run creates these fresh inside the
# disposable temp workspace main() switches to — fixed names/keys just
# make a mid-run screenshot recognizable as this script's, not because
# anything here needs cleaning up (the whole temp workspace is discarded
# at the end regardless).
# ---------------------------------------------------------------------------
TEST_REQUEST_NAME = "Python Request Editor Test (scratch)"
EXECUTE_TEST_NAME = "Python Execute Test (scratch)"
UI_STATE_GETTER_TEST_NAME = "Python UI State Getter Test (scratch)"
FILE_UPLOAD_TEST_NAME = "Python File Upload Test (scratch)"
DELETE_TEST_NAME = "Python Delete Test (scratch)"
TEST_HEADER_KEY = "X-Py-Test"
SCRATCH_HEADER_KEY = "X-Py-Scratch"
TEST_PARAM_KEY = "pyParam"
SCRATCH_PARAM_KEY = "pyScratchParam"
FORM_FIELD_KEY = "item"
SCRATCH_FIELD_KEY = "scratchField"
TEST_VAR_KEY = "pyTestBase"
TEST_VAR_VALUE = "https://httpbin.org"
SCRATCH_VAR_KEY = "pyScratchVar"
HTTP_METHODS = ("GET", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS")

# Every test below that needs a saved request gets its own dedicated
# name here — created empty in main()'s Phase 1 (before any test runs),
# selected/populated/executed by its own test when its turn comes, and
# deleted in main()'s Phase 3 (after every test has run). Nothing here
# is shared between tests. DELETE_TEST_NAME is deliberately not part of
# this: test_delete_request's own create-then-delete cycle *is* what it
# tests, so it manages its own lifecycle independently rather than
# reusing a pre-made empty request.
REQUEST_TEST_NAMES: dict[str, str] = {
    "request_editor": TEST_REQUEST_NAME,
    "execute": EXECUTE_TEST_NAME,
    "ui_state_getter": UI_STATE_GETTER_TEST_NAME,
    "file_upload": FILE_UPLOAD_TEST_NAME,
    "large_response": "Python Large Response Test (scratch)",
    "response_cache": "Python Response Cache Test (scratch)",
    **{m: f"Python {m} Test (scratch)" for m in HTTP_METHODS},
}

# A committed, reusable fixture (real random bytes, not text) — read-only
# to every test that uses it; nothing here ever writes to or deletes it.
# Regenerate it if you ever want a fresh one:
#   py -c "import pathlib,secrets; pathlib.Path('scripts/random-sample.bin').write_bytes(secrets.token_bytes(512))"
RANDOM_FILE_PATH = Path(__file__).resolve().parent / "random-sample.bin"


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

    def state(self) -> dict:
        """GET /api/ui/state — the getter side of the control API (see
        CLAUDE.md's "every settable field needs a getter too" rule).
        Callers should poll() this, same as any other route: it reflects
        whatever App.svelte's reportUIState last pushed, which lands
        after the action that changed it has fully applied, not before."""
        body = self.get("/api/ui/state")
        return body if isinstance(body, dict) else {}


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


def decode_data_uri_bytes(data_uri: str) -> bytes:
    """Decodes httpbin's "data:<mime-type>;base64,<...>" shape (what it
    returns for a body it can't render as text — exactly what a random
    binary fixture produces) back to raw bytes, for an exact comparison
    against the original file instead of a fragile substring match."""
    if ";base64," not in data_uri:
        return data_uri.encode()
    return base64.b64decode(data_uri.split(";base64,", 1)[1])


# ---------------------------------------------------------------------------
# Test sections
# ---------------------------------------------------------------------------


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

    ui_state = api.state()
    r.check("GET /api/ui/state returns an object", isinstance(ui_state, dict), str(ui_state))

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


def test_request_editor(api: ControlAPI, r: Report, collection_id: str, item_id: str) -> None:
    """Populates the empty scratch request main() already created and
    saved for this test (see REQUEST_TEST_NAMES) — selecting it, not
    creating it, is the first step here."""
    r.section("Request editor (newRequest / setRequestField / tabs / params / headers)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    r.step(f"selectRequest {{id: {item_id}}}")
    api.action("selectRequest", {"id": item_id})
    poll(api.state, lambda s: s.get("selectedItemId") == item_id)

    r.step("setRequestField method = 'POST'")
    api.action("setRequestField", {"field": "method", "value": "POST"})
    url = f"{{{{{TEST_VAR_KEY}}}}}/post"
    r.step(f"setRequestField url = {url!r}")
    api.action("setRequestField", {"field": "url", "value": url})

    r.step("selectRequestTab 'body'")
    api.action("selectRequestTab", {"tab": "body"})

    # Body mode is its own explicit field (not inferred from whether
    # bodyRaw is non-empty) — exercise switching to form-data and
    # x-www-form-urlencoded, with a full add/remove cycle on each,
    # before settling back on 'raw' for the rest of this test (and
    # test_execute, which expects a JSON POST body).
    r.step("setRequestField bodyMode = 'form-data'")
    api.action("setRequestField", {"field": "bodyMode", "value": "form-data"})
    r.step(f"addRequestFormField {{key: {FORM_FIELD_KEY!r}, value: 'gizmo'}}")
    api.action("addRequestFormField", {"key": FORM_FIELD_KEY, "value": "gizmo"})
    r.step(f"addRequestFormField {{key: {SCRATCH_FIELD_KEY!r}}}  (scratch, removed below)")
    api.action("addRequestFormField", {"key": SCRATCH_FIELD_KEY, "value": "temporary"})
    r.step(f"removeRequestFormField {{key: {SCRATCH_FIELD_KEY!r}}}")
    api.action("removeRequestFormField", {"key": SCRATCH_FIELD_KEY})
    r.step("saveRequest  (checkpoint: verify the form-data body persists)")
    api.action("saveRequest")

    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: ((find_item(c, TEST_REQUEST_NAME) or {}).get("body") or {}).get("mode") == "form-data",
    )
    form_saved = find_item(collection, TEST_REQUEST_NAME) or {}
    form_body = form_saved.get("body") or {}
    r.check("body.mode round-tripped to 'form-data'", form_body.get("mode") == "form-data")
    form_fields = form_body.get("formFields") or []
    r.check(
        f"{FORM_FIELD_KEY} form field present with value 'gizmo'",
        any(f.get("key") == FORM_FIELD_KEY and f.get("value") == "gizmo" for f in form_fields),
        str(form_fields),
    )
    r.check(
        f"{SCRATCH_FIELD_KEY} form field was removed, not left behind",
        find_header_index(form_fields, SCRATCH_FIELD_KEY) is None,
        str(form_fields),
    )
    r.check("body.raw is empty while mode is 'form-data'", not form_body.get("raw"), str(form_body))

    r.step("setRequestField bodyMode = 'raw'  (back to what the rest of this test expects)")
    api.action("setRequestField", {"field": "bodyMode", "value": "raw"})
    body_raw = '{"widget":"gizmo","qty":3,"source":"python-test-script"}'
    r.step("setRequestField bodyRaw = <json>")
    api.action("setRequestField", {"field": "bodyRaw", "value": body_raw})

    r.step("selectRequestTab 'headers'")
    api.action("selectRequestTab", {"tab": "headers"})

    r.step("addRequestHeader {key: 'Content-Type', value: 'application/json'}")
    api.action("addRequestHeader", {"key": "Content-Type", "value": "application/json"})

    r.step(f"addRequestHeader {{key: {TEST_HEADER_KEY!r}, value: '1'}}")
    api.action("addRequestHeader", {"key": TEST_HEADER_KEY, "value": "1"})

    # Scratch header: prove addRequestHeader + removeRequestHeader-by-key both work,
    # without leaving anything behind.
    r.step(f"addRequestHeader {{key: {SCRATCH_HEADER_KEY!r}}}  (scratch, removed below)")
    api.action("addRequestHeader", {"key": SCRATCH_HEADER_KEY, "value": "temporary"})
    r.step(f"removeRequestHeader {{key: {SCRATCH_HEADER_KEY!r}}}")
    api.action("removeRequestHeader", {"key": SCRATCH_HEADER_KEY})

    r.step("selectRequestTab 'params'")
    api.action("selectRequestTab", {"tab": "params"})
    state = poll(api.state, lambda s: s.get("tab") == "params")
    r.check("state.tab reflects selectRequestTab 'params'", state.get("tab") == "params", str(state.get("tab")))

    r.step(f"addRequestParam {{key: {TEST_PARAM_KEY!r}, value: 'yes'}}")
    api.action("addRequestParam", {"key": TEST_PARAM_KEY, "value": "yes"})
    r.step(f"addRequestParam {{key: {SCRATCH_PARAM_KEY!r}}}  (scratch, removed below)")
    api.action("addRequestParam", {"key": SCRATCH_PARAM_KEY, "value": "temporary"})
    r.step("setRequestParam {index: 0, value: 'confirmed'}  (edit the first row by position)")
    api.action("setRequestParam", {"index": 0, "value": "confirmed"})
    r.step(f"removeRequestParam {{key: {SCRATCH_PARAM_KEY!r}}}")
    api.action("removeRequestParam", {"key": SCRATCH_PARAM_KEY})

    r.step("saveRequest")
    api.action("saveRequest")

    # Predicate checks body.mode specifically, not just presence — the
    # item already exists from the form-data checkpoint save above, so
    # "the name exists" would be trivially true before this save lands.
    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: ((find_item(c, TEST_REQUEST_NAME) or {}).get("body") or {}).get("mode") == "raw",
    )
    saved = find_item(collection, TEST_REQUEST_NAME)
    r.check("saveRequest persisted the request", saved is not None)
    if saved is None:
        return

    r.check("name round-tripped", saved.get("name") == TEST_REQUEST_NAME)
    r.check("method round-tripped", saved.get("method") == "POST")
    r.check("url round-tripped", saved.get("url") == url)
    r.check("body.mode round-tripped to 'raw'", (saved.get("body") or {}).get("mode") == "raw")
    r.check("body.raw round-tripped", (saved.get("body") or {}).get("raw") == body_raw)
    r.check(
        "body has no rawContentType key",
        "rawContentType" not in (saved.get("body") or {}),
        str(saved.get("body")),
    )
    r.check(
        "body.formFields cleared after switching back to 'raw'",
        not (saved.get("body") or {}).get("formFields"),
        str(saved.get("body")),
    )
    saved_headers = saved.get("headers") or []
    r.check(
        "Content-Type header present with value 'application/json'",
        any(h.get("key") == "Content-Type" and h.get("value") == "application/json" for h in saved_headers),
        str(saved_headers),
    )
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
    saved_params = saved.get("params") or []
    r.check(
        f"{TEST_PARAM_KEY} param present, with the setRequestParam-edited value",
        any(p.get("key") == TEST_PARAM_KEY and p.get("value") == "confirmed" for p in saved_params),
        str(saved_params),
    )
    r.check(
        f"{SCRATCH_PARAM_KEY} param was removed, not left behind",
        find_header_index(saved_params, SCRATCH_PARAM_KEY) is None,
        str(saved_params),
    )

    # --- Auth tab: set bearer auth, verify it round-trips, then clear it
    # again so test_execute's request goes out without an Authorization
    # header. ---
    r.step("selectRequestTab 'auth'")
    api.action("selectRequestTab", {"tab": "auth"})
    state = poll(api.state, lambda s: s.get("tab") == "auth")
    r.check("state.tab reflects selectRequestTab 'auth'", state.get("tab") == "auth", str(state.get("tab")))

    r.step("setRequestAuth {field: 'type', value: 'bearer'}, {field: 'token', value: '{{" + TEST_VAR_KEY + "}}'}")
    api.action("setRequestAuth", {"field": "type", "value": "bearer"})
    token_ref = f"{{{{{TEST_VAR_KEY}}}}}"
    api.action("setRequestAuth", {"field": "token", "value": token_ref})
    state = poll(api.state, lambda s: (s.get("auth") or {}).get("token") == token_ref)
    r.check(
        "state.auth mirrors type='bearer' and the token",
        (state.get("auth") or {}).get("type") == "bearer" and (state.get("auth") or {}).get("token") == token_ref,
        str(state.get("auth")),
    )

    r.step("saveRequest  (checkpoint: auth persists)")
    api.action("saveRequest")
    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: ((find_item(c, TEST_REQUEST_NAME) or {}).get("auth") or {}).get("type") == "bearer",
    )
    saved_auth = (find_item(collection, TEST_REQUEST_NAME) or {}).get("auth") or {}
    r.check("auth.type round-tripped to 'bearer'", saved_auth.get("type") == "bearer", str(saved_auth))
    r.check("auth.token round-tripped unsubstituted", saved_auth.get("token") == token_ref, str(saved_auth))

    r.step("setRequestAuth {field: 'type', value: 'none'}, saveRequest  (clear it)")
    api.action("setRequestAuth", {"field": "type", "value": "none"})
    api.action("saveRequest")
    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: "auth" not in (find_item(c, TEST_REQUEST_NAME) or {"auth": 1}),
    )
    r.check(
        "auth key is omitted entirely when type is 'none'",
        "auth" not in (find_item(collection, TEST_REQUEST_NAME) or {}),
        str(find_item(collection, TEST_REQUEST_NAME)),
    )


def test_execute(api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str) -> None:
    """Populates the empty scratch request main() already created and
    saved for this test (see REQUEST_TEST_NAMES) with just enough — a
    JSON POST — to prove sendRequest/execute works, independent of
    test_request_editor's own (much more thoroughly exercised) request."""
    r.section("Execute (sendRequest)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    r.step(f"selectRequest {{id: {item_id}}}, setRequestField method/url/bodyMode/bodyRaw, addRequestHeader, saveRequest")
    api.action("selectRequest", {"id": item_id})
    poll(api.state, lambda s: s.get("selectedItemId") == item_id)
    api.action("setRequestField", {"field": "method", "value": "POST"})
    api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/post"})
    api.action("selectRequestTab", {"tab": "body"})
    api.action("setRequestField", {"field": "bodyMode", "value": "raw"})
    body_raw = '{"widget":"gizmo","qty":3,"source":"python-test-script"}'
    api.action("setRequestField", {"field": "bodyRaw", "value": body_raw})
    api.action("selectRequestTab", {"tab": "headers"})
    api.action("addRequestHeader", {"key": "Content-Type", "value": "application/json"})
    api.action("selectRequestTab", {"tab": "params"})
    api.action("addRequestParam", {"key": TEST_PARAM_KEY, "value": "fromtab"})
    api.action("saveRequest")
    poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: ((find_item_by_id(c, item_id) or {}).get("body") or {}).get("raw") == body_raw,
    )

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
        # httpbin's /post echoes received query params under "args" —
        # proves the Params tab's rows actually reach the wire, appended
        # to the URL by internal/httpengine.buildURL.
        r.check(
            f"query param {TEST_PARAM_KEY}=fromtab reached httpbin (echoed in args)",
            f'"{TEST_PARAM_KEY}": "fromtab"' in body_text,
            body_text[:300],
        )


def test_ui_state_getter(api: ControlAPI, r: Report, collection_id: str, item_id: str) -> None:
    """GET /api/ui/state is the read-side counterpart to every ui:action
    (see CLAUDE.md): App.svelte's reportUIState mirrors the whole editor
    draft after each dispatched action, so a script can read back what
    an action did instead of screenshotting the window. Exercises a
    setter -> getter round trip for a few representative fields plus the
    main point of the feature — the result of the last sendRequest
    landing in state.response — rather than a screenshot. Populates the
    empty scratch request main() already created and saved for this test
    (see REQUEST_TEST_NAMES) with just enough — a GET — to have
    something real to send."""
    r.section("UI state getter (GET /api/ui/state)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    r.step(f"selectRequest {{id: {item_id}}}, setRequestField url, saveRequest")
    api.action("selectRequest", {"id": item_id})
    poll(api.state, lambda s: s.get("selectedItemId") == item_id)
    api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/get"})
    api.action("saveRequest")
    poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: bool((find_item_by_id(c, item_id) or {}).get("url")),
    )

    r.step("setRequestField name = 'State Getter Check'")
    api.action("setRequestField", {"field": "name", "value": "State Getter Check"})
    state = poll(api.state, lambda s: s.get("name") == "State Getter Check")
    r.check("state.name reflects the just-set field", state.get("name") == "State Getter Check", str(state.get("name")))
    r.check("state.selectedItemId matches the request being edited", state.get("selectedItemId") == item_id, str(state.get("selectedItemId")))

    r.step("selectRequestTab 'body'")
    api.action("selectRequestTab", {"tab": "body"})
    state = poll(api.state, lambda s: s.get("tab") == "body")
    r.check("state.tab reflects selectRequestTab", state.get("tab") == "body", str(state.get("tab")))

    r.step("toggleHelp  (watch: help panel opens)")
    api.action("toggleHelp")
    state = poll(api.state, lambda s: s.get("showHelp") is True)
    r.check("state.showHelp reflects toggleHelp", state.get("showHelp") is True, str(state.get("showHelp")))
    r.step("toggleHelp  (watch: closes again)")
    api.action("toggleHelp")
    state = poll(api.state, lambda s: s.get("showHelp") is False)
    r.check("state.showHelp reflects the second toggleHelp", state.get("showHelp") is False, str(state.get("showHelp")))

    r.step("sendRequest  (watch: response pane fills in)")
    api.action("sendRequest")
    state = poll(api.state, lambda s: s.get("response") is not None, timeout=10.0)
    response = state.get("response") or {}
    r.check(
        "state.response is populated with the last sendRequest's result",
        response.get("statusCode") == 200,
        str(response)[:200],
    )



# httpbin has a dedicated echo endpoint per body-carrying verb (/get,
# /put, /patch, /delete), each answering only its own verb with a real
# body — a clean, strong "the right method was actually sent" signal (a
# 200 there is otherwise unreachable). HEAD and OPTIONS have no endpoint
# of their own: HEAD is the bodyless twin of GET wherever GET is
# allowed, and httpbin answers OPTIONS everywhere (it's CORS support,
# not a per-resource verb). Both still have their own equally strong
# tell, though — neither ever carries a response body, unlike GET/POST
# at that same path — so they're tested against /get too, just checked
# for an empty body instead of a method-specific 200.
METHOD_ENDPOINT = {
    "GET": "get",
    "PUT": "put",
    "PATCH": "patch",
    "DELETE": "delete",
    "HEAD": "get",
    "OPTIONS": "get",
}


def test_http_methods(api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_ids: dict[str, str]) -> None:
    """POST is already exercised by test_execute; this covers the other
    common methods, each against its own empty scratch request main()
    already created and saved (see REQUEST_TEST_NAMES) — selected,
    populated with its method/url, saved, and sent. See METHOD_ENDPOINT
    for how each method is verified against real httpbin. Exhaustive
    method x body-mode coverage lives in Go
    (TestExecuteAllMethodsAndBodyTypes in
    internal/httpengine/executor_test.go, deterministic, no network);
    this is the thinner end-to-end slice proving it through the real UI
    + control API + a real network call."""
    r.section("HTTP methods (GET / PUT / PATCH / DELETE / HEAD / OPTIONS via the real UI + network)")

    for method in HTTP_METHODS:
        item_id = item_ids.get(method)
        if not item_id:
            r.check(f"{method} skipped — no empty scratch request for this test", False)
            continue
        endpoint = METHOD_ENDPOINT[method]
        r.step(f"selectRequest {{id: {item_id}}}  ({method}), setRequestField method/url, saveRequest")
        api.action("selectRequest", {"id": item_id})
        poll(api.state, lambda s, iid=item_id: s.get("selectedItemId") == iid)
        api.action("setRequestField", {"field": "method", "value": method})
        api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/{endpoint}"})
        api.action("saveRequest")
        collection = poll(
            lambda: api.get(f"/api/collections/{collection_id}"),
            lambda c, iid=item_id, m=method: (find_item_by_id(c, iid) or {}).get("method") == m,
        )
        saved = find_item_by_id(collection, item_id)
        r.check(f"{method} method round-tripped", (saved or {}).get("method") == method, str(saved))

        r.step("sendRequest")
        api.action("sendRequest")
        status, resp = api.post(
            "/api/execute",
            {"collectionId": collection_id, "itemId": item_id, "environmentId": environment_id},
        )
        got_200 = status == 200 and isinstance(resp, dict) and resp.get("statusCode") == 200
        if method in ("HEAD", "OPTIONS"):
            ok = got_200 and not resp.get("body")
            r.check(
                f"{method} {{{{{TEST_VAR_KEY}}}}}/{endpoint} -> 200 with an empty body "
                f"(GET/POST at that path always return one, so this proves {method} was actually sent)",
                ok,
                f"status={status} body={resp}",
            )
        else:
            r.check(
                f"{method} {{{{{TEST_VAR_KEY}}}}}/{endpoint} -> 200 (httpbin only accepts {method} on that path)",
                got_200,
                f"status={status} body={resp}",
            )


def test_file_upload(api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str) -> None:
    """Uses the committed scripts/random-sample.bin fixture — real random
    bytes, not text, so a substring check can't accidentally pass; this
    test only ever reads it, never writes or deletes it, so it's reused
    unchanged across runs (and by anyone poking at the app manually).
    Exercises both ways Freeman can send a file — a form-data file field
    alongside a plain text field, and the standalone binary body mode —
    against httpbin, decoding its data:...;base64,... response back to
    bytes each time for an exact comparison against the original file.
    Populates the empty scratch request main() already created and saved
    for this test (see REQUEST_TEST_NAMES); main()'s final phase deletes
    it along with every other test's, so this function doesn't delete it
    itself. Neither file-sending path can be driven through the native
    file *dialog* headlessly (SelectFile opens a real OS picker); the
    control API sets filePath/binaryFilePath directly instead, the same
    split as openWorkspace vs. SelectWorkspaceFolder."""
    r.section("File upload (form-data file field + binary body mode)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return
    if not RANDOM_FILE_PATH.exists():
        r.check(f"skipped — fixture not found at {RANDOM_FILE_PATH}", False)
        return
    original_bytes = RANDOM_FILE_PATH.read_bytes()
    path = str(RANDOM_FILE_PATH)

    r.step(f"selectRequest {{id: {item_id}}}, setRequestField method=POST, url, selectRequestTab 'body'")
    api.action("selectRequest", {"id": item_id})
    poll(api.state, lambda s: s.get("selectedItemId") == item_id)
    api.action("setRequestField", {"field": "method", "value": "POST"})
    api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/post"})
    api.action("selectRequestTab", {"tab": "body"})

    r.step("setRequestField bodyMode = 'form-data'")
    api.action("setRequestField", {"field": "bodyMode", "value": "form-data"})
    r.step("addRequestFormField {key: 'caption', type: 'text', value: 'from python'}")
    api.action("addRequestFormField", {"key": "caption", "type": "text", "value": "from python"})
    r.step(f"addRequestFormField {{key: 'blob', type: 'file', filePath: {path!r}}}")
    api.action("addRequestFormField", {"key": "blob", "type": "file", "filePath": path})
    r.step("saveRequest")
    api.action("saveRequest")

    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: ((find_item_by_id(c, item_id) or {}).get("body") or {}).get("mode") == "form-data",
    )
    saved = find_item_by_id(collection, item_id)
    r.check("the form-data body was saved", saved is not None, str(collection))
    if saved is None:
        return

    saved_fields = (saved.get("body") or {}).get("formFields") or []
    blob_field = next((f for f in saved_fields if f.get("key") == "blob"), None)
    r.check(
        "the file field round-tripped as type 'file' with its path",
        blob_field is not None and blob_field.get("type") == "file" and blob_field.get("filePath") == path,
        str(blob_field),
    )

    r.step("sendRequest  (watch the app: response pane should show the uploaded file echoed back)")
    api.action("sendRequest")
    status, resp = api.post(
        "/api/execute",
        {"collectionId": collection_id, "itemId": item_id, "environmentId": environment_id},
    )
    ok = status == 200 and isinstance(resp, dict) and resp.get("statusCode") == 200
    r.check("form-data file field: POST -> 200", ok, f"status={status} body={resp}")
    if ok:
        try:
            parsed = json.loads(resp.get("body", ""))
        except json.JSONDecodeError:
            parsed = {}
        received = decode_data_uri_bytes((parsed.get("files") or {}).get("blob", ""))
        r.check(
            "httpbin received the file's exact bytes",
            received == original_bytes,
            f"{len(received)} bytes back, {len(original_bytes)} expected",
        )
        r.check(
            "httpbin received the accompanying text field",
            (parsed.get("form") or {}).get("caption") == "from python",
            str(parsed.get("form")),
        )

    r.step("setRequestField bodyMode = 'binary', binaryFilePath, saveRequest")
    api.action("setRequestField", {"field": "bodyMode", "value": "binary"})
    api.action("setRequestField", {"field": "binaryFilePath", "value": path})
    api.action("saveRequest")
    # saveRequest's own SaveRequest+GetCollection round trip can still be
    # in flight when its 204 comes back (see poll()'s docstring) — without
    # waiting for the switch to 'binary' to actually land on disk, the
    # /api/execute check below can race it and still see the old
    # 'form-data' body. The form-data checkpoint above doesn't need this
    # explicitly: it's preceded by its own poll() at the scratch-request
    # creation step, which already absorbs the same latency.
    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: ((find_item_by_id(c, item_id) or {}).get("body") or {}).get("mode") == "binary",
    )
    r.check(
        "body.mode round-tripped to 'binary' before sending",
        ((find_item_by_id(collection, item_id) or {}).get("body") or {}).get("mode") == "binary",
        str((find_item_by_id(collection, item_id) or {}).get("body")),
    )

    r.step("sendRequest")
    api.action("sendRequest")
    status, resp = api.post(
        "/api/execute",
        {"collectionId": collection_id, "itemId": item_id, "environmentId": environment_id},
    )
    ok = status == 200 and isinstance(resp, dict) and resp.get("statusCode") == 200
    r.check("binary body mode: POST -> 200", ok, f"status={status} body={resp}")
    if ok:
        try:
            parsed = json.loads(resp.get("body", ""))
        except json.JSONDecodeError:
            parsed = {}
        received = decode_data_uri_bytes(parsed.get("data", ""))
        r.check(
            "httpbin received the file's exact bytes as the whole body",
            received == original_bytes,
            f"{len(received)} bytes back, {len(original_bytes)} expected",
        )
        r.check(
            "Content-Type detected as application/octet-stream (random bytes, no recognizable extension/content)",
            (parsed.get("headers") or {}).get("Content-Type") == "application/octet-stream",
            str(parsed.get("headers")),
        )


def test_large_response_truncation(
    api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str
) -> None:
    """Points the empty scratch request main() made for this test at a
    local server (spun up just for this function — nothing public
    reliably returns a body over httpengine.LargeResponseThreshold on
    demand) that returns a fixed 1.5 MB body, and checks the desktop app
    blanks it out of the response it mirrors, pointing bodyFile at the
    on-disk cache instead."""
    r.section("Large response truncation")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    big_body = b'{"padding":"' + b"x" * (1_500_000) + b'"}'

    class BigHandler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):  # noqa: N802 - http.server's naming convention
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(big_body)))
            self.end_headers()
            self.wfile.write(big_body)

        def log_message(self, format, *args):  # noqa: A002 - stdlib signature
            pass  # quiet — this script has its own reporting

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), BigHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    try:
        port = server.server_address[1]
        r.step(f"selectRequest {{id: {item_id}}}, setRequestField url -> a local server returning {len(big_body)} bytes, saveRequest")
        api.action("selectRequest", {"id": item_id})
        poll(api.state, lambda s: s.get("selectedItemId") == item_id)
        api.action("setRequestField", {"field": "method", "value": "GET"})
        api.action("setRequestField", {"field": "url", "value": f"http://127.0.0.1:{port}/"})
        api.action("saveRequest")

        r.step("sendRequest  (watch the app: a 'too large to show' callout should appear, not the raw body)")
        api.action("sendRequest")
        state = poll(api.state, lambda s: (s.get("response") or {}).get("truncated") is True, timeout=15.0)
        resp = state.get("response") or {}
        r.check("response.truncated is true for a body over the threshold", resp.get("truncated") is True, str(resp))
        r.check("response.body was left empty, not inlined", resp.get("body") == "", f"{len(resp.get('body') or '')} bytes")
        r.check("response.sizeBytes reflects the real body size", resp.get("sizeBytes") == len(big_body), str(resp.get("sizeBytes")))
        body_file = resp.get("bodyFile") or ""
        r.check(
            "response.bodyFile points at the workspace cache, not a stray temp file",
            os.path.normpath(body_file).startswith(os.path.normpath(os.path.join(state.get("workspaceRoot") or "", ".cache", "responses"))),
            repr(body_file),
        )
        r.check("the cache body file actually exists on disk", os.path.exists(body_file), body_file)

        state_json = json.dumps(state)
        r.check(
            "GET /api/ui/state itself stays small (the large body isn't mirrored)",
            len(state_json) < 10_000,
            f"{len(state_json)} bytes",
        )

        # openResponseCacheExternally/copyResponseCachePath/
        # openResponseCacheInFileExplorer aren't exercised here — each has
        # a real, disruptive OS side effect (launching an app, touching
        # the clipboard, opening a file manager window) with nothing
        # meaningful to assert over the control API beyond "the call
        # didn't error", not worth triggering on every automated run.
    finally:
        server.shutdown()
        thread.join(timeout=5)


def test_response_cache(api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str) -> None:
    """Sends the empty scratch request main() made for this test, then
    confirms its response survives navigating away and back (newRequest
    stands in for closing and reopening the app — the cache is on disk),
    that the body view autodetects as JSON and setResponseView flips
    pretty/raw, that an image/png response autodetects as image and its
    cache file gets a .png extension, that a brotli-encoded response is
    decoded and an XML one autodetects + formats, that setResponseTab
    flips between the body and the response headers, that the response
    pane's "..." menu toggles, and that both clearCachedResponse (per
    request) and clearResponseCache (whole workspace) really delete the
    entry, not just the in-memory copy.

    Those clears only touch the open workspace's own .cache/responses —
    here that's the disposable temp workspace, so this leaves no trace
    like everything else."""
    r.section("Response cache (persisted per-request, reloaded on reselect)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    r.step(f"selectRequest {{id: {item_id}}}, setRequestField method/url, saveRequest, sendRequest")
    api.action("selectRequest", {"id": item_id})
    poll(api.state, lambda s: s.get("selectedItemId") == item_id)
    api.action("setRequestField", {"field": "method", "value": "GET"})
    api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/get?probe=cache"})
    api.action("saveRequest")
    api.action("sendRequest")
    state = poll(api.state, lambda s: (s.get("response") or {}).get("statusCode") == 200, timeout=15.0)
    original_body = (state.get("response") or {}).get("body", "")
    r.check("sendRequest produced a 200 response", (state.get("response") or {}).get("statusCode") == 200, str(state.get("response")))
    r.check("the response has a body to cache", original_body != "", repr(original_body[:80]))

    r.step("newRequest  (stands in for closing and reopening the app — the cache is on disk)")
    api.action("newRequest")
    poll(api.state, lambda s: s.get("selectedItemId") is None)

    r.step(f"selectRequest {{id: {item_id}}}  (watch: the response pane should repopulate, not sit blank)")
    api.action("selectRequest", {"id": item_id})
    state = poll(api.state, lambda s: (s.get("response") or {}).get("body") == original_body and original_body != "")
    r.check(
        "reselecting the request reloaded its last response from the on-disk cache",
        (state.get("response") or {}).get("body") == original_body,
        f"got {(state.get('response') or {}).get('body', '')[:80]!r}",
    )

    # httpbin's /get returns application/json, so the body view should
    # autodetect as JSON and default to the pretty (highlighted) view.
    r.check("responseKind autodetected as json", state.get("responseKind") == "json", str(state.get("responseKind")))
    r.check("responseView defaults to pretty", state.get("responseView") == "pretty", str(state.get("responseView")))
    r.step("setResponseView {view: 'raw'}  (watch: the body should drop the highlighting/indentation)")
    api.action("setResponseView", {"view": "raw"})
    state = poll(api.state, lambda s: s.get("responseView") == "raw")
    r.check("state.responseView reflects setResponseView 'raw'", state.get("responseView") == "raw", str(state.get("responseView")))
    r.step("setResponseView {view: 'pretty'}  (back to the formatted view)")
    api.action("setResponseView", {"view": "pretty"})
    state = poll(api.state, lambda s: s.get("responseView") == "pretty")
    r.check("state.responseView reflects setResponseView 'pretty'", state.get("responseView") == "pretty", str(state.get("responseView")))

    r.step("setRequestField url -> an image endpoint, saveRequest, sendRequest  (watch: the pane should show the image, not raw bytes)")
    api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/image/png"})
    api.action("saveRequest")
    api.action("sendRequest")
    state = poll(
        api.state,
        lambda s: (s.get("response") or {}).get("statusCode") == 200 and s.get("responseKind") == "image",
        timeout=15.0,
    )
    r.check("responseKind autodetected as image for an image/png response", state.get("responseKind") == "image", str(state.get("responseKind")))
    png_path = os.path.join(state.get("workspaceRoot") or "", ".cache", "responses", f"{item_id}.body.png")
    r.check("the cached body file got a .png extension from its Content-Type", os.path.exists(png_path), png_path)

    r.step("setRequestField url -> /brotli, saveRequest, sendRequest  (Content-Encoding: br, decoded to JSON)")
    api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/brotli"})
    api.action("saveRequest")
    api.action("sendRequest")
    state = poll(
        api.state,
        lambda s: (s.get("response") or {}).get("statusCode") == 200 and s.get("responseKind") == "json",
        timeout=15.0,
    )
    r.check(
        "a brotli-encoded response is decoded (kind autodetects as json, not garbled text)",
        state.get("responseKind") == "json",
        f"kind={state.get('responseKind')}",
    )

    r.step("setRequestField url -> /xml, saveRequest, sendRequest, then setResponseView raw/pretty")
    api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/xml"})
    api.action("saveRequest")
    api.action("sendRequest")
    state = poll(api.state, lambda s: (s.get("response") or {}).get("statusCode") == 200 and s.get("responseKind") == "xml", timeout=15.0)
    r.check("responseKind autodetected as xml", state.get("responseKind") == "xml", str(state.get("responseKind")))
    api.action("setResponseView", {"view": "raw"})
    state = poll(api.state, lambda s: s.get("responseView") == "raw")
    r.check("setResponseView 'raw' works for an XML response too", state.get("responseView") == "raw", str(state.get("responseView")))
    api.action("setResponseView", {"view": "pretty"})
    poll(api.state, lambda s: s.get("responseView") == "pretty")

    r.step("setResponseTab {tab: 'headers'}  (watch: the pane should show the response headers, not the body)")
    api.action("setResponseTab", {"tab": "headers"})
    state = poll(api.state, lambda s: s.get("responseTab") == "headers")
    r.check("state.responseTab reflects setResponseTab 'headers'", state.get("responseTab") == "headers", str(state.get("responseTab")))
    r.check(
        "the response's headers are in state for that panel to show",
        bool((state.get("response") or {}).get("headers")),
        str(list(((state.get("response") or {}).get("headers") or {}).keys())),
    )
    api.action("setResponseTab", {"tab": "body"})
    state = poll(api.state, lambda s: s.get("responseTab") == "body")
    r.check("state.responseTab reflects setResponseTab 'body'", state.get("responseTab") == "body", str(state.get("responseTab")))

    r.step('toggleResponseActionsMenu  (watch: the response pane\'s "..." menu should open)')
    api.action("toggleResponseActionsMenu")
    state = poll(api.state, lambda s: s.get("showResponseActionsMenu") is True)
    r.check(
        "state.showResponseActionsMenu reflects toggleResponseActionsMenu",
        state.get("showResponseActionsMenu") is True,
        str(state.get("showResponseActionsMenu")),
    )
    api.action("toggleResponseActionsMenu")
    state = poll(api.state, lambda s: s.get("showResponseActionsMenu") is False)
    r.check(
        "state.showResponseActionsMenu reflects the second toggleResponseActionsMenu",
        state.get("showResponseActionsMenu") is False,
        str(state.get("showResponseActionsMenu")),
    )

    r.step('clearCachedResponse  (the "..." menu\'s per-request clear — watch: the pane should blank immediately)')
    api.action("clearCachedResponse")
    state = poll(api.state, lambda s: not (s.get("response") or {}).get("body"))
    r.check(
        "clearCachedResponse blanks the pane right away",
        not (state.get("response") or {}).get("body"),
        str(state.get("response")),
    )
    api.action("newRequest")
    poll(api.state, lambda s: s.get("selectedItemId") is None)
    api.action("selectRequest", {"id": item_id})
    state = poll(api.state, lambda s: s.get("selectedItemId") == item_id)
    r.check(
        "and it stays gone on reselect — the cache entry was deleted, not just the in-memory copy",
        not (state.get("response") or {}).get("body"),
        str(state.get("response")),
    )

    # Re-send so there's something for the whole-workspace clear to remove.
    r.step("sendRequest again, then clearResponseCache + newRequest + selectRequest  (watch: blank again)")
    api.action("sendRequest")
    poll(api.state, lambda s: (s.get("response") or {}).get("statusCode") == 200, timeout=15.0)
    api.action("clearResponseCache")
    api.action("newRequest")
    poll(api.state, lambda s: s.get("selectedItemId") is None)
    api.action("selectRequest", {"id": item_id})
    state = poll(api.state, lambda s: s.get("selectedItemId") == item_id)
    r.check(
        "the response is gone after clearResponseCache — the whole-workspace cache really was cleared",
        not (state.get("response") or {}).get("body"),
        str(state.get("response")),
    )


def test_delete_request(api: ControlAPI, r: Report, collection_id: str) -> None:
    """Fully self-contained: creates DELETE_TEST_NAME and deletes it
    again within this function — this is what actually exercises
    deleteRequest, not a cleanup step; the temp workspace gets discarded
    wholesale regardless of what's left in it."""
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
    """Adds TEST_VAR_KEY, left in place for the rest of the run — every
    later section that needs {{pyTestBase}} substituted relies on it
    still being there, and the temp workspace is discarded wholesale at
    the end regardless. SCRATCH_VAR_KEY is fully self-contained (added,
    updated, and removed again right here), proving add/set/remove all
    work, independent of that."""
    r.section("Environment editor (settings window's Environments tab / *Variable / saveEnvironment)")

    def env_var(state: dict, key: str) -> Optional[dict]:
        variables = (state.get("environment") or {}).get("variables") or []
        return next((v for v in variables if v.get("key") == key), None)

    r.step("toggleSettings, selectSettingsTab 'environments'  (watch: the settings window opens on that tab)")
    api.action("toggleSettings")
    api.action("selectSettingsTab", {"tab": "environments"})

    # Polling state() after each mutation (not just once at the very
    # end) closes a real race: firing the next action before this one's
    # effect is confirmed in the frontend's own state — not just emitted
    # — let a rapid burst of preceding actions (main()'s Phase 1 creates
    # ten requests right before this test runs) occasionally clobber an
    # add/remove here.
    r.step(f"addEnvironmentVariable {{key: {TEST_VAR_KEY!r}, value: {TEST_VAR_VALUE!r}}}")
    api.action("addEnvironmentVariable", {"key": TEST_VAR_KEY, "value": TEST_VAR_VALUE})
    poll(api.state, lambda s: env_var(s, TEST_VAR_KEY) is not None)

    r.step(f"addEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}, secret: true}}  (scratch)")
    api.action("addEnvironmentVariable", {"key": SCRATCH_VAR_KEY, "value": "temp", "secret": True})
    poll(api.state, lambda s: env_var(s, SCRATCH_VAR_KEY) is not None)
    r.step(f"setEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}, value: 'updated'}}")
    api.action("setEnvironmentVariable", {"key": SCRATCH_VAR_KEY, "value": "updated"})
    poll(api.state, lambda s: (env_var(s, SCRATCH_VAR_KEY) or {}).get("value") == "updated")
    r.step(f"removeEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}}}")
    api.action("removeEnvironmentVariable", {"key": SCRATCH_VAR_KEY})
    poll(api.state, lambda s: env_var(s, SCRATCH_VAR_KEY) is None)

    r.step("saveEnvironment")
    api.action("saveEnvironment")

    r.step("toggleSettings  (watch: the settings window should close)")
    api.action("toggleSettings")

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

    # --- new / rename / delete a whole environment ---
    scratch_env_name = "Python Scratch Environment"
    r.step("newEnvironment  (watch: a 'New environment' is created and selected)")
    api.action("newEnvironment")
    state = poll(
        api.state,
        lambda s: (s.get("environment") or {}).get("id") not in (None, environment_id)
        and (s.get("environment") or {}).get("name") == "New environment",
    )
    scratch_env_id = (state.get("environment") or {}).get("id")
    r.check("newEnvironment created and switched to a fresh environment", bool(scratch_env_id) and scratch_env_id != environment_id, str(scratch_env_id))

    r.step(f"setEnvironmentField {{field: 'name', value: {scratch_env_name!r}}}, saveEnvironment")
    api.action("setEnvironmentField", {"field": "name", "value": scratch_env_name})
    poll(api.state, lambda s: (s.get("environment") or {}).get("name") == scratch_env_name)
    api.action("saveEnvironment")
    envs = poll(lambda: api.get("/api/environments"), lambda es: any(e.get("name") == scratch_env_name for e in es))
    r.check("the renamed environment shows up in GET /api/environments", any(e.get("name") == scratch_env_name for e in envs), str(envs))

    r.step("deleteEnvironment  (no id — deletes the one in the editor)")
    api.action("deleteEnvironment")
    envs = poll(lambda: api.get("/api/environments"), lambda es: all(e.get("id") != scratch_env_id for e in es))
    r.check("the scratch environment is gone after deleteEnvironment", all(e.get("id") != scratch_env_id for e in envs), str(envs))

    r.step(f"selectEnvironment {{id: {environment_id}}}  (restore the one with {TEST_VAR_KEY} as active)")
    api.action("selectEnvironment", {"id": environment_id})
    poll(api.state, lambda s: (s.get("environment") or {}).get("id") == environment_id)


def test_settings_window(api: ControlAPI, r: Report) -> None:
    """The settings window: a Workspace tab (current folder + Change…,
    which just re-runs openWorkspace) and an Environments tab (covered by
    test_environment_editor instead, since it needs a real environment_id
    to work with). This covers the window/tab chrome itself plus
    openWorkspace's round trip — a real reload, not just a dialog
    stand-in, which is the whole point of the control API having it."""
    r.section("Settings window (toggleSettings / selectSettingsTab / openWorkspace)")

    r.step("toggleSettings  (watch: the settings window should open)")
    api.action("toggleSettings")
    state = poll(api.state, lambda s: s.get("showSettings") is True)
    r.check("state.showSettings reflects toggleSettings", state.get("showSettings") is True, str(state.get("showSettings")))
    # settingsTab is sticky across opens (same as the request editor's own
    # tab) rather than resetting — set it explicitly rather than assuming
    # whichever tab an earlier section left it on.
    r.step("selectSettingsTab {tab: 'workspace'}")
    api.action("selectSettingsTab", {"tab": "workspace"})
    state = poll(api.state, lambda s: s.get("settingsTab") == "workspace")
    r.check("state.settingsTab reflects selectSettingsTab", state.get("settingsTab") == "workspace", str(state.get("settingsTab")))
    root = state.get("workspaceRoot")
    r.check("state.workspaceRoot is a non-empty path", bool(root), str(root))

    r.step("selectSettingsTab {tab: 'environments'}")
    api.action("selectSettingsTab", {"tab": "environments"})
    state = poll(api.state, lambda s: s.get("settingsTab") == "environments")
    r.check("state.settingsTab reflects selectSettingsTab", state.get("settingsTab") == "environments", str(state.get("settingsTab")))

    r.step("selectSettingsTab {tab: 'workspace'}")
    api.action("selectSettingsTab", {"tab": "workspace"})
    poll(api.state, lambda s: s.get("settingsTab") == "workspace")

    r.step("toggleSettings  (watch: it should close again)")
    api.action("toggleSettings")
    state = poll(api.state, lambda s: s.get("showSettings") is False)
    r.check("state.showSettings reflects the second toggleSettings", state.get("showSettings") is False, str(state.get("showSettings")))

    if not root:
        return
    r.step(f"openWorkspace {{path: {root!r}}}  (re-open the same workspace — a real reload, not just a dialog stand-in)")
    api.action("openWorkspace", {"path": root})
    state = poll(api.state, lambda s: s.get("workspaceRoot") == root)
    r.check("state.workspaceRoot still matches after re-opening it", state.get("workspaceRoot") == root, str(state.get("workspaceRoot")))


def test_layout_comfort(api: ControlAPI, r: Report) -> None:
    """The resizable sidebar/log splitters are pure layout, not app data —
    there's no saved/executed request to check here, just that the
    setters round-trip (with clamping) and the two collapse toggles
    work. Restores every value to what it found before it started,
    rather than assuming defaults, since it runs against whatever state
    earlier sections already left."""
    r.section("Layout comfort (splitters, collapsible panes)")

    state = api.state()
    original_sidebar_width = state.get("sidebarWidth")
    original_status_bar_height = state.get("statusBarHeight")
    original_show_log = state.get("showControlApiLog")
    original_tab = state.get("tab")
    original_pane_collapsed = state.get("requestPaneCollapsed")

    try:
        r.step("setSidebarWidth {px: 400}")
        api.action("setSidebarWidth", {"px": 400})
        state = poll(api.state, lambda s: s.get("sidebarWidth") == 400)
        r.check("state.sidebarWidth reflects setSidebarWidth", state.get("sidebarWidth") == 400, str(state.get("sidebarWidth")))

        r.step("setSidebarWidth {px: 99999}  (clamped to a sane max)")
        api.action("setSidebarWidth", {"px": 99999})
        state = poll(api.state, lambda s: s.get("sidebarWidth") != 400)
        r.check(
            "state.sidebarWidth clamped rather than accepting an unbounded value",
            isinstance(state.get("sidebarWidth"), (int, float)) and state.get("sidebarWidth") < 99999,
            str(state.get("sidebarWidth")),
        )

        r.step("setStatusBarHeight {px: 250}")
        api.action("setStatusBarHeight", {"px": 250})
        state = poll(api.state, lambda s: s.get("statusBarHeight") == 250)
        r.check(
            "state.statusBarHeight reflects setStatusBarHeight", state.get("statusBarHeight") == 250, str(state.get("statusBarHeight"))
        )

        r.step("toggleControlApiLog  (watch: the log panel should collapse)")
        api.action("toggleControlApiLog")
        state = poll(api.state, lambda s: s.get("showControlApiLog") != original_show_log)
        r.check(
            "state.showControlApiLog reflects toggleControlApiLog",
            state.get("showControlApiLog") != original_show_log,
            str(state.get("showControlApiLog")),
        )
        r.step("toggleControlApiLog  (watch: it should expand again)")
        api.action("toggleControlApiLog")
        state = poll(api.state, lambda s: s.get("showControlApiLog") == original_show_log)
        r.check(
            "state.showControlApiLog reflects the second toggleControlApiLog",
            state.get("showControlApiLog") == original_show_log,
            str(state.get("showControlApiLog")),
        )

        r.step("selectRequestTab {tab: 'body'}  (deterministic: switches and expands, never toggles)")
        api.action("selectRequestTab", {"tab": "body"})
        state = poll(api.state, lambda s: s.get("tab") == "body" and s.get("requestPaneCollapsed") is False)
        r.check(
            "state.tab is 'body' and requestPaneCollapsed is False after selectRequestTab",
            state.get("tab") == "body" and state.get("requestPaneCollapsed") is False,
            str(state),
        )
        r.step("selectRequestTab {tab: 'body'}  (the same tab again — still expanded, not a toggle)")
        api.action("selectRequestTab", {"tab": "body"})
        state = poll(api.state, lambda s: s.get("tab") == "body")
        r.check(
            "selectRequestTab on the same tab doesn't collapse it — only toggleRequestPane/clicking does",
            state.get("requestPaneCollapsed") is False,
            str(state.get("requestPaneCollapsed")),
        )

        r.step("toggleRequestPane  (watch: the Body table collapses, the response pane grows)")
        api.action("toggleRequestPane")
        state = poll(api.state, lambda s: s.get("requestPaneCollapsed") is True)
        r.check(
            "state.requestPaneCollapsed reflects toggleRequestPane", state.get("requestPaneCollapsed") is True, str(state.get("requestPaneCollapsed"))
        )
        r.step("toggleRequestPane  (watch: it should expand again)")
        api.action("toggleRequestPane")
        state = poll(api.state, lambda s: s.get("requestPaneCollapsed") is False)
        r.check(
            "state.requestPaneCollapsed reflects the second toggleRequestPane",
            state.get("requestPaneCollapsed") is False,
            str(state.get("requestPaneCollapsed")),
        )
    finally:
        r.step("restoring sidebarWidth/statusBarHeight/tab/requestPaneCollapsed to what this test found them as")
        if isinstance(original_sidebar_width, (int, float)):
            api.action("setSidebarWidth", {"px": original_sidebar_width})
        if isinstance(original_status_bar_height, (int, float)):
            api.action("setStatusBarHeight", {"px": original_status_bar_height})
        if original_tab in ("params", "headers", "body"):
            api.action("selectRequestTab", {"tab": original_tab})  # also forces expanded
        if original_pane_collapsed:
            api.action("toggleRequestPane")  # re-collapse if that's how it was found


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

    print(f"Freeman control API test — base URL {args.base_url}, delay {args.delay}s")

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
