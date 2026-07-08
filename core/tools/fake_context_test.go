package tools

import (
	"iter"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

// fakeState is a minimal in-memory session.State for tests that need to
// exercise the read-tracking guard -- a nil ctx always bypasses it, so
// those tests need something real backing State().
type fakeState struct {
	mu sync.Mutex
	m  map[string]any
}

func (s *fakeState) Get(key string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return v, nil
}

func (s *fakeState) Set(key string, val any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]any)
	}
	s.m[key] = val
	return nil
}

func (s *fakeState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, v := range s.m {
			if !yield(k, v) {
				return
			}
		}
	}
}

// fakeContext is a fully-implemented (via embedded agent.ContextMock)
// agent.Context whose State() is backed by fakeState, standing in for a
// real session across a sequence of tool calls within one test.
type fakeContext struct {
	agent.ContextMock
	state *fakeState
}

func newFakeContext() *fakeContext {
	return &fakeContext{state: &fakeState{m: make(map[string]any)}}
}

func (c *fakeContext) State() session.State { return c.state }
