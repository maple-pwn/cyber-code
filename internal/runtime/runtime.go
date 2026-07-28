// Package runtime coordinates the canonical agent and owned service lifetime.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"claude-code-go/internal/agent"
	"claude-code-go/internal/core"
	"claude-code-go/internal/hooks"
	"claude-code-go/internal/provider"
	"claude-code-go/internal/session"
)

// Runtime owns one agent engine and the resources used by that engine.
type Runtime struct {
	engine *agent.Engine

	rootCtx context.Context
	cancel  context.CancelFunc
	closers []io.Closer

	mu        sync.Mutex
	closed    bool
	runs      sync.WaitGroup
	turn      chan struct{}
	session   *persistentSession
	hooks     *hooks.Runner
	sessionID string
	started   bool

	shutdownOnce sync.Once
	shutdownDone chan struct{}
	shutdownErr  error
}

type persistentSession struct {
	store *session.Store
	id    string
}

// New constructs a runtime. Owned services are closed in reverse order.
func New(modelProvider provider.Provider, options agent.Options, services ...io.Closer) *Runtime {
	return newRuntime(modelProvider, options, nil, services...)
}

// NewPersistent constructs a runtime whose canonical events and completed
// conversation snapshots are stored under sessionID.
func NewPersistent(modelProvider provider.Provider, options agent.Options, store *session.Store, sessionID string, services ...io.Closer) (*Runtime, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if options.SessionID != "" && options.SessionID != sessionID {
		return nil, fmt.Errorf("agent session ID %q does not match persistent session %q", options.SessionID, sessionID)
	}
	options.SessionID = sessionID
	if _, err := store.Events(context.Background(), sessionID); err != nil {
		return nil, fmt.Errorf("open session %q: %w", sessionID, err)
	}
	return newRuntime(modelProvider, options, &persistentSession{store: store, id: sessionID}, services...), nil
}

// Resume restores the last atomic conversation snapshot before constructing a
// persistent runtime for the same session.
func Resume(modelProvider provider.Provider, options agent.Options, store *session.Store, sessionID string, services ...io.Closer) (*Runtime, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	snapshot, err := store.Resume(context.Background(), sessionID)
	if err != nil {
		return nil, fmt.Errorf("resume session %q: %w", sessionID, err)
	}
	options.InitialHistory = snapshot.History
	return NewPersistent(modelProvider, options, store, sessionID, services...)
}

func newRuntime(modelProvider provider.Provider, options agent.Options, persisted *persistentSession, services ...io.Closer) *Runtime {
	rootCtx, cancel := context.WithCancel(context.Background())
	closers := make([]io.Closer, 0, len(services)+1)
	if closer, ok := modelProvider.(io.Closer); ok {
		closers = append(closers, closer)
	}
	closers = append(closers, services...)
	turn := make(chan struct{}, 1)
	turn <- struct{}{}
	return &Runtime{
		engine:       agent.NewEngine(modelProvider, options),
		rootCtx:      rootCtx,
		cancel:       cancel,
		closers:      closers,
		turn:         turn,
		session:      persisted,
		hooks:        options.Hooks,
		sessionID:    options.SessionID,
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
	go func() {
		defer r.runs.Done()
		defer close(output)
		defer cancel()
		defer stopCallerCancellation()
		select {
		case <-runCtx.Done():
			return
		case <-r.turn:
		}
		defer func() { r.turn <- struct{}{} }()
		if r.hooks != nil {
			if !r.started {
				if _, err := r.hooks.Run(runCtx, hooks.HookInput{EventName: hooks.HookEventSessionStart, SessionID: r.sessionID}); err != nil {
					r.sendHookError(ctx, output, hooks.HookEventSessionStart, err)
					return
				}
				r.started = true
			}
			outcome, err := r.hooks.Run(runCtx, hooks.HookInput{
				EventName: hooks.HookEventUserPromptSubmit, SessionID: r.sessionID,
				Prompt: prompt, ToolInput: map[string]any{"prompt": prompt},
			})
			if err != nil {
				r.sendHookError(ctx, output, hooks.HookEventUserPromptSubmit, err)
				return
			}
			if outcome.Denied {
				r.sendHookError(ctx, output, hooks.HookEventUserPromptSubmit, fmt.Errorf("%s", outcome.Reason))
				return
			}
			if outcome.Changed {
				updated, ok := outcome.UpdatedInput["prompt"].(string)
				if !ok {
					r.sendHookError(ctx, output, hooks.HookEventUserPromptSubmit, fmt.Errorf("transformed prompt must be a string"))
					return
				}
				prompt = updated
			}
			if len(outcome.AdditionalContext) > 0 {
				prompt += "\n\n" + strings.Join(outcome.AdditionalContext, "\n")
			}
		}

		source := r.engine.Run(runCtx, prompt)
		for event := range source {
			if runCtx.Err() != nil {
				continue
			}
			if r.hooks != nil && (event.Type == core.EventCompleted || event.Type == core.EventError) {
				outcome, err := r.hooks.Run(runCtx, hooks.HookInput{
					EventName: hooks.HookEventStop, SessionID: r.sessionID, Message: event.FinishReason,
				})
				if err != nil || outcome.Denied {
					if err == nil {
						err = fmt.Errorf("%s", outcome.Reason)
					}
					event = hookErrorEvent(hooks.HookEventStop, err)
				}
			}
			if r.session != nil {
				record, err := r.session.store.Append(runCtx, r.session.id, event)
				if err != nil {
					r.sendPersistenceError(ctx, output, "append event", err)
					cancel()
					continue
				}
				event = record.Event
				if event.Type == core.EventCompleted || event.Type == core.EventError {
					snapshot := session.Snapshot{SessionID: r.session.id, LastSequence: record.Sequence, History: r.engine.History()}
					if err := r.session.store.SaveSnapshot(runCtx, snapshot); err != nil {
						r.sendPersistenceError(ctx, output, "save snapshot", err)
						cancel()
						continue
					}
				}
			}
			select {
			case output <- event:
			case <-runCtx.Done():
			}
		}
	}()
	return output
}

func (r *Runtime) sendHookError(ctx context.Context, output chan<- core.Event, event hooks.HookEvent, cause error) {
	hookEvent := hookErrorEvent(event, cause)
	select {
	case output <- hookEvent:
	case <-ctx.Done():
	case <-r.rootCtx.Done():
	}
}

func hookErrorEvent(event hooks.HookEvent, cause error) core.Event {
	return core.Event{Type: core.EventError, Err: &core.Error{
		Kind: core.ErrorKindTool, Op: "runtime.hook", Message: fmt.Sprintf("%s hook failed", event), Cause: cause,
	}}
}

func (r *Runtime) sendPersistenceError(ctx context.Context, output chan<- core.Event, operation string, cause error) {
	event := core.Event{Type: core.EventError, Err: &core.Error{
		Kind: core.ErrorKindInternal, Op: "runtime.session", Message: operation + " failed", Cause: cause,
	}}
	select {
	case output <- event:
	case <-ctx.Done():
	case <-r.rootCtx.Done():
	}
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
