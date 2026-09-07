"""Every way a request can vary, against the local server.

The other check modules prove the UI wires up to the engine at all. This
one walks the axes a request can actually differ along — the four auth
types, the body modes, the status codes that come back, and each of the
Options tab's settings — and asserts on what the server reports it
received, not on what the app says it sent.

That distinction is the point of control_api.server: httpbin can't show
that "Skip TLS certificate check" changes anything (it has a valid
certificate), can't be given a client certificate to demand, and can't
be made slow on purpose. A server this suite starts itself can do all
three, and answers in microseconds instead of over the internet.
"""

from __future__ import annotations

import json

from .. import ControlAPI, Report, poll
from ..fixtures import TEST_CERT_VAR_KEY, find_item_by_id
from ..server import (
    BASIC_PASSWORD,
    BASIC_USER,
    CLIENT_CERT,
    CLIENT_KEY,
    OAUTH_ACCESS_TOKEN,
    OAUTH_CLIENT_ID,
    OAUTH_CLIENT_SECRET,
    TestServers,
)

# Every option back to its default, applied between checks so one
# section can't leave a setting behind for the next.
DEFAULT_OPTIONS = {
    "followRedirects": True,
    "maxRedirects": 10,
    "storeCookies": True,
    "timeoutMs": 30000,
    "skipTlsVerify": False,
    "clientCertFile": "",
    "clientCertKeyFile": "",
}

# What each option reads as when it isn't in the saved JSON. Go omits
# every zero-valued one, so an absent key means the zero value — except
# that an absent `options` object altogether means the defaults above,
# which is a different thing for the two booleans that default to true.
#
# maxRedirects is the exception within the exception: it's a *int on the
# Go side precisely so "absent" and "explicitly 0" stay distinguishable,
# and absent means the workspace default rather than 0 (no cap).
ZERO_OPTIONS = {k: (False if isinstance(v, bool) else v) for k, v in DEFAULT_OPTIONS.items()}
ZERO_OPTIONS.update(
    {"maxRedirects": DEFAULT_OPTIONS["maxRedirects"], "timeoutMs": 0, "clientCertFile": "", "clientCertKeyFile": ""}
)


def saved_options(item: dict) -> dict:
    """The item's options as all seven values, whichever of them Go left
    out of the JSON."""
    if "options" not in item:
        return dict(DEFAULT_OPTIONS)
    stored = item.get("options") or {}
    return {k: stored.get(k, ZERO_OPTIONS[k]) for k in DEFAULT_OPTIONS}


class _Runner:
    """Select once, then configure-save-execute as many times as the
    section needs. Every send goes out twice on purpose: sendRequest so
    the app window visibly does the thing, then POST /api/execute for
    the response the checks read — the same split the older modules use,
    since the rendered pane isn't readable over the control API."""

    def __init__(self, api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str) -> None:
        self.api = api
        self.r = r
        self.collection_id = collection_id
        self.environment_id = environment_id
        self.item_id = item_id
        # Auth and options are sticky in the draft, so the runner tracks
        # the whole set rather than only what each step changes —
        # configure() has to wait for the real, complete configuration to
        # be on disk, and consecutive steps here often differ in nothing
        # else.
        self.options = dict(DEFAULT_OPTIONS)
        self.auth: dict = {"type": "none"}
        self.body_mode = "none"
        api.action("selectRequest", {"id": item_id})
        poll(api.state, lambda s: s.get("selectedItemId") == item_id)

    def configure(self, url: str, method: str = "GET", auth: dict = None, options: dict = None, body_mode: str = None) -> None:
        self.url, self.method = url, method
        self.api.action("setRequestField", {"field": "method", "value": method})
        self.api.action("setRequestField", {"field": "url", "value": url})
        for field, value in (auth or {}).items():
            self.api.action("setRequestAuth", {"field": field, "value": value})
        self.auth.update(auth or {})
        for field, value in (options or {}).items():
            self.api.action("setRequestOption", {"field": field, "value": value})
        self.options.update(options or {})
        if body_mode is not None:
            self.api.action("setRequestField", {"field": "bodyMode", "value": body_mode})
            self.body_mode = body_mode
        self.api.action("saveRequest")
        # saveRequest's 204 can come back with its own round trip still
        # in flight, and /api/execute reads what is on disk. Waiting on
        # the URL alone isn't enough: consecutive steps often reuse it,
        # so the wait would be satisfied by the *previous* save and the
        # send would go out with the previous options. Wait for every
        # part of the configuration instead.
        poll(
            lambda: self.api.get(f"/api/collections/{self.collection_id}"),
            lambda c: self._matches(find_item_by_id(c, self.item_id) or {}),
        )

    def _matches(self, item: dict) -> bool:
        auth = item.get("auth") or {}
        return (
            item.get("url") == self.url
            and item.get("method") == self.method
            and saved_options(item) == self.options
            and ((item.get("body") or {}).get("mode") or "none") == self.body_mode
            # Go omits the empty auth fields, so an absent one reads as
            # "", and an absent auth object as no auth at all.
            and all(auth.get(k, "none" if k == "type" else "") == v for k, v in self.auth.items())
        )

    def send(self) -> tuple[int, dict]:
        self.api.action("sendRequest")
        return self.api.post(
            "/api/execute",
            {"collectionId": self.collection_id, "itemId": self.item_id, "environmentId": self.environment_id},
        )

    def reset_options(self) -> None:
        for field, value in DEFAULT_OPTIONS.items():
            self.api.action("setRequestOption", {"field": field, "value": value})
        self.options = dict(DEFAULT_OPTIONS)

    def clear_auth(self) -> None:
        self.api.action("setRequestAuth", {"field": "type", "value": "none"})
        self.auth = {"type": "none"}


def _body_json(resp: dict) -> dict:
    try:
        return json.loads(resp.get("body") or "")
    except (ValueError, TypeError):
        return {}


def test_auth_variations(
    api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str, servers: TestServers
) -> None:
    """All four auth types, each against an endpoint that answers 401
    until the right credential arrives — so a check can't pass on a
    request that sent nothing."""
    r.section("Auth types (none / bearer / basic / apikey / oauth2 against the local server)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    run = _Runner(api, r, collection_id, environment_id, item_id)
    base = servers.base_url

    r.step("auth 'none' against /bearer  (should be refused — proves the endpoint really checks)")
    run.configure(f"{base}/bearer", auth={"type": "none"})
    status, resp = run.send()
    r.check(
        "an unauthenticated request is refused with 401",
        status == 200 and resp.get("statusCode") == 401,
        f"status={status} resp={resp}",
    )

    r.step("setRequestAuth type='bearer', token  (watch: the Auth tab switches)")
    run.configure(f"{base}/bearer", auth={"type": "bearer", "token": "t0ken-from-the-auth-tab"})
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "bearer auth reaches the server as Authorization: Bearer <token>",
        resp.get("statusCode") == 200 and body.get("token") == "t0ken-from-the-auth-tab",
        f"status={status} body={body}",
    )

    r.step(f"setRequestAuth type='basic', username={BASIC_USER!r}, password")
    run.configure(f"{base}/basic-auth", auth={"type": "basic", "username": BASIC_USER, "password": BASIC_PASSWORD})
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "basic auth reaches the server base64-encoded and is accepted",
        resp.get("statusCode") == 200 and body.get("authenticated") is True,
        f"status={status} body={body}",
    )

    r.step("setRequestAuth type='basic' with the wrong password  (the credential has to actually matter)")
    run.configure(f"{base}/basic-auth", auth={"type": "basic", "username": BASIC_USER, "password": "wrong"})
    status, resp = run.send()
    r.check(
        "the wrong password is refused, so the check above proves something",
        resp.get("statusCode") == 401,
        f"status={status} resp={resp}",
    )

    r.step("setRequestAuth type='apikey', key='X-API-Key', value")
    run.configure(f"{base}/api-key?header=X-API-Key", auth={"type": "apikey", "key": "X-API-Key", "value": "key-9f3a"})
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "apikey auth arrives as the named header with the given value",
        resp.get("statusCode") == 200 and body.get("key") == "key-9f3a",
        f"status={status} body={body}",
    )

    r.step("setRequestAuth type='oauth2' with the client-credentials grant  (the app fetches a token first)")
    run.configure(
        f"{base}/bearer",
        auth={
            "type": "oauth2",
            "tokenUrl": f"{base}/oauth/token",
            "clientId": OAUTH_CLIENT_ID,
            "clientSecret": OAUTH_CLIENT_SECRET,
            "scope": "orders.read",
        },
    )
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "oauth2 fetches a token and sends it as a bearer token",
        resp.get("statusCode") == 200 and body.get("token") == OAUTH_ACCESS_TOKEN,
        f"status={status} body={body}",
    )

    # A different token URL, not a different secret: the engine caches a
    # token under its URL, client id and scope, so changing only the
    # secret would be answered from the cache and prove nothing.
    r.step("setRequestAuth tokenUrl -> an endpoint that errors  (a token that can't be fetched must fail the request)")
    run.configure(f"{base}/bearer", auth={"type": "oauth2", "tokenUrl": f"{base}/status/500"})
    status, resp = run.send()
    r.check(
        "a request whose token can't be fetched fails rather than going out unauthenticated",
        status != 200,
        f"status={status} resp={str(resp)[:200]}",
    )

    run.clear_auth()


def test_body_and_status_variations(
    api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str, servers: TestServers
) -> None:
    """The body modes the older modules don't reach (raw, form-data and
    binary are covered by test_execute/test_file_upload), plus the
    status codes the app has to surface unchanged rather than treat as
    failures."""
    r.section("Body modes and status codes")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    run = _Runner(api, r, collection_id, environment_id, item_id)
    base = servers.base_url

    r.step("bodyMode 'x-www-form-urlencoded' with two fields")
    api.action("selectRequestTab", {"tab": "body"})
    api.action("addRequestFormField", {"key": "grant", "type": "text", "value": "widget"})
    api.action("addRequestFormField", {"key": "qty", "type": "text", "value": "7"})
    run.configure(f"{base}/post", method="POST", body_mode="x-www-form-urlencoded")
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "urlencoded fields arrive as a parsed form, not as a raw string",
        resp.get("statusCode") == 200 and (body.get("form") or {}).get("grant") == "widget" and (body.get("form") or {}).get("qty") == "7",
        f"status={status} form={body.get('form')}",
    )
    r.check(
        "...under the Content-Type the mode implies",
        (body.get("headers") or {}).get("Content-Type", "").startswith("application/x-www-form-urlencoded"),
        str((body.get("headers") or {}).get("Content-Type")),
    )

    r.step("bodyMode 'none' on a POST  (nothing should be sent, and that has to be fine)")
    run.configure(f"{base}/post", method="POST", body_mode="none")
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "a POST with no body succeeds and the server sees an empty one",
        resp.get("statusCode") == 200 and not body.get("data") and not body.get("form"),
        f"status={status} data={body.get('data')!r} form={body.get('form')}",
    )

    # A 404 and a 500 are answers, not errors: the app has to show them
    # the way it shows a 200, or you couldn't test an API's failure
    # modes with it at all.
    for code in (204, 301, 404, 418, 500):
        r.step(f"GET /status/{code}")
        run.configure(f"{base}/status/{code}", options={"followRedirects": False})
        status, resp = run.send()
        r.check(
            f"a {code} comes back as a response to look at, not a failed send",
            status == 200 and resp.get("statusCode") == code,
            f"status={status} resp={str(resp)[:160]}",
        )
    run.reset_options()


def test_option_variations(
    api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str, servers: TestServers
) -> None:
    """The Options tab, end to end. Each setting is checked by making
    the server behave in a way only that setting can cope with — a
    redirect chain, a cookie, a deliberate delay, a self-signed
    certificate, a demand for a client one."""
    r.section("Request options (redirects / cookies / timeout / TLS / client certificate)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    run = _Runner(api, r, collection_id, environment_id, item_id)
    base = servers.base_url

    # --- redirects --------------------------------------------------------

    r.step("followRedirects=true against a 3-hop redirect chain")
    run.configure(f"{base}/redirect/3", options={**DEFAULT_OPTIONS, "followRedirects": True})
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "following redirects lands on the final 200, not the first 302",
        resp.get("statusCode") == 200 and body.get("method") == "GET",
        f"status={status} resp={str(resp)[:160]}",
    )

    r.step("followRedirects=false against the same chain")
    run.configure(f"{base}/redirect/3", options={"followRedirects": False})
    status, resp = run.send()
    location = {k.lower(): v for k, v in (resp.get("headers") or {}).items()}.get("location")
    r.check(
        "not following hands back the 302 itself — the only way to assert on a redirect",
        resp.get("statusCode") == 302,
        f"status={status} resp={str(resp)[:160]}",
    )
    r.check("...with its Location header intact", bool(location), str(resp.get("headers"))[:200])

    r.step("followRedirects=true, maxRedirects=3 against an endless chain")
    run.configure(f"{base}/redirect-loop", options={"followRedirects": True, "maxRedirects": 3})
    status, resp = run.send()
    r.check(
        "the redirect cap stops an endless chain instead of hanging",
        status != 200,
        f"status={status} resp={str(resp)[:200]}",
    )
    run.reset_options()

    # --- the cookie jar ---------------------------------------------------

    r.step("storeCookies=true: GET /cookies/set?pySession=abc123  (the server sets one, then redirects)")
    run.configure(f"{base}/cookies/set?pySession=abc123", options={**DEFAULT_OPTIONS, "storeCookies": True})
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "the redirect after Set-Cookie is followed and the cookie is echoed straight back",
        resp.get("statusCode") == 200 and (body.get("cookies") or {}).get("pySession") == "abc123",
        f"status={status} body={body}",
    )

    r.step("storeCookies=true: a *separate* GET /cookies  (this is what the shared jar is for)")
    run.configure(f"{base}/cookies", options={"storeCookies": True})
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "a later request on the jar sends the cookie the earlier one was given",
        (body.get("cookies") or {}).get("pySession") == "abc123",
        f"status={status} body={body}",
    )

    r.step("storeCookies=false: the same GET /cookies")
    run.configure(f"{base}/cookies", options={"storeCookies": False})
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "a request off the jar sends no cookie at all, isolating it from that session",
        body.get("cookies") == {},
        f"status={status} body={body}",
    )
    run.reset_options()

    # --- timeout ----------------------------------------------------------

    r.step("timeoutMs=400 against /delay/2  (the server takes 2s on purpose)")
    run.configure(f"{base}/delay/2", options={**DEFAULT_OPTIONS, "timeoutMs": 400})
    status, resp = run.send()
    r.check(
        "a request that outruns its timeout fails instead of waiting",
        status != 200,
        f"status={status} resp={str(resp)[:200]}",
    )

    r.step("timeoutMs=5000 against /delay/1  (comfortably inside the limit)")
    run.configure(f"{base}/delay/1", options={"timeoutMs": 5000})
    status, resp = run.send()
    r.check(
        "a request that finishes inside its timeout is unaffected",
        status == 200 and resp.get("statusCode") == 200,
        f"status={status} resp={str(resp)[:160]}",
    )
    run.reset_options()

    # --- TLS --------------------------------------------------------------

    r.step(f"GET {servers.tls.base_url}/get with skipTlsVerify=false  (self-signed — should be refused)")
    run.configure(f"{servers.tls.base_url}/get", options={**DEFAULT_OPTIONS, "skipTlsVerify": False})
    status, resp = run.send()
    r.check(
        "a self-signed certificate is rejected by default",
        status != 200,
        f"status={status} resp={str(resp)[:200]}",
    )

    r.step("...the same request with skipTlsVerify=true")
    run.configure(f"{servers.tls.base_url}/get", options={"skipTlsVerify": True})
    status, resp = run.send()
    r.check(
        "skipping the certificate check is what makes the same request work",
        status == 200 and resp.get("statusCode") == 200,
        f"status={status} resp={str(resp)[:200]}",
    )
    run.reset_options()

    # --- client certificates ---------------------------------------------

    r.step(f"GET {servers.mtls.base_url}/client-cert with no client certificate  (the server demands one)")
    run.configure(f"{servers.mtls.base_url}/client-cert", options={**DEFAULT_OPTIONS, "skipTlsVerify": True})
    status, resp = run.send()
    r.check(
        "a server that requires mutual TLS refuses a request without a certificate",
        status != 200,
        f"status={status} resp={str(resp)[:200]}",
    )

    # Written as {{pyCertDir}}/… rather than a literal path: which
    # certificate to present belongs to the environment, so these fields
    # take substitution like the URL does — and for a while didn't, so
    # every mutual-TLS request opened a file named "{{pyCertDir}}".
    r.step("...the same request with clientCertFile/clientCertKeyFile set to {{pyCertDir}}/client.pem and .key")
    run.configure(
        f"{servers.mtls.base_url}/client-cert",
        options={
            "skipTlsVerify": True,
            "clientCertFile": f"{{{{{TEST_CERT_VAR_KEY}}}}}/{CLIENT_CERT.name}",
            "clientCertKeyFile": f"{{{{{TEST_CERT_VAR_KEY}}}}}/{CLIENT_KEY.name}",
        },
    )
    status, resp = run.send()
    body = _body_json(resp)
    r.check(
        "presenting the client certificate completes the handshake",
        status == 200 and resp.get("statusCode") == 200 and body.get("clientCertificate") is True,
        f"status={status} resp={str(resp)[:200]}",
    )
    r.check(
        "...and the server sees the certificate Freeman was told to present",
        body.get("subject") == "freeman-test-client",
        str(body),
    )

    r.step("clientCertFile without its key  (half a pair is no pair — the handshake should fail again)")
    run.configure(
        f"{servers.mtls.base_url}/client-cert",
        options={"skipTlsVerify": True, "clientCertFile": str(CLIENT_CERT), "clientCertKeyFile": ""},
    )
    status, resp = run.send()
    r.check(
        "a certificate without its key is ignored rather than half-applied",
        status != 200,
        f"status={status} resp={str(resp)[:200]}",
    )
    run.reset_options()


def test_encoding_variations(
    api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str, servers: TestServers
) -> None:
    """Compressed responses. Freeman advertises gzip/deflate/br and has
    to hand back the decoded document — the proof is that the body
    parses, since undecoded bytes would be garbage whatever the
    Content-Type claimed."""
    r.section("Response encodings (gzip / deflate / brotli)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    run = _Runner(api, r, collection_id, environment_id, item_id)

    for encoding in ("gzip", "deflate", "brotli"):
        r.step(f"GET /{encoding}")
        run.configure(f"{servers.base_url}/{encoding}")
        status, resp = run.send()
        body = resp.get("body") or ""
        try:
            json.loads(body)
            decoded = True
        except (ValueError, TypeError):
            decoded = False
        r.check(
            f"a {encoding}-encoded response is decoded to parseable JSON",
            status == 200 and resp.get("statusCode") == 200 and decoded,
            f"status={status} body={body[:120]!r}",
        )
