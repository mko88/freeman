"""Sending requests: the happy path, every HTTP method, file uploads."""

from __future__ import annotations

import json

from .. import ControlAPI, Report, poll
from ..fixtures import (
    TEST_PARAM_KEY,
    TEST_VAR_KEY,
    HTTP_METHODS,
    REQUEST_TEST_NAMES,
    RANDOM_FILE_PATH,
    find_item_by_id,
    decode_data_uri_bytes,
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
        r.check("response statusCode is 200 (the test server answered)", resp.get("statusCode") == 200, str(resp))
        body_text = resp.get("body", "")
        r.check(
            "{{pyTestBase}} was substituted into the URL before sending",
            '"url"' in body_text,
            body_text[:200],
        )
        r.check("posted body round-tripped through the server", "python-test-script" in body_text, body_text[:200])
        # /post echoes received query params under "args" — proves the
        # Params tab's rows actually reach the wire, appended to the URL
        # by internal/httpengine.buildURL.
        r.check(
            f"query param {TEST_PARAM_KEY}=fromtab reached the server (echoed in args)",
            f'"{TEST_PARAM_KEY}": "fromtab"' in body_text,
            body_text[:300],
        )


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
    for how each method is verified against the local test server (see
    control_api.server: /get, /post and friends answer only their own
    method, so a 405 is what would show a method never went out).
    Exhaustive
    method x body-mode coverage lives in Go
    (TestExecuteAllMethodsAndBodyTypes in
    internal/httpengine/executor_test.go); this is the thinner end-to-end
    slice proving it through the real UI + control API + a real socket."""
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
                f"{method} {{{{{TEST_VAR_KEY}}}}}/{endpoint} -> 200 (that path only accepts {method})",
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
    against the local test server, decoding its data:...;base64,...
    response back to bytes each time for an exact comparison against the
    original file.
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
            "the server received the file's exact bytes",
            received == original_bytes,
            f"{len(received)} bytes back, {len(original_bytes)} expected",
        )
        r.check(
            "the server received the accompanying text field",
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
            "the server received the file's exact bytes as the whole body",
            received == original_bytes,
            f"{len(received)} bytes back, {len(original_bytes)} expected",
        )
        r.check(
            "Content-Type detected as application/octet-stream (random bytes, no recognizable extension/content)",
            (parsed.get("headers") or {}).get("Content-Type") == "application/octet-stream",
            str(parsed.get("headers")),
        )
