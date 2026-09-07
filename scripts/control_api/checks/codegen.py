"""The Code tab and POST /api/codegen."""

from __future__ import annotations

from .. import ControlAPI, Report, poll
from ..fixtures import TEST_VAR_KEY


def test_code_tab(api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str, test_base: str) -> None:
    """The Code tab renders the draft request as a runnable command via
    POST /api/codegen (backend: internal/codegen). Configures the scratch
    request main() created for this test, then checks both formats
    through selectCodeFormat + GET /api/ui/state's `code`, that the
    Options tab's settings reach the script, plus a direct /api/codegen
    call and copyRequestCode. Exhaustive format/body-mode coverage is in
    Go (internal/codegen)."""
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

    # {{pyTestBase}} is the local test server (set by
    # test_environment_editor), so the substituted URL is what the
    # generated script has to contain. Both formats pull the parts out as
    # variables, so the command at the bottom of each reads as a list of
    # names. See internal/codegen.
    checks = {
        "bash": [
            "#!/usr/bin/env bash\nset -euo pipefail",
            f"url='{test_base}/post'",
            "-H 'X-Trace: abc'",
            "-H 'Authorization: Bearer t0ken'",
            # -L because the request follows redirects and curl doesn't
            # unless told, --max-time because Freeman gives up after 30s
            # and curl never would — the generated script has to send
            # what Send sends. See the Options tab.
            'curl -X POST -L "$url" \\\n  --max-time 30 \\\n  "${headers[@]}"',
        ],
        "powershell": [
            f"$uri = '{test_base}/post'",
            "$headers = @{",
            "'Authorization' = 'Bearer t0ken'",
            "Invoke-RestMethod `\n    -Method POST `\n    -Uri $uri `\n    -TimeoutSec 30 `\n    -Headers $headers",
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

    # Every Options-tab setting that a standalone script can express has
    # to reach the script, or the Code tab would hand back a command that
    # talks to a different server than Send does. The cookie jar is the
    # one that can't: it belongs to the app, not to one command.
    options = {
        "maxRedirects": 3,
        "timeoutMs": 2500,
        "skipTlsVerify": True,
        "clientCertFile": "/certs/client.pem",
        "clientCertKeyFile": "/certs/client.key",
    }
    option_checks = {
        "bash": ["--max-redirs 3", "--max-time 2.5", "--insecure", '--cert "$cert" --key "$key"'],
        "powershell": ["-MaximumRedirection 3", "-TimeoutSec 3", "-SkipCertificateCheck", "-Certificate $cert"],
    }
    r.step(f"setRequestOption {options!r}, then re-read the code for each format")
    for field, value in options.items():
        api.action("setRequestOption", {"field": field, "value": value})
    for fmt, needles in option_checks.items():
        api.action("selectCodeFormat", {"format": fmt})
        state = poll(
            api.state,
            lambda s, f=fmt, n=needles: s.get("codeFormat") == f and all(x in (s.get("code") or "") for x in n),
        )
        code = state.get("code") or ""
        r.check(
            f"{fmt}: request options reach the generated script {needles!r}",
            all(x in code for x in needles),
            f"code={code[:400]!r}",
        )

    r.step("setRequestOption back to the defaults")
    for field, value in {
        "maxRedirects": 0,
        "timeoutMs": 0,
        "skipTlsVerify": False,
        "clientCertFile": "",
        "clientCertKeyFile": "",
    }.items():
        api.action("setRequestOption", {"field": field, "value": value})

    r.step("copyRequestCode  (clipboard write — just needs to not error)")
    api.action("copyRequestCode")

    r.step("POST /api/codegen directly with an inline item")
    status, body = api.post(
        "/api/codegen",
        {
            "item": {"method": "GET", "url": f"{{{{{TEST_VAR_KEY}}}}}/get", "headers": []},
            "environmentId": environment_id,
            "format": "bash",
        },
    )
    r.check(
        "POST /api/codegen returns a bash script with the var substituted",
        status == 200 and isinstance(body, dict) and f"url='{test_base}/get'" in body.get("code", ""),
        f"status={status} body={body}",
    )
