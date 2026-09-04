// Package script defines the seam for pre-request and test scripting
// (Postman-style JS attached to a request). v1 ships only NoopEngine;
// a goja-backed Engine can be added later without changing callers.
package script

import (
	"context"

	"freeman/internal/domain"
	"freeman/internal/httpengine"
)

// Engine runs a request's PreRequestScript before it's executed and its
// TestScript after a response comes back.
type Engine interface {
	// RunPreRequest may return a modified copy of vars (e.g. a script that
	// computes a signature or timestamp variable).
	RunPreRequest(ctx context.Context, item domain.Item, vars map[string]string) (map[string]string, error)
	// RunTest runs after the response is captured; a returned error means
	// a test assertion failed.
	RunTest(ctx context.Context, item domain.Item, resp *httpengine.Response) error
}
