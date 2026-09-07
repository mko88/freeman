package httpengine

import (
	"encoding/base64"
	"encoding/json"
	"time"
	"unicode/utf8"
)

// LargeResponseThreshold is the body size past which the desktop app
// keeps a response out of its state mirror (see Response.Truncated).
// Execute itself always returns Body inline — trimming happens a layer
// up, in wailsapp, against the on-disk response cache it already writes.
const LargeResponseThreshold = 1 << 20 // 1 MiB

// MaxResponseBytes is the hard ceiling on how much of a response body
// Execute will hold in memory — applied to the bytes off the wire and
// again to the decompressed result, so a small compressed payload can't
// inflate past it either (brotli and gzip both reach ratios where a few
// hundred KB becomes gigabytes). Past this, Execute keeps what it has
// and sets Response.Capped rather than failing: a clipped body a user
// can look at beats an out-of-memory kill.
//
// A var, not a const, so a caller (or a test) can raise or lower it.
var MaxResponseBytes int64 = 64 << 20 // 64 MiB

// RequestTimeout bounds a whole execution — connect, send, and read the
// response body. Applied as a context deadline in Execute, so it
// composes with whatever context the caller passed rather than replacing
// it. Zero disables it.
var RequestTimeout = 30 * time.Second

// Response is the captured result of executing a request.
type Response struct {
	StatusCode int                 `json:"statusCode"`
	Status     string              `json:"status"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body"`
	Duration   time.Duration       `json:"durationNs"`
	SizeBytes  int                 `json:"sizeBytes"`
	// Truncated/BodyFile are set by wailsapp, not Execute: when a body is
	// over LargeResponseThreshold the desktop app blanks Body and points
	// BodyFile at the response cache file that holds the real thing (see
	// wailsapp.saveResponseCache), so the Wails bridge and GET
	// /api/ui/state — which re-mirrors the whole editor draft on every
	// keystroke — never carry a multi-MB string. The headless server
	// leaves both zero and returns Body inline.
	Truncated bool   `json:"truncated,omitempty"`
	BodyFile  string `json:"bodyFile,omitempty"`
	// Capped means the response was bigger than MaxResponseBytes and
	// Body holds only the first MaxResponseBytes of it — distinct from
	// Truncated, where the whole body exists and merely lives in
	// BodyFile instead. SizeBytes counts what was kept, since the rest
	// was never read; the response's own Content-Length header, if it
	// sent one, still says how big it really was.
	Capped bool `json:"capped,omitempty"`
	// BodyBase64 only ever exists in JSON output, put there by
	// MarshalJSON when Body isn't text. In process it stays empty and
	// Body holds the real bytes — see MarshalJSON.
	BodyBase64 string `json:"bodyBase64,omitempty"`
}

// MarshalJSON keeps a binary body recoverable.
//
// Body is a Go string, which holds arbitrary bytes quite happily, and
// every in-process reader depends on that — wailsapp.saveResponseCache
// writes []byte(resp.Body) straight to disk, which is how an image gets
// cached at all. JSON has no such tolerance: encoding/json replaces
// every byte that isn't valid UTF-8 with U+FFFD, so a PNG crossing the
// Wails bridge or GET /api/ui/state arrived as a wall of replacement
// characters — corrupted, and with nothing to say it had been.
//
// So the split happens here, at the one boundary where it matters,
// rather than in Execute: a body that is valid UTF-8 marshals as
// `body` exactly as before, and one that isn't marshals as
// `bodyBase64` with `body` empty. A caller that finds bodyBase64 set
// knows the bytes are binary and can decode them back exactly.
func (r Response) MarshalJSON() ([]byte, error) {
	// A local type with no methods, so marshalling it doesn't recurse.
	type wire Response
	out := wire(r)
	if !utf8.ValidString(r.Body) {
		out.BodyBase64 = base64.StdEncoding.EncodeToString([]byte(r.Body))
		out.Body = ""
	}
	return json.Marshal(out)
}
