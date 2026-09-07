"""The Environments tab: variables and whole-environment CRUD."""

from __future__ import annotations

from typing import Optional

from .. import ControlAPI, Report, poll
from ..fixtures import TEST_VAR_KEY, TEST_VAR_VALUE, SCRATCH_VAR_KEY, find_variable_index


def test_environment_editor(api: ControlAPI, r: Report, environment_id: str) -> None:
    """Adds TEST_VAR_KEY, left in place for the rest of the run — every
    later section that needs {{pyTestBase}} substituted relies on it
    still being there, and the temp workspace is discarded wholesale at
    the end regardless. SCRATCH_VAR_KEY is fully self-contained (added,
    updated, and removed again right here), proving add/set/remove all
    work, independent of that."""
    r.section("Environment editor (settings window's Environments tab / *Variable / saveEnvironment)")

    def env_var(state: dict, key: str) -> Optional[dict]:
        variables = (state.get("environment") or {}).get("variables") or []
        return next((v for v in variables if v.get("key") == key), None)

    r.step("toggleSettings, selectSettingsTab 'environments'  (watch: the settings window opens on that tab)")
    api.action("toggleSettings")
    api.action("selectSettingsTab", {"tab": "environments"})

    # Polling state() after each mutation (not just once at the very
    # end) closes a real race: firing the next action before this one's
    # effect is confirmed in the frontend's own state — not just emitted
    # — let a rapid burst of preceding actions (main()'s Phase 1 creates
    # ten requests right before this test runs) occasionally clobber an
    # add/remove here.
    r.step(f"addEnvironmentVariable {{key: {TEST_VAR_KEY!r}, value: {TEST_VAR_VALUE!r}}}")
    api.action("addEnvironmentVariable", {"key": TEST_VAR_KEY, "value": TEST_VAR_VALUE})
    poll(api.state, lambda s: env_var(s, TEST_VAR_KEY) is not None)

    r.step(f"addEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}, secret: true}}  (scratch)")
    api.action("addEnvironmentVariable", {"key": SCRATCH_VAR_KEY, "value": "temp", "secret": True})
    poll(api.state, lambda s: env_var(s, SCRATCH_VAR_KEY) is not None)
    r.step(f"setEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}, value: 'updated'}}")
    api.action("setEnvironmentVariable", {"key": SCRATCH_VAR_KEY, "value": "updated"})
    poll(api.state, lambda s: (env_var(s, SCRATCH_VAR_KEY) or {}).get("value") == "updated")
    r.step(f"removeEnvironmentVariable {{key: {SCRATCH_VAR_KEY!r}}}")
    api.action("removeEnvironmentVariable", {"key": SCRATCH_VAR_KEY})
    poll(api.state, lambda s: env_var(s, SCRATCH_VAR_KEY) is None)

    r.step("saveEnvironment")
    api.action("saveEnvironment")

    r.step("toggleSettings  (watch: the settings window should close)")
    api.action("toggleSettings")

    env = poll(
        lambda: api.get(f"/api/environments/{environment_id}"),
        lambda e: find_variable_index(e.get("variables"), TEST_VAR_KEY) is not None,
    )
    variables = env.get("variables") or []
    r.check(
        f"{TEST_VAR_KEY} = {TEST_VAR_VALUE!r} persisted",
        any(v.get("key") == TEST_VAR_KEY and v.get("value") == TEST_VAR_VALUE for v in variables),
        str(variables),
    )
    r.check(
        f"{SCRATCH_VAR_KEY} was removed, not left behind",
        find_variable_index(variables, SCRATCH_VAR_KEY) is None,
        str(variables),
    )

    # --- new / rename / delete a whole environment ---
    scratch_env_name = "Python Scratch Environment"
    r.step("newEnvironment  (watch: a 'New environment' is created and selected)")
    api.action("newEnvironment")
    state = poll(
        api.state,
        lambda s: (s.get("environment") or {}).get("id") not in (None, environment_id)
        and (s.get("environment") or {}).get("name") == "New environment",
    )
    scratch_env_id = (state.get("environment") or {}).get("id")
    r.check("newEnvironment created and switched to a fresh environment", bool(scratch_env_id) and scratch_env_id != environment_id, str(scratch_env_id))

    r.step(f"setEnvironmentField {{field: 'name', value: {scratch_env_name!r}}}, saveEnvironment")
    api.action("setEnvironmentField", {"field": "name", "value": scratch_env_name})
    poll(api.state, lambda s: (s.get("environment") or {}).get("name") == scratch_env_name)
    api.action("saveEnvironment")
    envs = poll(lambda: api.get("/api/environments"), lambda es: any(e.get("name") == scratch_env_name for e in es))
    r.check("the renamed environment shows up in GET /api/environments", any(e.get("name") == scratch_env_name for e in envs), str(envs))

    # Opening an environment in the settings list is not the same act as
    # making it active — that's the whole reason the list replaced a
    # picker. environment follows what's open; environmentId stays put.
    r.step(f"expandEnvironment {{id: {environment_id}}}  (open the other one without switching to it)")
    api.action("expandEnvironment", {"id": environment_id})
    state = poll(api.state, lambda s: (s.get("environment") or {}).get("id") == environment_id)
    r.check(
        "expandEnvironment opens one without making it active",
        (state.get("environment") or {}).get("id") == environment_id and state.get("environmentId") == scratch_env_id,
        f"open={(state.get('environment') or {}).get('id')} active={state.get('environmentId')}",
    )

    r.step("expandEnvironment  (no id — closes whichever is open)")
    api.action("expandEnvironment")
    state = poll(api.state, lambda s: s.get("environment") is None)
    r.check("expandEnvironment with no id closes it", state.get("environment") is None, str(state.get("environment")))
    r.check("closing it left the active one alone", state.get("environmentId") == scratch_env_id, str(state.get("environmentId")))

    r.step("deleteEnvironment  (no id — deletes the active one)")
    api.action("deleteEnvironment")
    envs = poll(lambda: api.get("/api/environments"), lambda es: all(e.get("id") != scratch_env_id for e in es))
    r.check("the scratch environment is gone after deleteEnvironment", all(e.get("id") != scratch_env_id for e in envs), str(envs))

    r.step(f"selectEnvironment {{id: {environment_id}}}  (restore the one with {TEST_VAR_KEY} as active)")
    api.action("selectEnvironment", {"id": environment_id})
    poll(api.state, lambda s: (s.get("environment") or {}).get("id") == environment_id)
