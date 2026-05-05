package macserver

import (
	"context"
	"encoding/json"
	"errors"
)

// FakeSupervisor implements the same Call/Close interface as Supervisor for
// tests. Use SetHandler to control responses per method.
type FakeSupervisor struct {
	handlers map[string]func(json.RawMessage) (json.RawMessage, error)
}

func NewFakeSupervisor() *FakeSupervisor {
	return &FakeSupervisor{handlers: map[string]func(json.RawMessage) (json.RawMessage, error){}}
}

func (f *FakeSupervisor) SetHandler(method string, fn func(json.RawMessage) (json.RawMessage, error)) {
	f.handlers[method] = fn
}

func (f *FakeSupervisor) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	h, ok := f.handlers[method]
	if !ok {
		return nil, errors.New("fake: no handler for " + method)
	}
	return h(params)
}

func (f *FakeSupervisor) Close() error { return nil }

// HelperClient is what tools.go depends on; both Supervisor and FakeSupervisor satisfy it.
type HelperClient interface {
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	Close() error
}
