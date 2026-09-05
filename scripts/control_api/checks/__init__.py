"""One module per group of checks.

Grouped by what a reader would go looking for rather than by run order —
the entry point decides that. A module here may hold several `test_*`
functions when they're about the same surface (execute.py covers the
happy path, every HTTP method, and file uploads, because all three are
"send this and look at what came back").

`consistency` is the odd one out: it reads source files rather than
driving the running app, so it needs no `ControlAPI`.
"""
