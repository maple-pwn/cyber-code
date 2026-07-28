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
	"claude-code-go/internal/session"
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

func TestPersistentRuntimeLogsAndResumesConversationHistory(t *testing.T) {
	store, err := session.NewStore(t.TempDir(), session.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	firstProvider := &completedProvider{events: []core.Event{
		{Type: core.EventTextDelta, Text: "first answer"},
		{Type: core.EventCompleted, FinishReason: "stop"},
	}}
	first, err := NewPersistent(firstProvider, agent.Options{Model: "test"}, store, "resume-me")
	if err != nil {
		t.Fatal(err)
	}
	collectRuntimeEvents(t, first.Run(context.Background(), "first question"))
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	records, err := store.Events(context.Background(), "resume-me")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || records[0].Event.Type != core.EventUserMessage || records[2].Event.Type != core.EventCompleted {
		t.Fatalf("persisted records = %#v", records)
	}

	secondProvider := &completedProvider{events: []core.Event{{Type: core.EventCompleted, FinishReason: "stop"}}}
	resumed, err := Resume(secondProvider, agent.Options{Model: "test"}, store, "resume-me")
	if err != nil {
		t.Fatal(err)
	}
	history := resumed.History()
	if len(history) != 2 || history[0].Content[0].Text != "first question" || history[1].Content[0].Text != "first answer" {
		t.Fatalf("resumed history = %#v", history)
	}
	collectRuntimeEvents(t, resumed.Run(context.Background(), "second question"))
	secondProvider.mu.Lock()
	request := secondProvider.request
	secondProvider.mu.Unlock()
	if len(request.Messages) != 3 || request.Messages[2].Role != core.RoleUser || request.Messages[2].Content[0].Text != "second question" {
		t.Fatalf("resumed provider request = %#v", request)
	}
	snapshot, err := store.Resume(context.Background(), "resume-me")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LastSequence != 5 || len(snapshot.History) != 4 {
		t.Fatalf("updated snapshot = %#v", snapshot)
	}
	if err := resumed.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentRuntimeForwardsCompactionCoverageFromStore(t *testing.T) {
	store, err := session.NewStore(t.TempDir(), session.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	compactor, err := session.NewCompactor(session.CompactOptions{
		ThresholdTokens:    1,
		KeepRecentMessages: 1,
		Summarize:          func(context.Context, []core.Message) (string, error) { return "summary", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &completedProvider{count: 100, events: []core.Event{{Type: core.EventCompleted, FinishReason: "stop"}}}
	persistent, err := NewPersistent(model, agent.Options{
		InitialHistory: []core.Message{
			{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "old question"}}},
			{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentText, Text: "old answer"}}},
		},
		Compactor: compactor,
	}, store, "compact-runtime")
	if err != nil {
		t.Fatal(err)
	}
	events := collectRuntimeEvents(t, persistent.Run(context.Background(), "new question"))
	if len(events) != 3 || events[1].Type != core.EventCompacted || events[1].CoveredSequence != 1 {
		t.Fatalf("runtime events = %#v", events)
	}
	if err := persistent.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
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

type completedProvider struct {
	mu      sync.Mutex
	count   int
	events  []core.Event
	request core.Request
}

func (model *completedProvider) Name() string { return "completed" }
func (model *completedProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true}, nil
}
func (model *completedProvider) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	model.mu.Lock()
	model.request = request
	events := append([]core.Event(nil), model.events...)
	model.mu.Unlock()
	stream := make(chan core.Event)
	go func() {
		defer close(stream)
		for _, event := range events {
			select {
			case stream <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return stream, nil
}
func (model *completedProvider) CountTokens(context.Context, core.Request) (int, error) {
	return model.count, nil
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
