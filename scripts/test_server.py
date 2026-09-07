#!/usr/bin/env python3
"""Run the suite's test server on fixed ports, to point Freeman at by hand.

`scripts/test_control_api.py` starts this same server (see
scripts/control_api/server.py) on ports the OS picks and shuts it down
when the run ends. That's right for a test and wrong for poking at the
app yourself: a request you save today has to still work tomorrow, so
this runs it on fixed ports and stays up until you stop it.

    py scripts/test_server.py

Then send requests at http://127.0.0.1:8100/… from the app. Every
endpoint below answers on all three servers.

`py scripts/seed_test_requests.py` fills a collection with one saved
request per endpoint, so there's something to click on rather than a
list of paths to retype.

Ctrl-C stops it. Nothing is written to disk and nothing listens off
loopback unless you pass --host.

Requires Python 3.8+, standard library only.
"""

from __future__ import annotations

import argparse
import sys
import threading
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from control_api.server import (  # noqa: E402 - after the sys.path line above
    BASIC_PASSWORD,
    BASIC_USER,
    CLIENT_CERT,
    CLIENT_KEY,
    OAUTH_CLIENT_ID,
    OAUTH_CLIENT_SECRET,
    TestServers,
)

DEFAULT_PORT = 8100
DEFAULT_TLS_PORT = 8101
DEFAULT_MTLS_PORT = 8102

# path, what it's for. Printed on startup so the window you leave this
# running in doubles as the reference.
ENDPOINTS = [
    ("/get /post /put /patch /delete", "echo back everything received; each answers only its own method"),
    ("/anything", "the same echo, on any method"),
    ("/status/<code>", "answer with that status and no body"),
    ("/redirect/<n>", "n hops of 302, ending at /get"),
    ("/redirect-loop", "302 to itself, forever — for the redirect cap"),
    ("/cookies/set?name=value", "Set-Cookie, then redirect to /cookies"),
    ("/cookies", "echo the cookies sent — for the shared jar"),
    ("/delay/<seconds>", "answer that slowly — for the timeout"),
    ("/bytes/<n>", "n deterministic bytes as application/octet-stream"),
    ("/large/<n>", "a JSON body with n bytes of padding — for truncation"),
    ("/image /image/png", "a real PNG"),
    ("/xml", "an XML document"),
    ("/gzip /deflate /brotli", "the echo, compressed, to prove it's decoded"),
    ("/basic-auth", f"401 unless Basic {BASIC_USER}:{BASIC_PASSWORD}"),
    ("/bearer", "401 unless there's a Bearer token; echoes the token back"),
    ("/api-key?header=X-API-Key", "401 unless that header is set; echoes it back"),
    ("/oauth/token", f"client-credentials grant for {OAUTH_CLIENT_ID}/{OAUTH_CLIENT_SECRET}"),
    ("/client-cert", "only reachable through the mutual-TLS server"),
]


def main() -> int:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--port", type=int, default=DEFAULT_PORT, help=f"plain HTTP port (default: {DEFAULT_PORT})")
    parser.add_argument("--tls-port", type=int, default=DEFAULT_TLS_PORT, help=f"HTTPS port, self-signed (default: {DEFAULT_TLS_PORT})")
    parser.add_argument(
        "--mtls-port", type=int, default=DEFAULT_MTLS_PORT, help=f"HTTPS port demanding a client certificate (default: {DEFAULT_MTLS_PORT})"
    )
    parser.add_argument(
        "--host",
        default="127.0.0.1",
        help="interface to listen on (default: 127.0.0.1 — loopback only; anything else exposes it to your network)",
    )
    parser.add_argument("--quiet", action="store_true", help="don't print the endpoint list")
    args = parser.parse_args()

    try:
        servers = TestServers((args.port, args.tls_port, args.mtls_port), args.host)
    except OSError as e:
        print(f"[ERROR] couldn't listen: {e}")
        print("        something else is probably on one of those ports — pass --port/--tls-port/--mtls-port")
        return 2

    print(f"  plain        {servers.plain.base_url}")
    print(f"  TLS          {servers.tls.base_url}   (self-signed: needs 'Skip TLS certificate check')")
    print(f"  mutual TLS   {servers.mtls.base_url}   (also needs the client certificate below)")
    print()
    print(f"  client certificate   {CLIENT_CERT}")
    print(f"  client key           {CLIENT_KEY}")

    if not args.quiet:
        print()
        width = max(len(path) for path, _ in ENDPOINTS)
        for path, desc in ENDPOINTS:
            print(f"  {path.ljust(width)}   {desc}")

    print("\nCtrl-C to stop.")
    try:
        # Wait on an event rather than the serving threads: they're
        # daemons, and joining them would swallow Ctrl-C on Windows.
        threading.Event().wait()
    except KeyboardInterrupt:
        print("\nStopping.")
    finally:
        servers.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
