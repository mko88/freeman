"""The Code tab and POST /api/codegen."""

from __future__ import annotations

from .. import ControlAPI, Report, poll
from ..fixtures import TEST_VAR_KEY


def test_code_tab(api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str) -> None:
    """The Code tab renders the draft request as a runnable command via
    POST /api/codegen (backend: internal/codegen). Configures the scratch
    request main() created for this test, then checks each of the four
    formats through selectCodeFormat + GET /api/ui/state's `code`, plus a
    direct /api/codegen call and copyRequestCode. Exhaustive
    format/body-mode coverage is in Go (internal/codegen)."""
    r.section("Code tab (selectRequestTab 'code' / selectCodeFormat / copyRequestCode / POST /api/codegen)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    r.step(f"selectRequest {{id: {item_id}}}, set method/url/header/auth, saveRequest")
    api.action("selectRequest", {"id": item_id})
    poll(api.state, lambda s: s.get("selectedItemId") == item_id)
    api.action("setRequestField", {"field": "method", "value": "POST"})
    api.action("setRequestField", {"field": "url", "value": f"{{{{{TEST_VAR_KEY}}}}}/post"})
    api.action("selectRequestTab", {"tab": "headers"})
    api.action("addRequestHeader", {"key": "X-Trace", "value": "abc"})
    api.action("selectRequestTab", {"tab": "auth"})
    api.action("setRequestAuth", {"field": "type", "value": "bearer"})
    api.action("setRequestAuth", {"field": "token", "value": "t0ken"})
    api.action("saveRequest")

    r.step("selectRequestTab 'code'")
    api.action("selectRequestTab", {"tab": "code"})
    state = poll(api.state, lambda s: s.get("tab") == "code")
    r.check("state.tab reflects selectRequestTab 'code'", state.get("tab") == "code", str(state.get("tab")))

    # {{pyTestBase}} is https://httpbin.org (set by test_environment_editor).
    checks = {
        "curl": ["curl -X POST 'https://httpbin.org/post'", "-H 'X-Trace: abc'", "-H 'Authorization: Bearer t0ken'"],
        # The script forms pull the parts out as variables; the
        # one-liners keep everything inline. See internal/codegen.
        "shell": [
            "#!/usr/bin/env bash\nset -euo pipefail",
            "url='https://httpbin.org/post'",
            "-H 'Authorization: Bearer t0ken'",
            'curl -X POST "$url" \\\n  "${headers[@]}"',
        ],
        "powershell": ["Invoke-RestMethod -Method POST -Uri 'https://httpbin.org/post'", "'Authorization' = 'Bearer t0ken'"],
        "powershell-script": [
            "$uri = 'https://httpbin.org/post'",
            "$headers = @{",
            "Invoke-RestMethod `\n    -Method POST `\n    -Uri $uri `\n    -Headers $headers",
        ],
    }
    for fmt, needles in checks.items():
        r.step(f"selectCodeFormat {{format: {fmt!r}}}")
        api.action("selectCodeFormat", {"format": fmt})
        state = poll(
            api.state,
            lambda s, f=fmt, n=needles: s.get("codeFormat") == f and all(x in (s.get("code") or "") for x in n),
        )
        code = state.get("code") or ""
        r.check(
            f"{fmt}: state.code contains {needles!r}",
            state.get("codeFormat") == fmt and all(x in code for x in needles),
            f"codeFormat={state.get('codeFormat')} code={code[:200]!r}",
        )

    r.step("copyRequestCode  (clipboard write — just needs to not error)")
    api.action("copyRequestCode")

    r.step("POST /api/codegen directly with an inline item")
    status, body = api.post(
        "/api/codegen",
        {
            "item": {"method": "GET", "url": f"{{{{{TEST_VAR_KEY}}}}}/get", "headers": []},
            "environmentId": environment_id,
            "format": "curl",
        },
    )
    r.check(
        "POST /api/codegen returns a curl string with the var substituted",
        status == 200 and isinstance(body, dict) and body.get("code", "").startswith("curl -X GET 'https://httpbin.org/get'"),
        f"status={status} body={body}",
    )
