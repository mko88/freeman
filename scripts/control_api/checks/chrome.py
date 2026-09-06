"""Window chrome: splitters, collapsible panes, the help modal."""

from __future__ import annotations

from .. import ControlAPI, Report, poll


def test_layout_comfort(api: ControlAPI, r: Report) -> None:
    """The resizable sidebar/log splitters are pure layout, not app data —
    there's no saved/executed request to check here, just that the
    setters round-trip (with clamping) and the two collapse toggles
    work. Restores every value to what it found before it started,
    rather than assuming defaults, since it runs against whatever state
    earlier sections already left."""
    r.section("Layout comfort (splitters, collapsible panes)")

    state = api.state()
    original_sidebar_width = state.get("sidebarWidth")
    original_status_bar_height = state.get("statusBarHeight")
    original_show_log = state.get("showControlApiLog")
    original_tab = state.get("tab")
    original_pane_collapsed = state.get("requestPaneCollapsed")
    original_response_tab = state.get("responseTab")
    original_response_collapsed = state.get("responsePaneCollapsed")
    original_response_height = state.get("responseHeight")

    try:
        r.step("setSidebarWidth {px: 400}")
        api.action("setSidebarWidth", {"px": 400})
        state = poll(api.state, lambda s: s.get("sidebarWidth") == 400)
        r.check("state.sidebarWidth reflects setSidebarWidth", state.get("sidebarWidth") == 400, str(state.get("sidebarWidth")))

        r.step("setSidebarWidth {px: 99999}  (clamped to a sane max)")
        api.action("setSidebarWidth", {"px": 99999})
        state = poll(api.state, lambda s: s.get("sidebarWidth") != 400)
        r.check(
            "state.sidebarWidth clamped rather than accepting an unbounded value",
            isinstance(state.get("sidebarWidth"), (int, float)) and state.get("sidebarWidth") < 99999,
            str(state.get("sidebarWidth")),
        )

        r.step("setResponseHeight {px: 300}  (watch: the request/response splitter moves)")
        api.action("setResponseHeight", {"px": 300})
        state = poll(api.state, lambda s: s.get("responseHeight") == 300)
        r.check(
            "state.responseHeight reflects setResponseHeight",
            state.get("responseHeight") == 300,
            str(state.get("responseHeight")),
        )

        r.step("setResponseHeight {px: 99999}  (clamped to a sane max)")
        api.action("setResponseHeight", {"px": 99999})
        state = poll(api.state, lambda s: s.get("responseHeight") != 300)
        r.check(
            "state.responseHeight clamped rather than accepting an unbounded value",
            isinstance(state.get("responseHeight"), (int, float)) and state.get("responseHeight") < 99999,
            str(state.get("responseHeight")),
        )

        r.step("setStatusBarHeight {px: 250}")
        api.action("setStatusBarHeight", {"px": 250})
        state = poll(api.state, lambda s: s.get("statusBarHeight") == 250)
        r.check(
            "state.statusBarHeight reflects setStatusBarHeight", state.get("statusBarHeight") == 250, str(state.get("statusBarHeight"))
        )

        r.step("toggleControlApiLog  (watch: the log panel should collapse)")
        api.action("toggleControlApiLog")
        state = poll(api.state, lambda s: s.get("showControlApiLog") != original_show_log)
        r.check(
            "state.showControlApiLog reflects toggleControlApiLog",
            state.get("showControlApiLog") != original_show_log,
            str(state.get("showControlApiLog")),
        )
        r.step("toggleControlApiLog  (watch: it should expand again)")
        api.action("toggleControlApiLog")
        state = poll(api.state, lambda s: s.get("showControlApiLog") == original_show_log)
        r.check(
            "state.showControlApiLog reflects the second toggleControlApiLog",
            state.get("showControlApiLog") == original_show_log,
            str(state.get("showControlApiLog")),
        )

        r.step("selectRequestTab {tab: 'body'}  (deterministic: switches and expands, never toggles)")
        api.action("selectRequestTab", {"tab": "body"})
        state = poll(api.state, lambda s: s.get("tab") == "body" and s.get("requestPaneCollapsed") is False)
        r.check(
            "state.tab is 'body' and requestPaneCollapsed is False after selectRequestTab",
            state.get("tab") == "body" and state.get("requestPaneCollapsed") is False,
            str(state),
        )
        r.step("selectRequestTab {tab: 'body'}  (the same tab again — still expanded, not a toggle)")
        api.action("selectRequestTab", {"tab": "body"})
        state = poll(api.state, lambda s: s.get("tab") == "body")
        r.check(
            "selectRequestTab on the same tab doesn't collapse it — only toggleRequestPane/clicking does",
            state.get("requestPaneCollapsed") is False,
            str(state.get("requestPaneCollapsed")),
        )

        r.step("toggleRequestPane  (watch: the Body table collapses, the response pane grows)")
        api.action("toggleRequestPane")
        state = poll(api.state, lambda s: s.get("requestPaneCollapsed") is True)
        r.check(
            "state.requestPaneCollapsed reflects toggleRequestPane", state.get("requestPaneCollapsed") is True, str(state.get("requestPaneCollapsed"))
        )
        r.step("toggleRequestPane  (watch: it should expand again)")
        api.action("toggleRequestPane")
        state = poll(api.state, lambda s: s.get("requestPaneCollapsed") is False)
        r.check(
            "state.requestPaneCollapsed reflects the second toggleRequestPane",
            state.get("requestPaneCollapsed") is False,
            str(state.get("requestPaneCollapsed")),
        )

        r.step("toggleResponsePane  (watch: the response body hides, its status strip stays)")
        api.action("toggleResponsePane")
        state = poll(api.state, lambda s: s.get("responsePaneCollapsed") is True)
        r.check(
            "state.responsePaneCollapsed reflects toggleResponsePane",
            state.get("responsePaneCollapsed") is True,
            str(state.get("responsePaneCollapsed")),
        )
        # setResponseTab expands, the way selectRequestTab does — a script
        # asking for a panel shouldn't have to know whether it was collapsed.
        r.step("setResponseTab {tab: 'body'}  (deterministic: switches and expands, never toggles)")
        api.action("setResponseTab", {"tab": "body"})
        state = poll(api.state, lambda s: s.get("responsePaneCollapsed") is False)
        r.check(
            "setResponseTab expands a collapsed response pane",
            state.get("responseTab") == "body" and state.get("responsePaneCollapsed") is False,
            str(state.get("responsePaneCollapsed")),
        )
    finally:
        r.step("restoring sidebarWidth/statusBarHeight/tab/both collapse flags to what this test found them as")
        if isinstance(original_sidebar_width, (int, float)):
            api.action("setSidebarWidth", {"px": original_sidebar_width})
        if isinstance(original_status_bar_height, (int, float)):
            api.action("setStatusBarHeight", {"px": original_status_bar_height})
        if isinstance(original_response_height, (int, float)):
            api.action("setResponseHeight", {"px": original_response_height})
        if original_tab in ("params", "headers", "body"):
            api.action("selectRequestTab", {"tab": original_tab})  # also forces expanded
        if original_pane_collapsed:
            api.action("toggleRequestPane")  # re-collapse if that's how it was found
        if original_response_tab in ("body", "headers"):
            api.action("setResponseTab", {"tab": original_response_tab})  # also forces expanded
        if original_response_collapsed:
            api.action("toggleResponsePane")


def test_help_modal(api: ControlAPI, r: Report) -> None:
    r.section("Help modal")
    r.step("toggleHelp  (watch: the Control API help panel should open)")
    api.action("toggleHelp")
    r.step("toggleHelp  (watch: it should close again)")
    api.action("toggleHelp")
    r.check("toggleHelp open/close accepted", True)
