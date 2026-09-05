"""The HTTP client every check drives Freeman through."""

from __future__ import annotations

import json
import time
import urllib.error
import urllib.request
from typing import Any, Optional


class ApiError(RuntimeError):
    pass


class ControlAPI:
    """Thin HTTP client for Freeman's control API. stdlib-only (urllib) so
    this script needs nothing beyond a base Python install."""

    def __init__(self, base_url: str, delay: float, verbose: bool = False):
        self.base_url = base_url.rstrip("/")
        self.delay = delay
        self.verbose = verbose

    def _request(
        self,
        method: str,
        path: str,
        body: Optional[dict] = None,
        content_type: Optional[str] = "application/json",
        extra_headers: Optional[dict] = None,
    ) -> tuple[int, Any]:
        """content_type/extra_headers exist for test_api_guard, which has
        to send the exact header shapes a browser would (see
        internal/httpapi/guard.go); every other caller takes the
        defaults."""
        url = f"{self.base_url}{path}"
        data = json.dumps(body).encode("utf-8") if body is not None else None
        headers = {"Content-Type": content_type} if data is not None and content_type else {}
        headers.update(extra_headers or {})
        req = urllib.request.Request(url, data=data, method=method, headers=headers)
        if self.verbose:
            print(f"    -> {method} {path}" + (f" {json.dumps(body)}" if body else ""))
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                raw, status = resp.read(), resp.status
        except urllib.error.HTTPError as e:
            raw, status = e.read(), e.code
        except urllib.error.URLError as e:
            raise ApiError(
                f"Can't reach {url}: {e.reason}. Is Freeman running with the control API "
                "enabled? (desktop build only — see CLAUDE.md)"
            ) from e
        text = raw.decode("utf-8") if raw else ""
        parsed: Any = None
        if text:
            try:
                parsed = json.loads(text)
            except json.JSONDecodeError:
                parsed = text
        return status, parsed

    def get(self, path: str) -> Any:
        status, body = self._request("GET", path)
        if status >= 400:
            raise ApiError(f"GET {path} -> {status}: {body}")
        return body

    def post(self, path: str, body: Optional[dict] = None) -> tuple[int, Any]:
        return self._request("POST", path, body)

    def delete(self, path: str) -> tuple[int, Any]:
        return self._request("DELETE", path)

    def raw(
        self,
        method: str,
        path: str,
        body: Optional[dict] = None,
        content_type: Optional[str] = "application/json",
        extra_headers: Optional[dict] = None,
    ) -> tuple[int, Any]:
        """Send a request with full control over the headers, returning
        (status, body) without raising on 4xx — test_api_guard needs to
        assert on refusals, not trip over them."""
        return self._request(method, path, body, content_type, extra_headers)

    def action(self, name: str, payload: Optional[dict] = None, wait: bool = True) -> None:
        """Fire one ui:action. Waits `self.delay` seconds afterward by
        default so a human watching the app window can follow along —
        pass wait=False for the rapid-fire regression check, which
        specifically wants zero delay between calls."""
        status, body = self.post("/api/ui/action", {"action": name, "payload": payload or {}})
        if status != 204:
            raise ApiError(f"ui:action {name} -> {status}: {body}")
        if wait and self.delay > 0:
            time.sleep(self.delay)

    def state(self) -> dict:
        """GET /api/ui/state — the getter side of the control API (see
        CLAUDE.md's "every settable field needs a getter too" rule).
        Callers should poll() this, same as any other route: it reflects
        whatever App.svelte's reportUIState last pushed, which lands
        after the action that changed it has fully applied, not before."""
        body = self.get("/api/ui/state")
        return body if isinstance(body, dict) else {}
