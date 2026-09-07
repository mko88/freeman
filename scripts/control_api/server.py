"""A local stand-in for httpbin, so this suite needs no network.

Freeman can vary a request along a lot of axes — method, query params,
headers, five body modes, four auth types, redirects, cookies, timeout,
and the TLS pair — and each of those needs a server that reports back
what it actually received. This module is that server: three of them,
in fact, because two of the axes are properties of the connection rather
than the request.

    plain   http://127.0.0.1:<port>    everything that isn't TLS
    tls     https://127.0.0.1:<port>   a self-signed certificate
    mtls    https://127.0.0.1:<port>   self-signed, and demands a client one

All three serve the same handler, so any endpoint works on any of them.

The response shapes match httpbin's where this suite already depended on
them (`args`, `headers`, `data`, `json`, `form`, `files`, `url`), which
is why the checks that predate this file didn't have to change: a body
httpbin can't render as text comes back as a `data:<type>;base64,<...>`
URI, and `files` is keyed by field name. Everything past that point is
ours.

Standard library only, like the rest of the suite. Two things it can't
make itself are committed under testdata/ instead: the self-signed
certificate pair each TLS server presents (and the client pair the mTLS
one demands), and BROTLI_JSON — Python can decompress brotli but not
compress it. See testdata/README.md for how to regenerate them.

Nothing here writes to disk or listens anywhere but loopback on a port
the OS picks, and every server is shut down in the entry point's
`finally`, so this leaves no more trace than the rest of the run.
"""

from __future__ import annotations

import base64
import gzip
import http.server
import json
import re
import socket
import ssl
import threading
import time
import urllib.parse
import zlib
from email.parser import BytesParser
from email.policy import HTTP
from pathlib import Path
from typing import Optional

TESTDATA = Path(__file__).resolve().parent / "testdata"

SERVER_CERT = TESTDATA / "server.pem"
SERVER_KEY = TESTDATA / "server.key"
CLIENT_CERT = TESTDATA / "client.pem"
CLIENT_KEY = TESTDATA / "client.key"

# A brotli stream of BROTLI_PAYLOAD. Python's stdlib has a brotli
# decompressor but no compressor, so this was produced once with Go's
# encoder — the same library Freeman decodes with. See
# testdata/README.md.
BROTLI_JSON = base64.b64decode("ixuAeyJicm90bGkiOiB0cnVlLCAibWV0aG9kIjogIkdFVCIsICJvcmlnaW4iOiAiMTI3LjAuMC4xIn0D")
BROTLI_PAYLOAD = b'{"brotli": true, "method": "GET", "origin": "127.0.0.1"}'

# The smallest thing that is unambiguously a PNG: an 8x8 solid square.
# Freeman's image handling keys off the magic bytes and the
# Content-Type, so the pixels themselves don't matter — being a real,
# decodable PNG does.
PNG_BYTES = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAIAQAAAADYv8WvAAAAEUlEQVR4nGP4"
    "z8DAwMDEwMAAAA4EAgFvKQlvAAAAAElFTkSuQmCC"
)

XML_DOC = b"""<?xml version="1.0" encoding="UTF-8"?>
<slideshow title="Freeman test slideshow" author="the control API suite">
  <slide type="all">
    <title>Wake up to WonderWidgets!</title>
  </slide>
</slideshow>
"""

# The token this server's /oauth/token hands out, and the credentials it
# insists on first. Fixed rather than random so a failing check reads as
# a mismatch rather than a mystery.
OAUTH_CLIENT_ID = "freeman-test-client"
OAUTH_CLIENT_SECRET = "freeman-test-secret"
OAUTH_ACCESS_TOKEN = "test-access-token-9f3a"

BASIC_USER = "freeman"
BASIC_PASSWORD = "s3cret"


def _is_textual(raw: bytes) -> bool:
    try:
        raw.decode("utf-8")
        return True
    except UnicodeDecodeError:
        return False


def _as_data_uri(raw: bytes, content_type: str = "application/octet-stream") -> str:
    """httpbin's encoding for a body it can't put in JSON as text, and
    what fixtures.decode_data_uri_bytes reads back."""
    return f"data:{content_type};base64," + base64.b64encode(raw).decode()


class _Handler(http.server.BaseHTTPRequestHandler):
    # HTTP/1.1 so keep-alive works; without it Go's transport reconnects
    # for every request, which is slow enough to be noticeable across a
    # few hundred checks.
    protocol_version = "HTTP/1.1"

    # --- plumbing ---------------------------------------------------------

    def log_message(self, format, *args):  # noqa: A002 - stdlib signature
        pass  # quiet: this suite does its own reporting

    def _read_body(self) -> bytes:
        length = self.headers.get("Content-Length")
        if length:
            return self.rfile.read(int(length))
        if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
            chunks = []
            while True:
                size = int(self.rfile.readline().strip() or b"0", 16)
                if size == 0:
                    self.rfile.readline()
                    break
                chunks.append(self.rfile.read(size))
                self.rfile.readline()
            return b"".join(chunks)
        return b""

    def _send(self, status: int, body: bytes = b"", content_type: str = "application/json", headers=None) -> None:
        self.send_response(status)
        if content_type:
            self.send_header("Content-Type", content_type)
        for name, value in (headers or []):
            self.send_header(name, value)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        # A HEAD response carries the headers a GET would, body included
        # in Content-Length, but no body — which is exactly what makes it
        # provable that HEAD, not GET, went out.
        if self.command != "HEAD":
            self.wfile.write(body)

    def _send_json(self, payload: dict, status: int = 200, headers=None) -> None:
        self._send(status, json.dumps(payload, indent=2).encode(), "application/json", headers)

    # --- the httpbin-shaped echo -----------------------------------------

    def _echo(self, raw: bytes) -> dict:
        """What httpbin's /get, /post, ... return: everything the server
        saw, so a check can assert on the wire rather than on the app's
        own account of what it sent."""
        parsed = urllib.parse.urlsplit(self.path)
        args = {k: v[0] if len(v) == 1 else v for k, v in urllib.parse.parse_qs(parsed.query).items()}
        out = {
            "args": args,
            "headers": {k: v for k, v in self.headers.items()},
            "method": self.command,
            "origin": self.client_address[0],
            "url": f"http://{self.headers.get('Host', '127.0.0.1')}{self.path}",
            "data": "",
            "json": None,
            "form": {},
            "files": {},
        }

        content_type = self.headers.get("Content-Type", "")
        if not raw:
            return out

        if content_type.startswith("multipart/form-data"):
            out["form"], out["files"] = self._parse_multipart(raw, content_type)
            return out

        if content_type.startswith("application/x-www-form-urlencoded") and _is_textual(raw):
            out["form"] = {
                k: v[0] if len(v) == 1 else v
                for k, v in urllib.parse.parse_qs(raw.decode()).items()
            }
            return out

        if _is_textual(raw):
            text = raw.decode()
            out["data"] = text
            try:
                out["json"] = json.loads(text)
            except ValueError:
                out["json"] = None
        else:
            out["data"] = _as_data_uri(raw, content_type or "application/octet-stream")
        return out

    def _parse_multipart(self, raw: bytes, content_type: str) -> tuple[dict, dict]:
        """Splits a multipart body into text fields and file parts, the
        way httpbin reports them: `form` keyed by field name, `files`
        keyed by field name with the contents as a data URI when they
        aren't text."""
        message = BytesParser(policy=HTTP).parsebytes(
            b"Content-Type: " + content_type.encode() + b"\r\nMIME-Version: 1.0\r\n\r\n" + raw
        )
        form: dict = {}
        files: dict = {}
        for part in message.iter_parts():
            disposition = part.get("Content-Disposition", "")
            name = _disposition_param(disposition, "name")
            if name is None:
                continue
            payload = part.get_payload(decode=True) or b""
            if _disposition_param(disposition, "filename") is not None:
                files[name] = (
                    payload.decode()
                    if _is_textual(payload)
                    else _as_data_uri(payload, part.get_content_type())
                )
            else:
                form[name] = payload.decode("utf-8", "replace")
        return form, files

    # --- routing ----------------------------------------------------------

    def do_GET(self):  # noqa: N802 - http.server's naming convention
        self._route()

    do_HEAD = do_GET
    do_POST = do_GET
    do_PUT = do_GET
    do_PATCH = do_GET
    do_DELETE = do_GET
    do_OPTIONS = do_GET

    def _route(self) -> None:
        raw = self._read_body()
        path = urllib.parse.urlsplit(self.path).path.rstrip("/") or "/"
        query = dict(urllib.parse.parse_qsl(urllib.parse.urlsplit(self.path).query))

        try:
            self._dispatch(path, query, raw)
        except BrokenPipeError:
            pass  # the app gave up on us (a timeout check does exactly that)

    def _dispatch(self, path: str, query: dict, raw: bytes) -> None:
        # OPTIONS answers everywhere, with headers and no body — the
        # method's own semantics, and what makes "an empty body" the
        # proof that OPTIONS rather than GET went out.
        if self.command == "OPTIONS":
            self._send(200, b"", "", [("Allow", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")])
            return

        # httpbin's method-specific endpoints: /get only answers GET,
        # /post only POST, and so on. Freeman's method tests rely on
        # that — a 405 is what proves the method actually went out.
        method_paths = {
            "/get": ("GET", "HEAD"),
            "/post": ("POST",),
            "/put": ("PUT",),
            "/patch": ("PATCH",),
            "/delete": ("DELETE",),
        }
        if path in method_paths:
            if self.command not in method_paths[path]:
                self._send_json({"error": f"{self.command} not allowed on {path}"}, 405)
            else:
                self._send_json(self._echo(raw))
            return

        # /anything answers anything, for a check that cares about the
        # echo rather than about the method being enforced.
        if path == "/anything" or path == "/":
            self._send_json(self._echo(raw))
            return

        if m := re.fullmatch(r"/status/(\d{3})", path):
            code = int(m.group(1))
            # No body: a bare status is what a check asserting on the
            # status line wants, and it keeps 204/304 legal.
            self._send(code, b"", "")
            return

        if m := re.fullmatch(r"/redirect/(\d+)", path):
            remaining = int(m.group(1))
            target = f"/redirect/{remaining - 1}" if remaining > 1 else "/get"
            self._send(302, b"", "", [("Location", target)])
            return

        # Never arrives anywhere: the only way to see what a redirect cap
        # does is to give it a chain that outlives it.
        if path == "/redirect-loop":
            self._send(302, b"", "", [("Location", "/redirect-loop")])
            return

        if path == "/cookies/set":
            headers = [("Set-Cookie", f"{k}={v}; Path=/") for k, v in query.items()]
            headers.append(("Location", "/cookies"))
            self._send(302, b"", "", headers)
            return

        if path == "/cookies":
            jar = {}
            for chunk in (self.headers.get("Cookie") or "").split(";"):
                if "=" in chunk:
                    k, v = chunk.split("=", 1)
                    jar[k.strip()] = v.strip()
            self._send_json({"cookies": jar})
            return

        if m := re.fullmatch(r"/delay/(\d+(?:\.\d+)?)", path):
            time.sleep(float(m.group(1)))
            self._send_json(self._echo(raw))
            return

        if m := re.fullmatch(r"/bytes/(\d+)", path):
            count = int(m.group(1))
            # Deterministic rather than random: a check comparing bytes
            # wants the same answer every run.
            payload = bytes((i * 37 + 11) % 256 for i in range(count))
            self._send(200, payload, "application/octet-stream")
            return

        if path in ("/image", "/image/png"):
            self._send(200, PNG_BYTES, "image/png")
            return

        if path == "/xml":
            self._send(200, XML_DOC, "application/xml")
            return

        if path == "/brotli":
            self._send(200, BROTLI_JSON, "application/json", [("Content-Encoding", "br")])
            return

        if path == "/gzip":
            self._send(200, gzip.compress(json.dumps(self._echo(raw)).encode()), "application/json", [("Content-Encoding", "gzip")])
            return

        if path == "/deflate":
            self._send(200, zlib.compress(json.dumps(self._echo(raw)).encode()), "application/json", [("Content-Encoding", "deflate")])
            return

        if m := re.fullmatch(r"/large/(\d+)", path):
            size = int(m.group(1))
            body = b'{"padding":"' + b"x" * size + b'"}'
            self._send(200, body, "application/json")
            return

        # --- the auth types, each answering 401 until it's satisfied ---

        if path == "/basic-auth":
            expected = "Basic " + base64.b64encode(f"{BASIC_USER}:{BASIC_PASSWORD}".encode()).decode()
            if self.headers.get("Authorization") != expected:
                self._send_json({"authenticated": False}, 401, [("WWW-Authenticate", 'Basic realm="freeman"')])
            else:
                self._send_json({"authenticated": True, "user": BASIC_USER})
            return

        if path == "/bearer":
            header = self.headers.get("Authorization") or ""
            if not header.startswith("Bearer "):
                self._send_json({"authenticated": False}, 401)
            else:
                self._send_json({"authenticated": True, "token": header[len("Bearer "):]})
            return

        # The API-key auth type is a header of the user's choosing, so
        # the endpoint has to be told which one to look at.
        if path == "/api-key":
            name = query.get("header", "X-API-Key")
            value = self.headers.get(name)
            if not value:
                self._send_json({"authenticated": False, "expectedHeader": name}, 401)
            else:
                self._send_json({"authenticated": True, "header": name, "key": value})
            return

        if path == "/oauth/token":
            self._oauth_token(raw)
            return

        # Only reachable with a valid client certificate, because the
        # mTLS server refuses the handshake without one — so answering
        # at all is the assertion.
        if path == "/client-cert":
            cert = self.connection.getpeercert() if hasattr(self.connection, "getpeercert") else None
            subject = ""
            for rdn in (cert or {}).get("subject", ()):
                for key, value in rdn:
                    if key == "commonName":
                        subject = value
            self._send_json({"clientCertificate": True, "subject": subject})
            return

        self._send_json({"error": "no such endpoint", "path": path}, 404)

    def _oauth_token(self, raw: bytes) -> None:
        """The client-credentials grant, checked properly: the wrong
        secret has to fail, or a check could pass on a token the server
        would have handed to anyone."""
        form = {k: v[0] for k, v in urllib.parse.parse_qs(raw.decode("utf-8", "replace")).items()}
        client_id, client_secret = form.get("client_id"), form.get("client_secret")
        if not client_id:
            # The other half of the spec: credentials in a Basic header
            # rather than the form. Freeman sends the form, but a script
            # generated from it might not.
            header = self.headers.get("Authorization") or ""
            if header.startswith("Basic "):
                decoded = base64.b64decode(header[len("Basic "):]).decode("utf-8", "replace")
                client_id, _, client_secret = decoded.partition(":")

        if form.get("grant_type") != "client_credentials":
            self._send_json({"error": "unsupported_grant_type"}, 400)
            return
        if client_id != OAUTH_CLIENT_ID or client_secret != OAUTH_CLIENT_SECRET:
            self._send_json({"error": "invalid_client"}, 401)
            return
        self._send_json(
            {
                "access_token": OAUTH_ACCESS_TOKEN,
                "token_type": "Bearer",
                "expires_in": 3600,
                "scope": form.get("scope", ""),
            }
        )


def _disposition_param(disposition: str, name: str) -> Optional[str]:
    m = re.search(rf'{name}="([^"]*)"', disposition)
    return m.group(1) if m else None


class _ThreadingServer(http.server.ThreadingHTTPServer):
    daemon_threads = True
    # SO_REUSEADDR clears a TIME_WAIT left by a check that made the app
    # give up mid-response. On Windows it does more than that: it lets a
    # second process bind a port another one is already serving, with no
    # error and no way to tell which of the two a connection reaches. So
    # it's set per instance instead of here — on when the OS picks the
    # port, off when the caller names one and a clash has to be heard
    # about. See TestServer.
    allow_reuse_address = False


class TestServer:
    """One of the three servers: a thread, a port, and a base URL.

    Started on construction and stopped by close(), which the entry
    point calls in its `finally` so a failed run doesn't leave a
    listener behind."""

    def __init__(self, scheme: str = "http", client_certs: bool = False, port: int = 0, host: str = "127.0.0.1") -> None:
        # Port 0 lets the OS pick, which is what the suite wants: no
        # clash with whatever else is listening, and nothing to clean
        # up. scripts/test_server.py passes fixed ones instead, so a
        # request saved against it still works tomorrow — and there a
        # port already in use has to raise rather than quietly become a
        # second listener on it (see _ThreadingServer).
        self.server = _ThreadingServer((host, port), _Handler, bind_and_activate=False)
        self.server.allow_reuse_address = port == 0
        try:
            self.server.server_bind()
            self.server.server_activate()
        except OSError:
            self.server.server_close()
            raise
        if scheme == "https":
            ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            ctx.load_cert_chain(SERVER_CERT, SERVER_KEY)
            if client_certs:
                # The client certificate is self-signed, so it is its own
                # issuer — trusting the certificate itself is what makes
                # CERT_REQUIRED accept exactly that one and nothing else.
                ctx.verify_mode = ssl.CERT_REQUIRED
                ctx.load_verify_locations(cafile=str(CLIENT_CERT))
            self.server.socket = ctx.wrap_socket(self.server.socket, server_side=True)
        self.port = self.server.server_address[1]
        self.base_url = f"{scheme}://{host}:{self.port}"
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)

    def __enter__(self) -> "TestServer":
        return self

    def __exit__(self, *exc) -> None:
        self.close()


class TestServers:
    """The three of them, started together and closed together."""

    def __init__(self, ports: tuple = (0, 0, 0), host: str = "127.0.0.1") -> None:
        started = []
        try:
            self.plain = TestServer(port=ports[0], host=host)
            started.append(self.plain)
            self.tls = TestServer("https", port=ports[1], host=host)
            started.append(self.tls)
            self.mtls = TestServer("https", client_certs=True, port=ports[2], host=host)
        except OSError:
            # A named port being taken is a normal way for this to fail;
            # the ones that did come up must not be left listening.
            for server in started:
                server.close()
            raise

    @property
    def base_url(self) -> str:
        return self.plain.base_url

    def close(self) -> None:
        for server in (self.plain, self.tls, self.mtls):
            try:
                server.close()
            except OSError:
                pass

    def __enter__(self) -> "TestServers":
        return self

    def __exit__(self, *exc) -> None:
        self.close()


def reachable(url: str, timeout: float = 2.0) -> bool:
    """A plain TCP connect, so the entry point can say 'the test server
    didn't come up' rather than letting every check fail one by one."""
    parts = urllib.parse.urlsplit(url)
    try:
        with socket.create_connection((parts.hostname, parts.port), timeout):
            return True
    except OSError:
        return False
