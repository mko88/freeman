package httpengine

import "time"

// LargeResponseThreshold is the body size past which Execute writes the
// body to a temp file (see Response.Truncated) instead of returning it
// inline. Below it, Body is populated exactly as before — the common
// case for ordinary API responses is unaffected.
const LargeResponseThreshold = 1 << 20 // 1 MiB

// Response is the captured result of executing a request.
type Response struct {
	StatusCode int                 `json:"statusCode"`
	Status     string              `json:"status"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body"`
	Duration   time.Duration       `json:"durationNs"`
	SizeBytes  int                 `json:"sizeBytes"`
	// Truncated is true when Body was left "" and the full body was
	// written to BodyFile instead, because it was over
	// LargeResponseThreshold. Both the desktop app and the headless
	// server keep this body out of their normal request/response and
	// state-mirroring paths until it's explicitly asked for (the
	// desktop control API's GET /api/ui/state, in particular, mirrors
	// the whole editor draft on every keystroke — inlining a multi-MB
	// body there would mean re-encoding it that often, and if the very
	// request being sent targets that same endpoint, embedding it in
	// its own next response, growing without bound).
	Truncated bool `json:"truncated,omitempty"`
	// BodyFile is the full body's path on disk when Truncated is true —
	// read via ReadResponseBodyFile ("" otherwise).
	BodyFile string `json:"bodyFile,omitempty"`
}
