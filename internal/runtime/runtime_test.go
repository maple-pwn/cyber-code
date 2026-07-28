package runtime

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"claude-code-go/internal/agent"
	"claude-code-go/internal/core"
	"claude-code-go/internal/provider"
)

func TestShutdownCancelsRunsBeforeClosingServices(t *testing.T) {
	model := newBlockingProvider()
	service := &recordingCloser{close: func() error {
		select {
		case <-model.exited:
			return nil
		default:
			return errors.New("provider still running")
		}
	}}
	runtime := New(model, agent.Options{Model: "model-test"}, service)
	output := runtime.Run(context.Background(), "hi")
	if event := <-output; event.Type != core.EventUserMessage {
		t.Fatalf("first event = %q", event.Type)
	}
	<-model.started

	shutdown := make(chan error, 1)
	go func() { shutdown <- runtime.Shutdown(context.Background()) }()
	assertChannelCloses(t, output)
	if err := <-shutdown; err != nil {
		t.Fatal(err)
	}
	if service.closeCount() != 1 {
		t.Fatalf("service closed %d times", service.closeCount())
	}
}

func TestShutdownIsIdempotentAndClosesServicesInReverseOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	closer := func(name string) io.Closer {
		return &recordingCloser{close: func() error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			return nil
		}}
	}
	runtime := New(newBlockingProvider(), agent.Options{}, closer("first"), closer("second"))

	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("close order = %#v", order)
	}
}

func TestShutdownHonorsWaitContextButContinuesCleanup(t *testing.T) {
	release := make(chan struct{})
	closed := make(chan struct{})
	runtime := New(newBlockingProvider(), agent.Options{}, &recordingCloser{close: func() error {
		<-release
		close(closed)
		return nil
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := runtime.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v", err)
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not continue after wait context expired")
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunAfterShutdownReturnsClosedRuntimeError(t *testing.T) {
	runtime := New(newBlockingProvider(), agent.Options{})
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := collectRuntimeEvents(t, runtime.Run(context.Background(), "hi"))
	if len(events) != 1 || events[0].Type != core.EventError || events[0].Err == nil || events[0].Err.Kind != core.ErrorKindCanceled {
		t.Fatalf("events = %#v", events)
	}
}

func assertChannelCloses(t *testing.T, events <-chan core.Event) {
	t.Helper()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("unexpected event while shutting down")
		}
	case <-time.After(time.Second):
		t.Fatal("runtime output did not close")
	}
}

func collectRuntimeEvents(t *testing.T, events <-chan core.Event) []core.Event {
	t.Helper()
	var result []core.Event
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return result
			}
			result = append(result, event)
		case <-time.After(time.Second):
			t.Fatal("runtime output did not close")
		}
	}
}

type blockingProvider struct {
	started chan struct{}
	exited  chan struct{}
	once    sync.Once
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{}), exited: make(chan struct{})}
}

func (p *blockingProvider) Name() string { return "blocking" }
func (p *blockingProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true}, nil
}
func (p *blockingProvider) Stream(ctx context.Context, _ core.Request) (<-chan core.Event, error) {
	stream := make(chan core.Event)
	p.once.Do(func() { close(p.started) })
	go func() {
		defer close(stream)
		<-ctx.Done()
		close(p.exited)
	}()
	return stream, nil
}
func (p *blockingProvider) CountTokens(context.Context, core.Request) (int, error) {
	return 0, nil
}

type recordingCloser struct {
	mu    sync.Mutex
	count int
	close func() error
}

func (c *recordingCloser) Close() error {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
	return c.close()
}

func (c *recordingCloser) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}
