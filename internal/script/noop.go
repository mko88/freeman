package script

import (
	"context"

	"freeman/internal/domain"
	"freeman/internal/httpengine"
)

// NoopEngine implements Engine with no scripting support. It's what v1
// wires up until a goja-backed Engine replaces it.
type NoopEngine struct{}

func (NoopEngine) RunPreRequest(_ context.Context, _ domain.Item, vars map[string]string) (map[string]string, error) {
	return vars, nil
}

func (NoopEngine) RunTest(_ context.Context, _ domain.Item, _ *httpengine.Response) error {
	return nil
}
