package httpengine

import "time"

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
}
