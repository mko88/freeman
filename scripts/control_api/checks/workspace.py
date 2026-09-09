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

    version = api.get("/api/version")
    r.check(
        "GET /api/version reports a build",
        isinstance(version, dict) and bool(version.get("version")),
        str(version),
    )

    theme = api.get("/api/theme")
    r.check("GET /api/theme returns a palette", isinstance(theme, dict) and "accent" in theme, str(theme))

    headers_catalog = api.get("/api/headers")
    names = {e.get("name") for e in headers_catalog} if isinstance(headers_catalog, list) else set()
    r.check("GET /api/headers includes Content-Type", "Content-Type" in names, str(names))

    ui_state = api.state()
    r.check("GET /api/ui/state returns an object", isinstance(ui_state, dict), str(ui_state))

    # The endpoint an agent reads first. It's assembled from the same
    # catalogue the help modal renders, so the thing worth checking is
    # that the catalogue actually reached Go — a document with the prose
    # but no tables means the frontend never reported.
    status, agent_doc = api.raw("GET", "/api/agent")
    doc = agent_doc if isinstance(agent_doc, str) else ""
    r.check("GET /api/agent returns markdown", status == 200 and doc.startswith("# Freeman control API"), f"status={status} head={doc[:60]!r}")
    r.check(
        "GET /api/agent explains the act-then-read loop and the content-type rule",
        "/api/ui/action" in doc and "/api/ui/state" in doc and "Content-Type: application/json" in doc,
        "the hand-written half of the document is missing",
    )
    for name in ("sendRequest", "selectRequestTab", "renameCollection"):
        if f"`{name}`" not in doc:
            r.check(f"GET /api/agent lists the {name} action", False, "the frontend's catalogue never reached Go")
            break
    else:
        r.check("GET /api/agent lists the actions the frontend reported", True)

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

    # toggleSettings is a toggle, so this section has to know where it
    # starts. It can find the window already open — a previous section, a
    # previous interrupted run, or simply someone having left it open
    # before running the suite — and asserting "one toggle opens it"
    # would then be asserting the opposite of what happened.
    if api.state().get("showSettings"):
        r.step("toggleSettings  (it was already open — closing it, so this section starts from shut)")
        api.action("toggleSettings")
        poll(api.state, lambda s: s.get("showSettings") is False)

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

    r.step("selectSettingsTab {tab: 'collections'}  (where collections are created/renamed/deleted)")
    api.action("selectSettingsTab", {"tab": "collections"})
    state = poll(api.state, lambda s: s.get("settingsTab") == "collections")
    r.check("state.settingsTab reflects selectSettingsTab 'collections'", state.get("settingsTab") == "collections", str(state.get("settingsTab")))

    r.step("selectSettingsTab {tab: 'requests'}  (what a new request's options start as)")
    api.action("selectSettingsTab", {"tab": "requests"})
    state = poll(api.state, lambda s: s.get("settingsTab") == "requests")
    r.check("state.settingsTab reflects selectSettingsTab 'requests'", state.get("settingsTab") == "requests", str(state.get("settingsTab")))

    r.step("selectSettingsTab {tab: 'appearance'}  (fonts and scale)")
    api.action("selectSettingsTab", {"tab": "appearance"})
    state = poll(api.state, lambda s: s.get("settingsTab") == "appearance")
    r.check("state.settingsTab reflects selectSettingsTab 'appearance'", state.get("settingsTab") == "appearance", str(state.get("settingsTab")))

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


def test_workspace_settings(api: ControlAPI, r: Report) -> None:
    """The app-wide defaults: every setting a new request's Options tab
    starts from, plus the two response-size limits. They live in
    settings.yaml in the workspace, so this only ever touches the
    disposable one main() switched to — but it restores what it found
    anyway, because later checks send real requests through the timeout
    it sets here."""
    r.section("Workspace settings (GET/PUT /api/settings / setWorkspaceSetting)")

    keys = {
        "requestTimeoutMs",
        "maxRedirects",
        "inlineResponseBytes",
        "maxResponseBytes",
        "followRedirects",
        "storeCookies",
        "skipTlsVerify",
        "caCertFile",
        "useCustomCA",
        "clientCertFile",
        "clientCertKeyFile",
        "fontUi",
        "fontMono",
        "fontScalePercent",
    }
    original = api.get("/api/settings")
    r.check(
        f"GET /api/settings returns every default ({len(keys)} of them)",
        isinstance(original, dict) and keys <= set(original),
        str(original),
    )
    if not isinstance(original, dict) or not keys <= set(original):
        return

    # Driven with the window open on the tab that holds each field, so a
    # run shows the setting being changed rather than a dialog nobody
    # opened writing to a file nobody sees. Only closed again if this
    # section is what opened it — a window someone left open is theirs.
    opened_settings = not api.state().get("showSettings")

    try:
        if opened_settings:
            r.step("toggleSettings  (watch: these are the fields the settings window shows)")
            api.action("toggleSettings")
            poll(api.state, lambda s: s.get("showSettings") is True)
        r.step("selectSettingsTab {tab: 'requests'}")
        api.action("selectSettingsTab", {"tab": "requests"})
        poll(api.state, lambda s: s.get("settingsTab") == "requests")

        r.step("setWorkspaceSetting {field: 'maxRedirects', value: 7}")
        api.action("setWorkspaceSetting", {"field": "maxRedirects", "value": 7})
        state = poll(api.state, lambda s: (s.get("workspaceSettings") or {}).get("maxRedirects") == 7)
        r.check(
            "state.workspaceSettings reflects setWorkspaceSetting",
            (state.get("workspaceSettings") or {}).get("maxRedirects") == 7,
            str(state.get("workspaceSettings")),
        )
        r.check(
            "...and it reached settings.yaml, not just the UI",
            api.get("/api/settings").get("maxRedirects") == 7,
            str(api.get("/api/settings")),
        )

        # The settings are three types now, not just numbers, and the
        # value has to match the field rather than be coerced — a
        # Number('') of 0 would have turned every path into one.
        r.step("setWorkspaceSetting {field: 'storeCookies', value: false}  (a switch, not a number)")
        api.action("setWorkspaceSetting", {"field": "storeCookies", "value": False})
        state = poll(api.state, lambda s: (s.get("workspaceSettings") or {}).get("storeCookies") is False)
        r.check(
            "a boolean default round trips as a boolean",
            (state.get("workspaceSettings") or {}).get("storeCookies") is False
            and api.get("/api/settings").get("storeCookies") is False,
            str(api.get("/api/settings")),
        )

        r.step("setWorkspaceSetting {field: 'caCertFile', value: '{{pyCertDir}}/server.pem'}  (a path)")
        ca = "{{pyCertDir}}/server.pem"
        api.action("setWorkspaceSetting", {"field": "caCertFile", "value": ca})
        state = poll(api.state, lambda s: (s.get("workspaceSettings") or {}).get("caCertFile") == ca)
        r.check(
            "a path default round trips as the string it was given, {{var}} and all",
            (state.get("workspaceSettings") or {}).get("caCertFile") == ca
            and api.get("/api/settings").get("caCertFile") == ca,
            str(api.get("/api/settings")),
        )

        # The typography is the one group whose effect is the window
        # itself, so it has to survive the same round trip as the rest.
        r.step("selectSettingsTab {tab: 'appearance'}  (where the scale lives)")
        api.action("selectSettingsTab", {"tab": "appearance"})
        poll(api.state, lambda s: s.get("settingsTab") == "appearance")

        r.step("setWorkspaceSetting {field: 'fontScalePercent', value: 125}")
        api.action("setWorkspaceSetting", {"field": "fontScalePercent", "value": 125})
        state = poll(api.state, lambda s: (s.get("workspaceSettings") or {}).get("fontScalePercent") == 125)
        r.check(
            "the interface scale round trips",
            (state.get("workspaceSettings") or {}).get("fontScalePercent") == 125
            and api.get("/api/settings").get("fontScalePercent") == 125,
            str(api.get("/api/settings")),
        )

        r.step("PUT /api/settings with fontScalePercent 500  (expect it clamped to the ceiling)")
        status, saved = api.raw("PUT", "/api/settings", {**original, "fontScalePercent": 500})
        r.check(
            "an unusable scale is clamped rather than refused",
            status == 200 and isinstance(saved, dict) and saved.get("fontScalePercent") == 200,
            f"status={status} body={saved}",
        )

        # Go clamps rather than rejecting, and returns what it kept: a
        # ceiling below the inline threshold would truncate a body the
        # pane was about to render whole.
        r.step("PUT /api/settings with maxResponseBytes below inlineResponseBytes  (expect it clamped up)")
        status, saved = api.raw(
            "PUT",
            "/api/settings",
            {**original, "inlineResponseBytes": 4 * 1024 * 1024, "maxResponseBytes": 1024 * 1024},
        )
        r.check(
            "PUT /api/settings raises a ceiling that sits below the inline threshold",
            status == 200 and isinstance(saved, dict) and saved.get("maxResponseBytes") == 4 * 1024 * 1024,
            f"status={status} body={saved}",
        )

        r.step("PUT /api/settings with a negative timeout  (0 and negative both mean no deadline)")
        status, saved = api.raw("PUT", "/api/settings", {**original, "requestTimeoutMs": -1})
        r.check(
            "a negative timeout is clamped to 0 rather than refused",
            status == 200 and isinstance(saved, dict) and saved.get("requestTimeoutMs") == 0,
            f"status={status} body={saved}",
        )
    finally:
        r.step("PUT /api/settings  (restoring what this workspace had)")
        api.raw("PUT", "/api/settings", original)
        if opened_settings:
            r.step("selectSettingsTab 'workspace', toggleSettings  (closing the window this section opened)")
            api.action("selectSettingsTab", {"tab": "workspace"})
            api.action("toggleSettings")
