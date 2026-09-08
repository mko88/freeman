"""The shared cookie jar: the two places it shows, and the ways it can
be changed.

test_option_variations already proves the jar *works* — a cookie a
server sets on one request goes out on the next. This is about seeing
and editing it: the response pane's Cookies tab (what one response set,
read out of its own Set-Cookie headers) and the settings window's
(everything the app is holding, with a delete beside each row).
"""

from __future__ import annotations

from urllib.parse import quote

from .. import ControlAPI, Report, poll
from .variations import DEFAULT_OPTIONS, Runner, body_json


def _find(jar: list, name: str) -> dict:
    for c in jar or []:
        if c.get("name") == name:
            return c
    return {}


def _names(jar: list) -> list:
    return sorted(c.get("name") for c in jar or [])


def test_cookie_jar(
    api: ControlAPI, r: Report, collection_id: str, environment_id: str, item_id: str, base: str
) -> None:
    r.section("Cookie jar (GET/DELETE /api/cookies / refreshCookies / deleteCookie / clearCookies)")

    if not item_id:
        r.check("skipped — no empty scratch request for this test", False)
        return

    # Whatever an earlier section left in the jar (test_option_variations
    # sets pySession) would make every count below depend on run order.
    # Start from empty, and leave it that way at the end.
    status, jar = api.delete("/api/cookies")
    r.check(
        "DELETE /api/cookies with no name empties the jar and answers with what is left",
        status == 200 and jar == [],
        f"status={status} jar={str(jar)[:200]}",
    )

    run = Runner(api, r, collection_id, environment_id, item_id)

    # followRedirects=false on purpose. /cookies/set answers 302 with the
    # Set-Cookie headers on it; following the redirect would leave the
    # response pane holding the *target's* headers, which carry none —
    # and the pane's Cookies tab reads the response's own headers.
    r.step("GET /cookies/set?pyJarA=one&pyJarB=two&pyJarC=three, followRedirects=false")
    run.configure(
        f"{base}/cookies/set?pyJarA=one&pyJarB=two&pyJarC=three",
        options={**DEFAULT_OPTIONS, "storeCookies": True, "followRedirects": False},
    )
    status, resp = run.send()
    r.check(
        "the 302 that carries the Set-Cookie headers comes back as the response",
        resp.get("statusCode") == 302,
        f"status={status} resp={str(resp)[:160]}",
    )

    jar = api.get("/api/cookies")
    r.check(
        "GET /api/cookies lists all three, with the path the server implied",
        _names(jar) == ["pyJarA", "pyJarB", "pyJarC"] and _find(jar, "pyJarA").get("path") == "/",
        str(jar)[:300],
    )
    r.check(
        "...keyed to the host that set them, not left blank",
        bool(_find(jar, "pyJarB").get("domain")),
        str(_find(jar, "pyJarB")),
    )
    r.check(
        "...and with no expiry, which is how a session cookie reads",
        str(_find(jar, "pyJarB").get("expires", "")).startswith("0001-01-01"),
        str(_find(jar, "pyJarB").get("expires")),
    )

    # --- the response pane's own tab -------------------------------------

    r.step("setResponseTab {tab: 'cookies'}  (watch: the response pane switches to its Cookies tab)")
    api.action("setResponseTab", {"tab": "cookies"})
    state = poll(api.state, lambda s: s.get("responseTab") == "cookies")
    r.check(
        "state.responseTab reflects setResponseTab 'cookies'",
        state.get("responseTab") == "cookies",
        str(state.get("responseTab")),
    )
    r.check(
        "...and it expanded the pane, the way the other two tabs do",
        state.get("responsePaneCollapsed") is False,
        str(state.get("responsePaneCollapsed")),
    )
    # That tab reads the response's own Set-Cookie headers rather than
    # the jar, so what it can show has to be in state.response at all.
    headers = {k.lower(): v for k, v in ((state.get("response") or {}).get("headers") or {}).items()}
    r.check(
        "the last response's Set-Cookie headers are in the state mirror for that tab to render",
        len(headers.get("set-cookie") or []) >= 1,
        str(headers.get("set-cookie"))[:200],
    )
    api.action("setResponseTab", {"tab": "body"})
    poll(api.state, lambda s: s.get("responseTab") == "body")

    r.step("GET /cookies with the jar on  (all three should go out on a later request)")
    run.configure(f"{base}/cookies", options={**DEFAULT_OPTIONS, "storeCookies": True})
    status, resp = run.send()
    echoed = body_json(resp).get("cookies") or {}
    r.check(
        "every cookie in the jar reaches the next request",
        echoed == {"pyJarA": "one", "pyJarB": "two", "pyJarC": "three"},
        f"status={status} echoed={echoed}",
    )

    # The route's other half. The window drives this through Wails rather
    # than over HTTP, so it needs its own check or the query-parameter
    # form is only ever exercised by hand.
    c = _find(jar, "pyJarC")
    query = f"domain={quote(c.get('domain') or '')}&path={quote(c.get('path') or '')}&name=pyJarC"
    r.step(f"DELETE /api/cookies?{query}")
    status, jar = api.delete(f"/api/cookies?{query}")
    r.check(
        "deleting by domain+path+name forgets exactly that one",
        status == 200 and _names(jar) == ["pyJarA", "pyJarB"],
        f"status={status} jar={str(jar)[:300]}",
    )

    # --- the settings window's tab ---------------------------------------

    r.step("toggleSettings, selectSettingsTab {tab: 'cookies'}  (watch: the jar, one row per cookie)")
    api.action("toggleSettings")
    poll(api.state, lambda s: s.get("showSettings") is True)
    api.action("selectSettingsTab", {"tab": "cookies"})
    state = poll(api.state, lambda s: s.get("settingsTab") == "cookies")
    r.check(
        "state.settingsTab reflects selectSettingsTab 'cookies'",
        state.get("settingsTab") == "cookies",
        str(state.get("settingsTab")),
    )

    r.step("refreshCookies")
    api.action("refreshCookies")
    state = poll(api.state, lambda s: _names(s.get("cookies")) == ["pyJarA", "pyJarB"])
    r.check(
        "state.cookies mirrors the jar, so an action's effect is readable without looking at the window",
        _names(state.get("cookies")) == ["pyJarA", "pyJarB"],
        str(state.get("cookies"))[:300],
    )

    r.step("deleteCookie {name: 'pyJarA', domain, path}  (watch: that row goes, the other stays)")
    a = _find(jar, "pyJarA")
    api.action("deleteCookie", {"name": "pyJarA", "domain": a.get("domain"), "path": a.get("path")})
    state = poll(api.state, lambda s: _names(s.get("cookies")) == ["pyJarB"])
    r.check(
        "deleting one leaves the other alone",
        _names(state.get("cookies")) == ["pyJarB"],
        str(state.get("cookies"))[:300],
    )

    # The point of deleting one: it stops going out. cookiejar has no way
    # to forget a cookie, so this is the check that the rebuild behind
    # httpengine.Jar.Delete actually took.
    r.step("GET /cookies again  (the deleted ones must not be sent any more)")
    run.configure(f"{base}/cookies", options={"storeCookies": True})
    status, resp = run.send()
    echoed = body_json(resp).get("cookies") or {}
    r.check(
        "the deleted cookies are gone from the wire, not just from the list",
        echoed == {"pyJarB": "two"},
        f"status={status} echoed={echoed}",
    )

    r.step("clearCookies  (watch: the list empties)")
    api.action("clearCookies")
    state = poll(api.state, lambda s: (s.get("cookies") or []) == [])
    r.check("clearCookies empties state.cookies", (state.get("cookies") or []) == [], str(state.get("cookies")))
    r.check("...and GET /api/cookies agrees", api.get("/api/cookies") == [], str(api.get("/api/cookies")))

    r.step("selectSettingsTab {tab: 'workspace'}, toggleSettings  (back to where this found them)")
    api.action("selectSettingsTab", {"tab": "workspace"})
    poll(api.state, lambda s: s.get("settingsTab") == "workspace")
    api.action("toggleSettings")
    poll(api.state, lambda s: s.get("showSettings") is False)
    run.reset_options()
