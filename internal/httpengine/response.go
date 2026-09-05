package httpengine

import "time"

// LargeResponseThreshold is the body size past which the desktop app
// keeps a response out of its state mirror (see Response.Truncated).
// Execute itself always returns Body inline — trimming happens a layer
// up, in wailsapp, against the on-disk response cache it already writes.
const LargeResponseThreshold = 1 << 20 // 1 MiB

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
}
