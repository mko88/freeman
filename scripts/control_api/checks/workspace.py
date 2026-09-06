"""The read-only routes, sidebar navigation, and the settings window."""

from __future__ import annotations

from .. import ControlAPI, Report, poll


def test_read_only_routes(api: ControlAPI, r: Report) -> dict:
    r.section("Read-only routes")

    health = api.get("/api/health")
    r.check("GET /api/health", health == {"status": "ok"}, str(health))

    workspace = api.get("/api/workspace")
    r.check(
        "GET /api/workspace has a collection and an environment",
        bool(workspace.get("collections")) and bool(workspace.get("environments")),
        "open a workspace with at least one of each before running this script",
    )

    theme = api.get("/api/theme")
    r.check("GET /api/theme returns a palette", isinstance(theme, dict) and "accent" in theme, str(theme))

    headers_catalog = api.get("/api/headers")
    names = {e.get("name") for e in headers_catalog} if isinstance(headers_catalog, list) else set()
    r.check("GET /api/headers includes Content-Type", "Content-Type" in names, str(names))

    ui_state = api.state()
    r.check("GET /api/ui/state returns an object", isinstance(ui_state, dict), str(ui_state))

    return workspace


def test_workspace_navigation(api: ControlAPI, r: Report, workspace: dict) -> tuple[str, str]:
    r.section("Workspace navigation (selectCollection / selectEnvironment)")

    collection_id = workspace["collections"][0]["id"]
    environment_id = workspace["environments"][0]["id"]

    r.step(f"selectCollection {{id: {collection_id}}}")
    api.action("selectCollection", {"id": collection_id})
    r.check("selectCollection accepted", True)

    r.step(f"selectEnvironment {{id: {environment_id}}}")
    api.action("selectEnvironment", {"id": environment_id})
    r.check("selectEnvironment accepted", True)

    return collection_id, environment_id


SCRATCH_COLLECTION = "zz-control-api-scratch"
SCRATCH_COLLECTION_RENAMED = "zz-control-api-scratch-renamed"


def test_collection_management(api: ControlAPI, r: Report, original_collection_id: str) -> None:
    """Creating, renaming and deleting a collection from the top bar's
    switcher — the whole lifecycle, on a scratch collection that's gone
    again by the end. Also the two switcher menus' open state."""
    r.section("Collections (newCollection / renameCollection / deleteCollection)")

    created_id = ""
    try:
        r.step(f"newCollection {{name: {SCRATCH_COLLECTION!r}}}  (watch: the top bar should switch to it)")
        api.action("newCollection", {"name": SCRATCH_COLLECTION})
        state = poll(api.state, lambda s: s.get("collectionName") == SCRATCH_COLLECTION)
        created_id = state.get("collectionId") or ""
        r.check(
            "newCollection creates it and switches to it",
            state.get("collectionName") == SCRATCH_COLLECTION and bool(created_id),
            f"collectionId={created_id} collectionName={state.get('collectionName')!r}",
        )
        names = [c["name"] for c in state.get("collections") or []]
        r.check("the new collection is in state.collections", SCRATCH_COLLECTION in names, str(names))
        # Read back over HTTP rather than from the state mirror, which
        # carries the summaries but not the collection's own items. Items
        # must be [] and not null — the same round-trip bug
        # TestNewCollectionHasEmptyNotNilItems guards in Go.
        fetched = api.get(f"/api/collections/{created_id}")
        r.check(
            "the created collection round-trips with an empty item list, not null",
            fetched.get("items") == [],
            f"items={fetched.get('items')!r}",
        )

        r.step(f"renameCollection {{name: {SCRATCH_COLLECTION_RENAMED!r}}}")
        api.action("renameCollection", {"name": SCRATCH_COLLECTION_RENAMED})
        state = poll(api.state, lambda s: s.get("collectionName") == SCRATCH_COLLECTION_RENAMED)
        r.check(
            "renameCollection renames the open collection",
            state.get("collectionName") == SCRATCH_COLLECTION_RENAMED,
            str(state.get("collectionName")),
        )
        names = [c["name"] for c in state.get("collections") or []]
        r.check("the switcher's list shows the new name", SCRATCH_COLLECTION_RENAMED in names, str(names))

        r.step("toggleCollectionMenu  (watch: the top bar's collection menu should open)")
        api.action("toggleCollectionMenu")
        state = poll(api.state, lambda s: s.get("showCollectionMenu") is True)
        r.check("state.showCollectionMenu reflects toggleCollectionMenu", state.get("showCollectionMenu") is True, str(state.get("showCollectionMenu")))
        api.action("toggleCollectionMenu")
        poll(api.state, lambda s: s.get("showCollectionMenu") is False)

        r.step("toggleEnvironmentMenu  (watch: the environment menu beside it)")
        api.action("toggleEnvironmentMenu")
        state = poll(api.state, lambda s: s.get("showEnvironmentMenu") is True)
        r.check("state.showEnvironmentMenu reflects toggleEnvironmentMenu", state.get("showEnvironmentMenu") is True, str(state.get("showEnvironmentMenu")))
        api.action("toggleEnvironmentMenu")
        poll(api.state, lambda s: s.get("showEnvironmentMenu") is False)

        r.step(f"deleteCollection {{id: {created_id}}}  (watch: the top bar should fall back to another)")
        api.action("deleteCollection", {"id": created_id})
        state = poll(api.state, lambda s: created_id not in [c["id"] for c in s.get("collections") or []])
        names = [c["name"] for c in state.get("collections") or []]
        r.check("deleteCollection removes it", SCRATCH_COLLECTION_RENAMED not in names, str(names))
        r.check("a collection is still open afterwards", bool(state.get("collectionId")), str(state.get("collectionId")))
        created_id = ""
    finally:
        # Whatever failed above, the workspace goes back to what it was.
        if created_id:
            api.action("deleteCollection", {"id": created_id})
        r.step(f"selectCollection {{id: {original_collection_id}}}  (back to the one this test found open)")
        api.action("selectCollection", {"id": original_collection_id})


def test_settings_window(api: ControlAPI, r: Report) -> None:
    """The settings window: a Workspace tab (current folder + Change…,
    which just re-runs openWorkspace) and an Environments tab (covered by
    test_environment_editor instead, since it needs a real environment_id
    to work with). This covers the window/tab chrome itself plus
    openWorkspace's round trip — a real reload, not just a dialog
    stand-in, which is the whole point of the control API having it."""
    r.section("Settings window (toggleSettings / selectSettingsTab / openWorkspace)")

    r.step("toggleSettings  (watch: the settings window should open)")
    api.action("toggleSettings")
    state = poll(api.state, lambda s: s.get("showSettings") is True)
    r.check("state.showSettings reflects toggleSettings", state.get("showSettings") is True, str(state.get("showSettings")))
    # settingsTab is sticky across opens (same as the request editor's own
    # tab) rather than resetting — set it explicitly rather than assuming
    # whichever tab an earlier section left it on.
    r.step("selectSettingsTab {tab: 'workspace'}")
    api.action("selectSettingsTab", {"tab": "workspace"})
    state = poll(api.state, lambda s: s.get("settingsTab") == "workspace")
    r.check("state.settingsTab reflects selectSettingsTab", state.get("settingsTab") == "workspace", str(state.get("settingsTab")))
    root = state.get("workspaceRoot")
    r.check("state.workspaceRoot is a non-empty path", bool(root), str(root))

    r.step("selectSettingsTab {tab: 'environments'}")
    api.action("selectSettingsTab", {"tab": "environments"})
    state = poll(api.state, lambda s: s.get("settingsTab") == "environments")
    r.check("state.settingsTab reflects selectSettingsTab", state.get("settingsTab") == "environments", str(state.get("settingsTab")))

    r.step("selectSettingsTab {tab: 'workspace'}")
    api.action("selectSettingsTab", {"tab": "workspace"})
    poll(api.state, lambda s: s.get("settingsTab") == "workspace")

    r.step("toggleSettings  (watch: it should close again)")
    api.action("toggleSettings")
    state = poll(api.state, lambda s: s.get("showSettings") is False)
    r.check("state.showSettings reflects the second toggleSettings", state.get("showSettings") is False, str(state.get("showSettings")))

    if not root:
        return
    r.step(f"openWorkspace {{path: {root!r}}}  (re-open the same workspace — a real reload, not just a dialog stand-in)")
    api.action("openWorkspace", {"path": root})
    state = poll(api.state, lambda s: s.get("workspaceRoot") == root)
    r.check("state.workspaceRoot still matches after re-opening it", state.get("workspaceRoot") == root, str(state.get("workspaceRoot")))
