package provider_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"claude-code-go/internal/config"
	"claude-code-go/internal/core"
	"claude-code-go/internal/provider"
	"claude-code-go/internal/provider/testkit"
)

func TestRegistryRejectsDuplicateProvider(t *testing.T) {
	registry := provider.NewRegistry()
	factory := func(profile config.Profile) (provider.Provider, error) {
		return &fakeProvider{name: profile.Provider}, nil
	}
	if err := registry.Register("fake", factory); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	if err := registry.Register("fake", factory); err == nil {
		t.Fatal("expected duplicate provider registration to fail")
	}
}

func TestRegistryCreatesProviderByNameWithoutRetainingInstances(t *testing.T) {
	registry := provider.NewRegistry()
	created := 0
	if err := registry.Register("fake", func(profile config.Profile) (provider.Provider, error) {
		created++
		return &fakeProvider{name: profile.Model}, nil
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	profile := config.Profile{Provider: "fake", Model: "model-name"}
	first, err := registry.Create("fake", profile)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	second, err := registry.Create("fake", profile)
	if err != nil {
		t.Fatalf("create second provider: %v", err)
	}
	if first == second || created != 2 {
		t.Fatalf("registry retained an instance: first=%p second=%p created=%d", first, second, created)
	}
	if first.Name() != "model-name" {
		t.Fatalf("factory did not receive profile: %q", first.Name())
	}
	if _, err := registry.Create("missing", profile); err == nil {
		t.Fatal("expected unknown provider to fail")
	}
}

func TestProviderStreamHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := (&fakeProvider{name: "fake"}).Stream(ctx, core.Request{})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	cancel()

	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("stream emitted an event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not close after cancellation")
	}
}

func TestScriptedServerCapturesRequestsAndWritesSSE(t *testing.T) {
	server := testkit.NewServer(
		func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/event-stream")
			if err := testkit.WriteSSE(response, "message", `{"type":"text","text":"hello"}`); err != nil {
				t.Errorf("write SSE: %v", err)
			}
		},
	)
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL()+"/v1/messages", strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("X-Test", "captured")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got := string(body); got != "event: message\ndata: {\"type\":\"text\",\"text\":\"hello\"}\n\n" {
		t.Fatalf("unexpected SSE frame: %q", got)
	}

	captured := server.Requests()
	if len(captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(captured))
	}
	if captured[0].Method != http.MethodPost || captured[0].Path != "/v1/messages" || captured[0].Header.Get("X-Test") != "captured" || string(captured[0].Body) != `{"model":"test"}` {
		t.Fatalf("request was not captured faithfully: %#v", captured[0])
	}
}

func TestScriptedServerRejectsUnexpectedExtraRequest(t *testing.T) {
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	defer server.Close()

	for index, wantStatus := range []int{http.StatusNoContent, http.StatusInternalServerError} {
		response, err := server.Client().Get(server.URL() + "/step")
		if err != nil {
			t.Fatalf("request %d: %v", index, err)
		}
		response.Body.Close()
		if response.StatusCode != wantStatus {
			t.Fatalf("request %d status = %d, want %d", index, response.StatusCode, wantStatus)
		}
	}
}

type fakeProvider struct {
	name string
}

func (fake *fakeProvider) Name() string {
	return fake.name
}

func (fake *fakeProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true}, nil
}

func (fake *fakeProvider) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	events := make(chan core.Event)
	go func() {
		defer close(events)
		<-ctx.Done()
	}()
	return events, nil
}

func (fake *fakeProvider) CountTokens(context.Context, core.Request) (int, error) {
	return 0, nil
}
