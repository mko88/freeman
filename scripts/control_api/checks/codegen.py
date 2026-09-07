"""The Code tab and POST /api/codegen."""

from __future__ import annotations

from .. import ControlAPI, Report, poll
from ..fixtures import TEST_VAR_KEY


def test_code_tab(api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str, test_base: str) -> None:
    """The Code tab renders the draft request as a runnable command via
    POST /api/codegen (backend: internal/codegen). Configures the scratch
    request main() created for this test, then checks all four formats
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
    # generated script has to contain. Every format pulls the parts out
    # as variables, so the call at the bottom of each reads as a list of
    # names. See internal/codegen.
    checks = {
        "bash": [
            "#!/usr/bin/env bash\nset -euo pipefail",
            f"url='{test_base}/post'",
            "-H 'X-Trace: abc'",
            "-H 'Authorization: Bearer t0ken'",
            # -L because the request follows redirects and curl doesn't
            # unless told, --max-time because Freeman gives up after 30s
            # and curl never would, -b/-c because the request is on the
            # cookie jar — the generated script has to send what Send
            # sends. See the Options tab.
            'curl -X POST -L "$url" \\\n  -b \'cookies.txt\' -c \'cookies.txt\' --max-time 30 \\\n  "${headers[@]}"',
        ],
        "powershell": [
            f"$uri = '{test_base}/post'",
            "$headers = @{",
            "'Authorization' = 'Bearer t0ken'",
            "Invoke-RestMethod `\n    -Method POST `\n    -Uri $uri `\n    -TimeoutSec 30 `"
            "\n    -SessionVariable session `\n    -Headers $headers",
        ],
        "python": [
            "import requests",
            f"url = '{test_base}/post'",
            "session = requests.Session()",
            "MozillaCookieJar('cookies-python.txt')",
            "'Authorization': 'Bearer t0ken',",
            "allow_redirects=True",
            "timeout=30",
        ],
        "javascript": [
            f"const url = '{test_base}/post'",
            "Authorization: 'Bearer t0ken',",
            "const response = await fetch(url, {",
            "redirect: 'follow',",
            "signal: AbortSignal.timeout(30000),",
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

    # Every Options-tab setting a language can express has to reach the
    # script, or the Code tab would hand back a command that talks to a
    # different server than Send does. What a language genuinely can't
    # express is left out — see the javascript list below.
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
        "python": [
            "session.max_redirects = 3",
            "timeout=2.5",
            "verify=False",
            "cert=('/certs/client.pem', '/certs/client.key')",
        ],
        # fetch can express the timeout and, process-wide, the
        # certificate check. The redirect cap, the cookie jar and the
        # client certificate have no equivalent and don't appear at all.
        "javascript": [
            "AbortSignal.timeout(2500)",
            "process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0'",
        ],
    }
    # The options are set while a format is *already* selected, and the
    # first check reads that same format back without switching. That
    # ordering is the regression: codeKey once left draft.options out, so
    # setRequestOption regenerated nothing and state.code stayed stale —
    # invisible by hand, since changing an option means leaving the Code
    # tab and coming back, but not to a script. Switching format first
    # would hide it again.
    r.step(f"selectCodeFormat 'bash', then setRequestOption {options!r}  (no format switch after)")
    api.action("selectCodeFormat", {"format": "bash"})
    poll(api.state, lambda s: s.get("codeFormat") == "bash")
    for field, value in options.items():
        api.action("setRequestOption", {"field": field, "value": value})
    state = poll(api.state, lambda s: all(x in (s.get("code") or "") for x in option_checks["bash"]))
    r.check(
        "setRequestOption alone regenerates the code, with no format switch to force it",
        all(x in (state.get("code") or "") for x in option_checks["bash"]),
        f"code={(state.get('code') or '')[:400]!r}",
    )

    r.step("re-read the code for each of the other formats")
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
