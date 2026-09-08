"""Test-data identifiers and the small lookup helpers checks share.

Every run creates these fresh inside the disposable temp workspace the
entry point switches to — fixed names/keys just make a mid-run
screenshot recognizable as this script's, not because anything here
needs cleaning up (the whole temp workspace is discarded regardless)."""

from __future__ import annotations

import base64
from pathlib import Path
from typing import Optional


TEST_REQUEST_NAME = "Python Request Editor Test (scratch)"


EXECUTE_TEST_NAME = "Python Execute Test (scratch)"


UI_STATE_GETTER_TEST_NAME = "Python UI State Getter Test (scratch)"


FILE_UPLOAD_TEST_NAME = "Python File Upload Test (scratch)"


DELETE_TEST_NAME = "Python Delete Test (scratch)"


SMALL_WINS_NAME = "Python Small Wins Test (scratch)"


TEST_HEADER_KEY = "X-Py-Test"


SCRATCH_HEADER_KEY = "X-Py-Scratch"


TEST_PARAM_KEY = "pyParam"


SCRATCH_PARAM_KEY = "pyScratchParam"


FORM_FIELD_KEY = "item"


SCRATCH_FIELD_KEY = "scratchField"


TEST_VAR_KEY = "pyTestBase"


# The directory holding the client certificate pair. A second variable
# rather than a literal path in the check, because the certificate paths
# take {{var}} substitution too — and once didn't.
TEST_CERT_VAR_KEY = "pyCertDir"


# There is no TEST_VAR_VALUE constant to go with TEST_VAR_KEY: the value
# is the local test server's base URL (see control_api.server), and the
# OS picks its port, so it's passed down from the entry point instead.
# Every check writes its URL as {{pyTestBase}}/… and lets the app
# substitute it, which is also what proves substitution works.


SCRATCH_VAR_KEY = "pyScratchVar"


HTTP_METHODS = ("GET", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS")


REQUEST_TEST_NAMES: dict[str, str] = {
    "request_editor": TEST_REQUEST_NAME,
    "execute": EXECUTE_TEST_NAME,
    "ui_state_getter": UI_STATE_GETTER_TEST_NAME,
    "file_upload": FILE_UPLOAD_TEST_NAME,
    "large_response": "Python Large Response Test (scratch)",
    "response_cache": "Python Response Cache Test (scratch)",
    "code_tab": "Python Code Tab Test (scratch)",
    "small_wins": SMALL_WINS_NAME,
    "auth_variations": "Python Auth Variations Test (scratch)",
    "body_variations": "Python Body Variations Test (scratch)",
    "option_variations": "Python Option Variations Test (scratch)",
    "encoding_variations": "Python Encoding Variations Test (scratch)",
    **{m: f"Python {m} Test (scratch)" for m in HTTP_METHODS},
}


# parents[1] is scripts/ — this module sits one level down in
# scripts/control_api/, the fixture stays next to the entry point.
RANDOM_FILE_PATH = Path(__file__).resolve().parents[1] / "random-sample.bin"


def find_item(collection: dict, name: str) -> Optional[dict]:
    for item in collection.get("items", []):
        if item.get("name") == name:
            return item
    return None


def find_item_by_id(collection: dict, item_id: str) -> Optional[dict]:
    for item in collection.get("items", []):
        if item.get("id") == item_id:
            return item
    return None


def find_header_index(headers: Optional[list], key: str) -> Optional[int]:
    for i, h in enumerate(headers or []):
        if h.get("key") == key:
            return i
    return None


def find_variable_index(variables: Optional[list], key: str) -> Optional[int]:
    for i, v in enumerate(variables or []):
        if v.get("key") == key:
            return i
    return None


def decode_data_uri_bytes(data_uri: str) -> bytes:
    """Decodes the "data:<mime-type>;base64,<...>" shape the test server
    returns for a body it can't render as text — exactly what a random
    binary fixture produces — back to raw bytes, for an exact comparison
    against the original file instead of a fragile substring match.
    httpbin's own shape, which control_api.server matches here."""
    if ";base64," not in data_uri:
        return data_uri.encode()
    return base64.b64decode(data_uri.split(";base64,", 1)[1])
