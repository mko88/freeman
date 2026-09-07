"""The request editor: fields, tabs, params, headers, auth, delete."""

from __future__ import annotations

from .. import ControlAPI, Report, poll
from ..fixtures import (
    TEST_REQUEST_NAME,
    DELETE_TEST_NAME,
    TEST_HEADER_KEY,
    SCRATCH_HEADER_KEY,
    TEST_PARAM_KEY,
    SCRATCH_PARAM_KEY,
    FORM_FIELD_KEY,
    SCRATCH_FIELD_KEY,
    TEST_VAR_KEY,
    REQUEST_TEST_NAMES,
    find_item,
    find_header_index,
)


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
    # Added wrong on purpose, then corrected by position — so the
    # "value is 'gizmo'" assertion below covers setRequestFormField too,
    # the same way the params section covers setRequestParam.
    r.step(f"addRequestFormField {{key: {FORM_FIELD_KEY!r}, value: 'wrong'}}")
    api.action("addRequestFormField", {"key": FORM_FIELD_KEY, "value": "wrong"})
    r.step("setRequestFormField {index: 0, value: 'gizmo'}  (edit the first row by position)")
    api.action("setRequestFormField", {"index": 0, "value": "gizmo"})
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

    # How the raw editor colours what's typed. A view preference, so the
    # only thing to verify is that state mirrors it — the highlighting
    # itself is markup, which this suite can't see. Left on 'auto' at the
    # end so nothing downstream inherits a pinned language.
    for language in ("json", "xml", "plain", "auto"):
        r.step(f"selectBodyLanguage {{language: {language!r}}}")
        api.action("selectBodyLanguage", {"language": language})
        state = poll(api.state, lambda s, l=language: s.get("bodyLanguage") == l)
        r.check(
            f"state.bodyLanguage reflects selectBodyLanguage {language!r}",
            state.get("bodyLanguage") == language,
            str(state.get("bodyLanguage")),
        )

    r.step("selectRequestTab 'headers'")
    api.action("selectRequestTab", {"tab": "headers"})

    r.step("addRequestHeader {key: 'Content-Type', value: 'application/json'}")
    api.action("addRequestHeader", {"key": "Content-Type", "value": "application/json"})

    # Same trick as the form field above: added wrong, corrected by
    # position, so the "value is '1'" assertion covers setRequestHeader.
    # Index 1 because Content-Type went in first.
    r.step(f"addRequestHeader {{key: {TEST_HEADER_KEY!r}, value: '0'}}")
    api.action("addRequestHeader", {"key": TEST_HEADER_KEY, "value": "0"})
    r.step("setRequestHeader {index: 1, value: '1'}  (edit an existing row by position)")
    api.action("setRequestHeader", {"index": 1, "value": "1"})

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

    # How the request is sent, rather than what is sent — the Options
    # tab. Defaults are follow redirects and share the cookie jar, so an
    # untouched request carries no options key at all.
    r.step("selectRequestTab 'options'")
    api.action("selectRequestTab", {"tab": "options"})
    state = poll(api.state, lambda s: s.get("tab") == "options")
    r.check("state.tab reflects selectRequestTab 'options'", state.get("tab") == "options", str(state.get("tab")))
    defaults = {
        "followRedirects": True,
        "maxRedirects": 0,
        "storeCookies": True,
        "timeoutMs": 0,
        "skipTlsVerify": False,
        "clientCertFile": "",
        "clientCertKeyFile": "",
    }
    r.check("options start at the defaults", (state.get("options") or {}) == defaults, str(state.get("options")))

    changed = {
        "followRedirects": False,
        "maxRedirects": 3,
        "storeCookies": False,
        "timeoutMs": 2500,
        "skipTlsVerify": True,
        "clientCertFile": "/tmp/client.pem",
        "clientCertKeyFile": "/tmp/client.key",
    }
    r.step("setRequestOption for every field, saveRequest")
    for field, value in changed.items():
        api.action("setRequestOption", {"field": field, "value": value})
    state = poll(api.state, lambda s: (s.get("options") or {}) == changed)
    r.check("state.options reflects setRequestOption", (state.get("options") or {}) == changed, str(state.get("options")))
    api.action("saveRequest")
    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: (find_item(c, TEST_REQUEST_NAME) or {}).get("options") is not None,
    )
    saved_options = (find_item(collection, TEST_REQUEST_NAME) or {}).get("options") or {}
    # Go omits the zero-valued keys, so compare only what was set to
    # something non-zero — the rest is absent by design.
    r.check(
        "options round-tripped to the collection file",
        all(saved_options.get(k) == v for k, v in changed.items() if v not in (0, "", False)),
        str(saved_options),
    )

    r.step("setRequestOption back to the defaults, saveRequest  (the key should disappear again)")
    for field, value in defaults.items():
        api.action("setRequestOption", {"field": field, "value": value})
    api.action("saveRequest")
    collection = poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c: "options" not in (find_item(c, TEST_REQUEST_NAME) or {"options": 1}),
    )
    r.check(
        "an all-default request carries no options key",
        "options" not in (find_item(collection, TEST_REQUEST_NAME) or {}),
        str(find_item(collection, TEST_REQUEST_NAME)),
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
