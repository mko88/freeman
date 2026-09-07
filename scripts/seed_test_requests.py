#!/usr/bin/env python3
"""Fill a collection with one saved request per test-server endpoint.

`py scripts/test_server.py` gives you a server to send things at. This
gives you something to click on: a "Test Server" collection with a
request for each way Freeman can vary one — the four auth types, the
body modes, redirects, cookies, timeouts, TLS and mutual TLS — all
already configured, and a "Test Server" environment holding the URLs
they're written against.

    py scripts/test_server.py            # in one terminal, leave running
    py scripts/seed_test_requests.py     # in another, once

Everything is created through the control API, the same path a click
takes, so what lands on disk is exactly what the app would have saved.

It writes into your real workspace, unlike test_control_api.py, because
that's the point — so it never overwrites: a request whose name is
already in the collection is left exactly as it is and reported as
skipped. Run it as many times as you like; to get a modified one back,
delete it in the app and run this again. --list prints what it would
create without touching anything.

Requires Python 3.8+, standard library only. Freeman must be running.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from control_api import ApiError, ControlAPI, poll  # noqa: E402 - after sys.path
from control_api.fixtures import RANDOM_FILE_PATH  # noqa: E402
from control_api.server import (  # noqa: E402
    BASIC_PASSWORD,
    BASIC_USER,
    CLIENT_CERT,
    CLIENT_KEY,
    OAUTH_CLIENT_ID,
    OAUTH_CLIENT_SECRET,
)
from test_server import DEFAULT_MTLS_PORT, DEFAULT_PORT, DEFAULT_TLS_PORT  # noqa: E402

COLLECTION_NAME = "Test Server"
ENVIRONMENT_NAME = "Test Server"

# Written as {{vars}} rather than literal URLs so that moving the server
# to another port is one edit in the environment, not thirty in the
# requests — which is also the feature being demonstrated.
def environment_variables(port: int, tls_port: int, mtls_port: int) -> list[dict]:
    return [
        {"key": "base", "value": f"http://127.0.0.1:{port}"},
        {"key": "tlsBase", "value": f"https://127.0.0.1:{tls_port}"},
        {"key": "mtlsBase", "value": f"https://127.0.0.1:{mtls_port}"},
        {"key": "clientCert", "value": str(CLIENT_CERT)},
        {"key": "clientCertKey", "value": str(CLIENT_KEY)},
        {"key": "basicUser", "value": BASIC_USER},
        {"key": "basicPassword", "value": BASIC_PASSWORD, "secret": True},
        {"key": "oauthClientId", "value": OAUTH_CLIENT_ID},
        {"key": "oauthClientSecret", "value": OAUTH_CLIENT_SECRET, "secret": True},
    ]


# One entry per request. `fields` are setRequestField pairs, `auth`
# setRequestAuth, `options` setRequestOption, and headers/params/form
# their own add* actions — the same actions the control API documents,
# so this file doubles as a worked example of driving it.
REQUESTS: list[dict] = [
    {
        "name": "Echo a GET",
        "note": "everything the server received, echoed back",
        "method": "GET",
        "url": "{{base}}/get?greeting=hello",
        "headers": [("X-Demo", "freeman")],
        "params": [("extra", "from the params tab")],
    },
    {
        "name": "POST a JSON body",
        "method": "POST",
        "url": "{{base}}/post",
        "headers": [("Content-Type", "application/json")],
        "fields": [("bodyMode", "raw"), ("bodyRaw", '{\n  "sku": "A-1",\n  "qty": 3\n}')],
    },
    {
        "name": "POST a urlencoded form",
        "method": "POST",
        "url": "{{base}}/post",
        "fields": [("bodyMode", "x-www-form-urlencoded")],
        "form": [{"key": "sku", "type": "text", "value": "A-1"}, {"key": "qty", "type": "text", "value": "3"}],
    },
    {
        "name": "POST a multipart upload",
        "note": "a file field beside a text one",
        "method": "POST",
        "url": "{{base}}/post",
        "fields": [("bodyMode", "form-data")],
        "form": [
            {"key": "caption", "type": "text", "value": "a random sample"},
            {"key": "blob", "type": "file", "filePath": str(RANDOM_FILE_PATH)},
        ],
    },
    {
        "name": "POST a file as the whole body",
        "method": "POST",
        "url": "{{base}}/post",
        "fields": [("bodyMode", "binary"), ("binaryFilePath", str(RANDOM_FILE_PATH))],
    },
    {
        "name": "A 404, shown not swallowed",
        "method": "GET",
        "url": "{{base}}/status/404",
    },
    {
        "name": "A 500, shown not swallowed",
        "method": "GET",
        "url": "{{base}}/status/500",
    },
    {
        "name": "Follow a redirect chain",
        "note": "three hops, ending at /get",
        "method": "GET",
        "url": "{{base}}/redirect/3",
    },
    {
        "name": "Keep the redirect itself",
        "note": "the only way to assert on a 302's Location",
        "method": "GET",
        "url": "{{base}}/redirect/3",
        "options": [("followRedirects", False)],
    },
    {
        "name": "Stop an endless redirect",
        "note": "the server redirects to itself forever; the cap ends it",
        "method": "GET",
        "url": "{{base}}/redirect-loop",
        "options": [("maxRedirects", 3)],
    },
    {
        "name": "Get a cookie",
        "note": "send this first, then 'Send the cookie back'",
        "method": "GET",
        "url": "{{base}}/cookies/set?session=abc123",
    },
    {
        "name": "Send the cookie back",
        "note": "a different request, on the same jar",
        "method": "GET",
        "url": "{{base}}/cookies",
    },
    {
        "name": "Send it off the jar",
        "note": "the same request with cookies off — the server sees none",
        "method": "GET",
        "url": "{{base}}/cookies",
        "options": [("storeCookies", False)],
    },
    {
        "name": "Give up after a second",
        "note": "the server takes five on purpose",
        "method": "GET",
        "url": "{{base}}/delay/5",
        "options": [("timeoutMs", 1000)],
    },
    {
        "name": "Basic auth",
        "method": "GET",
        "url": "{{base}}/basic-auth",
        "auth": [("type", "basic"), ("username", "{{basicUser}}"), ("password", "{{basicPassword}}")],
    },
    {
        "name": "Bearer token",
        "method": "GET",
        "url": "{{base}}/bearer",
        "auth": [("type", "bearer"), ("token", "a-token-of-your-choosing")],
    },
    {
        "name": "API key header",
        "method": "GET",
        "url": "{{base}}/api-key?header=X-API-Key",
        "auth": [("type", "apikey"), ("key", "X-API-Key"), ("value", "key-9f3a")],
    },
    {
        "name": "OAuth2 client credentials",
        "note": "fetches a token from /oauth/token first, then sends it",
        "method": "GET",
        "url": "{{base}}/bearer",
        "auth": [
            ("type", "oauth2"),
            ("tokenUrl", "{{base}}/oauth/token"),
            ("clientId", "{{oauthClientId}}"),
            ("clientSecret", "{{oauthClientSecret}}"),
            ("scope", "orders.read"),
        ],
    },
    {
        "name": "A self-signed certificate",
        "note": "fails until you turn on Skip TLS certificate check",
        "method": "GET",
        "url": "{{tlsBase}}/get",
    },
    {
        "name": "A self-signed certificate, accepted",
        "method": "GET",
        "url": "{{tlsBase}}/get",
        "options": [("skipTlsVerify", True)],
    },
    {
        "name": "Mutual TLS",
        "note": "the server demands a client certificate; this one presents it",
        "method": "GET",
        "url": "{{mtlsBase}}/client-cert",
        "options": [
            ("skipTlsVerify", True),
            ("clientCertFile", "{{clientCert}}"),
            ("clientCertKeyFile", "{{clientCertKey}}"),
        ],
    },
    {
        "name": "A gzipped response",
        "method": "GET",
        "url": "{{base}}/gzip",
    },
    {
        "name": "A brotli response",
        "method": "GET",
        "url": "{{base}}/brotli",
    },
    {
        "name": "A PNG",
        "note": "shown as an image, with Raw and Hex beside it",
        "method": "GET",
        "url": "{{base}}/image/png",
    },
    {
        "name": "An XML document",
        "method": "GET",
        "url": "{{base}}/xml",
    },
    {
        "name": "A body too large to show",
        "note": "two megabytes — the pane offers the file instead",
        "method": "GET",
        "url": "{{base}}/large/2000000",
    },
]


def find_by_name(entries: list, name: str) -> dict:
    return next((e for e in entries or [] if e.get("name") == name), None)


def ensure_collection(api: ControlAPI) -> str:
    workspace = api.get("/api/workspace")
    existing = find_by_name(workspace.get("collections"), COLLECTION_NAME)
    if existing:
        print(f"  collection {COLLECTION_NAME!r} already there ({existing['itemCount']} requests)")
        api.action("selectCollection", {"id": existing["id"]})
        return existing["id"]

    print(f"  creating collection {COLLECTION_NAME!r}")
    api.action("newCollection", {"name": COLLECTION_NAME})
    workspace = poll(
        lambda: api.get("/api/workspace"),
        lambda w: find_by_name(w.get("collections"), COLLECTION_NAME) is not None,
    )
    return find_by_name(workspace["collections"], COLLECTION_NAME)["id"]


def ensure_environment(api: ControlAPI, variables: list[dict]) -> None:
    workspace = api.get("/api/workspace")
    existing = find_by_name(workspace.get("environments"), ENVIRONMENT_NAME)
    if existing:
        print(f"  environment {ENVIRONMENT_NAME!r} already there — leaving its variables alone")
        api.action("selectEnvironment", {"id": existing["id"]})
        return

    print(f"  creating environment {ENVIRONMENT_NAME!r} with {len(variables)} variables")
    api.action("newEnvironment")
    poll(api.state, lambda s: (s.get("environment") or {}).get("id") is not None)
    api.action("setEnvironmentField", {"field": "name", "value": ENVIRONMENT_NAME})
    for var in variables:
        api.action("addEnvironmentVariable", var)
        poll(
            api.state,
            lambda s, k=var["key"]: any(
                v.get("key") == k for v in (s.get("environment") or {}).get("variables") or []
            ),
        )
    api.action("saveEnvironment")


def create_request(api: ControlAPI, collection_id: str, spec: dict) -> None:
    api.action("newRequest")
    poll(api.state, lambda s: s.get("selectedItemId") is None)
    api.action("setRequestField", {"field": "name", "value": spec["name"]})
    api.action("setRequestField", {"field": "method", "value": spec.get("method", "GET")})
    api.action("setRequestField", {"field": "url", "value": spec["url"]})
    for field, value in spec.get("fields", []):
        api.action("setRequestField", {"field": field, "value": value})
    for field, value in spec.get("auth", []):
        api.action("setRequestAuth", {"field": field, "value": value})
    for field, value in spec.get("options", []):
        api.action("setRequestOption", {"field": field, "value": value})
    for key, value in spec.get("headers", []):
        api.action("addRequestHeader", {"key": key, "value": value})
    for key, value in spec.get("params", []):
        api.action("addRequestParam", {"key": key, "value": value})
    for field in spec.get("form", []):
        api.action("addRequestFormField", field)
    api.action("saveRequest")
    poll(
        lambda: api.get(f"/api/collections/{collection_id}"),
        lambda c, n=spec["name"]: find_by_name(c.get("items"), n) is not None,
    )


def main() -> int:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--base-url", default="http://127.0.0.1:8090", help="Freeman's control API")
    parser.add_argument("--port", type=int, default=DEFAULT_PORT, help=f"the test server's plain port (default: {DEFAULT_PORT})")
    parser.add_argument("--tls-port", type=int, default=DEFAULT_TLS_PORT, help=f"its TLS port (default: {DEFAULT_TLS_PORT})")
    parser.add_argument("--mtls-port", type=int, default=DEFAULT_MTLS_PORT, help=f"its mutual-TLS port (default: {DEFAULT_MTLS_PORT})")
    parser.add_argument("--delay", type=float, default=0.0, help="pause between actions, to watch it happen (default: 0)")
    parser.add_argument("--list", action="store_true", help="print what would be created and exit, touching nothing")
    args = parser.parse_args()

    variables = environment_variables(args.port, args.tls_port, args.mtls_port)

    if args.list:
        print(f"{ENVIRONMENT_NAME!r} environment:")
        for var in variables:
            shown = "********" if var.get("secret") else var["value"]
            print(f"  {var['key']:<20} {shown}")
        print(f"\n{COLLECTION_NAME!r} collection — {len(REQUESTS)} requests:")
        for spec in REQUESTS:
            note = f"   ({spec['note']})" if spec.get("note") else ""
            print(f"  {spec.get('method', 'GET'):<6} {spec['name']:<38} {spec['url']}{note}")
        return 0

    api = ControlAPI(args.base_url, args.delay)
    try:
        print(f"Freeman control API at {args.base_url}")
        collection_id = ensure_collection(api)
        ensure_environment(api, variables)

        existing = {i.get("name") for i in api.get(f"/api/collections/{collection_id}").get("items") or []}
        created = skipped = 0
        for spec in REQUESTS:
            if spec["name"] in existing:
                print(f"  · {spec['name']}  (already there — left alone)")
                skipped += 1
                continue
            print(f"  + {spec['name']}")
            create_request(api, collection_id, spec)
            created += 1
    except ApiError as e:
        print(f"\n[ERROR] {e}")
        return 2
    except KeyboardInterrupt:
        print("\nInterrupted.")
        return 130

    print(f"\n{created} created, {skipped} left alone.")
    print(f"The app is now on the {COLLECTION_NAME!r} collection and environment — switch back in the top bar.")
    print("Start the server they point at with:  py scripts/test_server.py")
    return 0


if __name__ == "__main__":
    sys.exit(main())
