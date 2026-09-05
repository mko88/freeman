"""Reading a response back: the state getter, truncation, the on-disk cache."""

from __future__ import annotations

import http.server
import json
import os
import threading

from .. import ControlAPI, Report, poll
from ..fixtures import TEST_VAR_KEY, REQUEST_TEST_NAMES, find_item_by_id


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

    # The "..." menu's clipboard action. Only fired, not asserted — what
    # it produces lands on the system clipboard, out of this suite's
    # reach — but firing it proves the action resolves the cache path
    # rather than throwing, which is the part that used to be reachable
    # with an unvalidated itemID. Its two siblings that launch an editor
    # or a file manager stay unfired on purpose; see consistency.py's
    # UNDRIVEABLE.
    r.step('copyResponseCachePath  (the "..." menu\'s Copy path — writes to the clipboard)')
    api.action("copyResponseCachePath")

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
