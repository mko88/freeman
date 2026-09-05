"""Pass/fail reporting, and the poll() every verification goes through."""

from __future__ import annotations

import time


class Report:
    def __init__(self, use_color: bool):
        self.passed = 0
        self.failed = 0
        self.use_color = use_color

    def _c(self, code: str, text: str) -> str:
        return f"\033[{code}m{text}\033[0m" if self.use_color else text

    def section(self, title: str) -> None:
        print("\n" + self._c("1;36", f"=== {title} ==="))

    def step(self, description: str) -> None:
        print(self._c("2", f"  > {description}"))

    def check(self, description: str, condition: bool, detail: str = "") -> None:
        if condition:
            self.passed += 1
            print(f"  {self._c('32', '[PASS]')} {description}")
        else:
            self.failed += 1
            suffix = f" — {detail}" if detail else ""
            print(f"  {self._c('31', '[FAIL]')} {description}{suffix}")

    def summary(self) -> int:
        total = self.passed + self.failed
        print("\n" + "-" * 60)
        if self.failed:
            print(self._c("1;31", f"{self.failed}/{total} checks FAILED"))
        else:
            print(self._c("1;32", f"All {total} checks passed"))
        return 1 if self.failed else 0


def poll(fetch, predicate, timeout: float = 6.0, interval: float = 0.1):
    """Call fetch() until predicate(result) is true or timeout elapses,
    returning the last result either way. Needed because POST
    /api/ui/action returns 204 as soon as the event is *emitted*, not
    once the frontend has applied it — an action like saveRequest or
    saveEnvironment does a real Go RPC plus a disk write, which can
    easily take longer than a human-watching --delay (especially
    --delay 0). An immediate verification GET without this would race
    the write instead of testing anything meaningful. The default
    timeout is generous (6s, not ~1s) because ui:action events are
    processed through one serialized queue in App.svelte — an action
    right after `sendRequest` can be stuck waiting behind a real network
    call to whatever API the request targets, not just local disk I/O."""
    deadline = time.time() + timeout
    result = fetch()
    while not predicate(result) and time.time() < deadline:
        time.sleep(interval)
        result = fetch()
    return result
