"""Support package for scripts/test_control_api.py.

Split out of that script once it passed ~1,700 lines: the entry point
keeps the argument parsing and the three-phase run order, and everything
else lives here — the HTTP client, the reporter, shared fixtures, and one
module per group of checks under `checks/`.

Nothing here imports anything outside the standard library, so the suite
still runs against a plain Python install with no setup.
"""

from .client import ApiError, ControlAPI
from .report import Report, poll

__all__ = ["ApiError", "ControlAPI", "Report", "poll"]
