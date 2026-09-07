package httpengine

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// A text body is unaffected: it still marshals as `body`, with no
// bodyBase64 key at all.
func TestMarshalJSONLeavesTextBodyAlone(t *testing.T) {
	data, err := json.Marshal(Response{Body: `{"ok":true}`})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["body"] != `{"ok":true}` {
		t.Fatalf("body should round-trip unchanged, got %q", got["body"])
	}
	if _, ok := got["bodyBase64"]; ok {
		t.Fatalf("a text body should carry no bodyBase64: %s", data)
	}
}

// The bug this exists for: a PNG's bytes are not valid UTF-8, and
// encoding/json replaces every invalid byte with U+FFFD. Marshalled
// straight, the body arrived corrupted with nothing to signal it.
func TestMarshalJSONKeepsBinaryBodyRecoverable(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\xff\xd8\xff"

	data, err := json.Marshal(Response{Body: png, SizeBytes: len(png)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got struct {
		Body       string `json:"body"`
		BodyBase64 string `json:"bodyBase64"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Body != "" {
		t.Fatalf("body should be empty for a binary payload, got %q", got.Body)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.BodyBase64)
	if err != nil {
		t.Fatalf("bodyBase64 is not valid base64: %v", err)
	}
	if string(decoded) != png {
		t.Fatalf("the bytes did not survive the round trip:\n got %q\nwant %q", decoded, png)
	}
	if strings.Contains(string(data), "\\ufffd") {
		t.Fatalf("no replacement character should reach the JSON: %s", data)
	}
}

// In process, Body still holds the real bytes — wailsapp's response
// cache writes []byte(resp.Body) to disk, so a split that happened
// before marshalling would cache an empty file.
func TestBodyStaysIntactInProcess(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n"
	r := Response{Body: png}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if r.Body != png {
		t.Fatalf("marshalling must not disturb the value it was given, got %q", r.Body)
	}
	if r.BodyBase64 != "" {
		t.Fatalf("BodyBase64 is an output-only field, got %q", r.BodyBase64)
	}
}
