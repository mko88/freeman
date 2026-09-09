"""Standing regression checks for bugs this suite has caught before."""

from __future__ import annotations

from .. import ControlAPI, Report, poll
from ..fixtures import find_variable_index


def test_rapid_fire_regression(api: ControlAPI, r: Report, environment_id: str) -> None:
    """Regression check for a real bug found 2026-09-04: firing ui:action
    events back-to-back with no delay used to race, because several
    actions (saveRequest, sendRequest, saveEnvironment,
    selectEnvironment, selectCollection, openWorkspace) do an async round
    trip and dispatchUIAction wasn't awaited between events. A
    removeEnvironmentVariable sent right after a saveEnvironment could
    get silently clobbered when the first saveEnvironment call's stale
    `environment = saved` resolved later. Fixed by serializing ui:action
    events through one promise chain in App.svelte. This replays that
    exact shape at zero delay, regardless of --delay. Fully
    self-contained, same as the other scratch-key tests."""
    r.section("Regression: rapid-fire ui:action ordering (no delay)")

    key = "pyRaceVar"
    # addEnvironmentVariable appends, so the row this burst creates lands
    # at the end — read the length first, because the set below has to
    # name a position and nothing here waits long enough to look one up.
    # (The set actions target by index only; key is a field they write.)
    before = api.get(f"/api/environments/{environment_id}")
    index = len(before.get("variables") or [])

    r.step(f"add/set/save/remove/save {key!r} back-to-back, no waiting between calls")
    api.action("addEnvironmentVariable", {"key": key, "value": "v1"}, wait=False)
    api.action("setEnvironmentVariable", {"index": index, "value": "v2"}, wait=False)
    api.action("saveEnvironment", wait=False)
    api.action("removeEnvironmentVariable", {"key": key}, wait=False)
    api.action("saveEnvironment", wait=False)

    env = poll(
        lambda: api.get(f"/api/environments/{environment_id}"),
        lambda e: find_variable_index(e.get("variables"), key) is None,
        timeout=3.0,
    )
    variables = env.get("variables") or []
    r.check(
        f"{key} ends up absent (rapid add-then-remove wasn't lost/reordered)",
        find_variable_index(variables, key) is None,
        str(variables),
    )
