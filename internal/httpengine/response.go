package httpengine

import "time"

// Response is the captured result of executing a request.
type Response struct {
	StatusCode int                 `json:"statusCode"`
	Status     string              `json:"status"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body"`
	Duration   time.Duration       `json:"durationNs"`
	SizeBytes  int                 `json:"sizeBytes"`
}
