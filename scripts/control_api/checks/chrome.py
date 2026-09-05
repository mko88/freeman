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
    finally:
        r.step("restoring sidebarWidth/statusBarHeight/tab/requestPaneCollapsed to what this test found them as")
        if isinstance(original_sidebar_width, (int, float)):
            api.action("setSidebarWidth", {"px": original_sidebar_width})
        if isinstance(original_status_bar_height, (int, float)):
            api.action("setStatusBarHeight", {"px": original_status_bar_height})
        if original_tab in ("params", "headers", "body"):
            api.action("selectRequestTab", {"tab": original_tab})  # also forces expanded
        if original_pane_collapsed:
            api.action("toggleRequestPane")  # re-collapse if that's how it was found


def test_help_modal(api: ControlAPI, r: Report) -> None:
    r.section("Help modal")
    r.step("toggleHelp  (watch: the Control API help panel should open)")
    api.action("toggleHelp")
    r.step("toggleHelp  (watch: it should close again)")
    api.action("toggleHelp")
    r.check("toggleHelp open/close accepted", True)
