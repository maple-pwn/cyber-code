// Package runtime coordinates the canonical agent and owned service lifetime.
package runtime

import (
	"context"
	"errors"
	"io"
	"sync"

	"claude-code-go/internal/agent"
	"claude-code-go/internal/core"
	"claude-code-go/internal/provider"
)

// Runtime owns one agent engine and the resources used by that engine.
type Runtime struct {
	engine *agent.Engine

	rootCtx context.Context
	cancel  context.CancelFunc
	closers []io.Closer

	mu     sync.Mutex
	closed bool
	runs   sync.WaitGroup

	shutdownOnce sync.Once
	shutdownDone chan struct{}
	shutdownErr  error
}

// New constructs a runtime. Owned services are closed in reverse order.
func New(modelProvider provider.Provider, options agent.Options, services ...io.Closer) *Runtime {
	rootCtx, cancel := context.WithCancel(context.Background())
	closers := make([]io.Closer, 0, len(services)+1)
	if closer, ok := modelProvider.(io.Closer); ok {
		closers = append(closers, closer)
	}
	closers = append(closers, services...)
	return &Runtime{
		engine:       agent.NewEngine(modelProvider, options),
		rootCtx:      rootCtx,
		cancel:       cancel,
		closers:      closers,
		shutdownDone: make(chan struct{}),
	}
}

// Run starts one agent turn governed by both ctx and the runtime lifetime.
func (r *Runtime) Run(ctx context.Context, prompt string) <-chan core.Event {
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return closedRuntimeEvents()
	}
	runCtx, cancel := context.WithCancel(r.rootCtx)
	stopCallerCancellation := context.AfterFunc(ctx, cancel)
	r.runs.Add(1)
	r.mu.Unlock()

	output := make(chan core.Event)
	source := r.engine.Run(runCtx, prompt)
	go func() {
		defer r.runs.Done()
		defer close(output)
		defer cancel()
		defer stopCallerCancellation()

		for event := range source {
			if runCtx.Err() != nil {
				continue
			}
			select {
			case output <- event:
			case <-runCtx.Done():
			}
		}
	}()
	return output
}

// History returns an independent snapshot of the canonical conversation.
func (r *Runtime) History() []core.Message {
	return r.engine.History()
}

// Shutdown cancels active turns and starts cleanup exactly once. ctx limits
// how long this caller waits; cleanup continues so later calls can await it.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.shutdownOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.mu.Unlock()
		go r.finishShutdown()
	})

	select {
	case <-r.shutdownDone:
		return r.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) finishShutdown() {
	r.runs.Wait()
	var closeErrors []error
	for index := len(r.closers) - 1; index >= 0; index-- {
		if r.closers[index] == nil {
			continue
		}
		if err := r.closers[index].Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	r.shutdownErr = errors.Join(closeErrors...)
	close(r.shutdownDone)
}

func closedRuntimeEvents() <-chan core.Event {
	events := make(chan core.Event, 1)
	events <- core.Event{Type: core.EventError, Err: &core.Error{
		Kind:    core.ErrorKindCanceled,
		Op:      "runtime.run",
		Message: "runtime is shut down",
	}}
	close(events)
	return events
}
