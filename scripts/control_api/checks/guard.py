"""Cross-origin / content-type refusals (internal/httpapi/guard.go)."""

from __future__ import annotations

from .. import ControlAPI, Report


def test_api_guard(api: ControlAPI, r: Report) -> None:
    """Regression cover for the CSRF hole internal/httpapi/guard.go
    closes. Binding to loopback keeps other machines out, but not the
    browser the user already has open: any page could reach the control
    API with a CORS "simple request" — no preflight, reply unreadable,
    write still applied. That was enough to exfiltrate secrets (point a
    request at an attacker's URL, put {{apiToken}} in its body, send it).

    Every check here sends a shape a browser *can* produce cross-origin
    and asserts it's refused. Nothing that would mutate state is ever
    expected to run, so this leaves no trace even if the guard were
    removed — the payloads below are toggles that cancel out, and the
    positive case is a plain GET /api/health.

    Go-level coverage of the same rules (including the same-origin
    browser case freeman-server depends on) is in
    internal/httpapi/guard_test.go."""
    r.section("API guard (cross-origin / content-type refusals — internal/httpapi/guard.go)")

    evil = "https://evil.example"
    toggle = {"action": "toggleHelp", "payload": {}}

    r.step("POST /api/ui/action with Content-Type: text/plain  (the no-preflight simple-request shape)")
    status, body = api.raw("POST", "/api/ui/action", toggle, content_type="text/plain")
    r.check(
        "text/plain body refused with 415, not parsed as JSON",
        status == 415,
        f"status={status} body={body}",
    )

    r.step("POST /api/ui/action with Content-Type: application/x-www-form-urlencoded")
    status, body = api.raw("POST", "/api/ui/action", toggle, content_type="application/x-www-form-urlencoded")
    r.check("form-encoded body refused with 415", status == 415, f"status={status} body={body}")

    r.step(f"POST /api/ui/action with Origin: {evil}  (correct content type, foreign origin)")
    status, body = api.raw("POST", "/api/ui/action", toggle, extra_headers={"Origin": evil})
    r.check(
        "a foreign Origin is refused with 403 even with the right content type",
        status == 403,
        f"status={status} body={body}",
    )

    r.step(f"GET /api/environments with Origin: {evil}  (reads are guarded too)")
    status, body = api.raw("GET", "/api/environments", extra_headers={"Origin": evil})
    r.check("a foreign Origin is refused on reads", status == 403, f"status={status} body={body}")

    r.step("GET /api/workspace with Sec-Fetch-Site: cross-site")
    status, body = api.raw("GET", "/api/workspace", extra_headers={"Sec-Fetch-Site": "cross-site"})
    r.check("Sec-Fetch-Site cross-site is refused", status == 403, f"status={status} body={body}")

    r.step("GET /api/workspace with Sec-Fetch-Site: same-site  (same site is still a different origin)")
    status, body = api.raw("GET", "/api/workspace", extra_headers={"Sec-Fetch-Site": "same-site"})
    r.check("Sec-Fetch-Site same-site is refused", status == 403, f"status={status} body={body}")

    r.step("POST /api/codegen with Content-Type: application/json; charset=utf-8  (a real client's shape)")
    status, body = api.raw(
        "POST",
        "/api/codegen",
        {"item": {"method": "GET", "url": "https://example.com"}, "environmentId": "", "format": "bash"},
        content_type="application/json; charset=utf-8",
    )
    r.check(
        "a charset parameter on the content type is still accepted",
        status == 200 and isinstance(body, dict) and body.get("code", "").startswith("#!/usr/bin/env bash"),
        f"status={status} body={body}",
    )

    r.step("GET /api/health with no browser headers  (this script's own shape — must still pass)")
    status, body = api.raw("GET", "/api/health")
    r.check(
        "a plain scripted request is untouched by the guard",
        status == 200 and body == {"status": "ok"},
        f"status={status} body={body}",
    )


def test_action_validation(api: ControlAPI, r: Report) -> None:
    """POST /api/ui/action refuses what the dispatcher couldn't act on,
    rather than answering 204 and doing nothing.

    Dispatching is fire-and-forget — the route emits a Wails event and
    answers without waiting — so an ignored action used to be
    indistinguishable from a performed one. That is a bad shape for an
    API whose callers are scripts and agents: the two least able to
    notice that nothing happened. The case that found it was a payload
    sent beside "action" instead of inside it, which reads as the action
    being broken.

    Validation is against the catalogue the frontend reports (see
    agentdocs.Catalog.Validate), so it can't describe an app that doesn't
    exist. Nothing here changes any state: every refusal is expected to
    stop before it dispatches, and the one accepted call is a
    selectRequest of whatever is already selected."""
    r.section("Action validation (POST /api/ui/action refuses what it can't act on)")

    for name, body in (
        ("an action nobody has", {"action": "noSuchAction"}),
        ("a known action with its required payload missing", {"action": "selectRequest"}),
        # The mistake this exists for: {"action": ..., "id": ...} rather
        # than {"action": ..., "payload": {"id": ...}}. It reaches the
        # route as an action with no payload at all.
        ("the payload's fields sent beside 'action' instead of inside it", {"action": "selectRequest", "id": "r_x"}),
        ("a null where a value was required", {"action": "selectRequest", "payload": {"id": None}}),
    ):
        r.step(f"POST /api/ui/action {body}")
        status, resp = api.raw("POST", "/api/ui/action", body)
        r.check(
            f"{name} is refused with 400 and an error that says why",
            status == 400 and isinstance(resp, dict) and bool(resp.get("error")),
            f"status={status} body={resp}",
        )

    # The other half, or the checks above would pass with everything
    # refused: a well-formed action still goes through. Re-selecting
    # what is already selected changes nothing.
    selected = api.state().get("selectedItemId")
    if selected:
        r.step(f"POST /api/ui/action selectRequest {{id: {selected}}}  (well-formed — must still be accepted)")
        status, _ = api.raw("POST", "/api/ui/action", {"action": "selectRequest", "payload": {"id": selected}})
        r.check("a well-formed action is still accepted", status == 204, f"status={status}")
    else:
        r.step("skipped the accepted-action half — nothing is selected to re-select")
