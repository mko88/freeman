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
