package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/provider"
	"cyber-code/internal/tasks"
	toolpkg "cyber-code/internal/tool"
)

type observableChildProvider struct {
	events []core.Event
}

func (model *observableChildProvider) Name() string { return "observable-child" }

func (model *observableChildProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true, Thinking: true}, nil
}

func (model *observableChildProvider) Stream(ctx context.Context, _ core.Request) (<-chan core.Event, error) {
	stream := make(chan core.Event, len(model.events))
	for _, event := range model.events {
		select {
		case stream <- event:
		case <-ctx.Done():
			close(stream)
			return stream, nil
		}
	}
	close(stream)
	return stream, nil
}

func (model *observableChildProvider) CountTokens(context.Context, core.Request) (int, error) {
	return 0, nil
}

func TestConfiguredTaskServiceForwardsSubagentEngineObservations(t *testing.T) {
	registry := toolpkg.NewRegistry()
	broker, err := permissions.NewBroker(permissions.Options{
		Mode: permissions.PermissionModeDefault, ModeSource: permissions.SourceCliArg,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := configureTaskService(taskCompositionOptions{
		Provider: &observableChildProvider{events: []core.Event{
			{Type: core.EventThinkingDelta, Text: "reasoning"},
			{Type: core.EventTextDelta, Text: "answer"},
			{Type: core.EventUsage, Usage: &core.Usage{InputTokens: 7, OutputTokens: 2}},
			{Type: core.EventCompleted, FinishReason: "stop"},
		}},
		Registry: registry, Broker: broker, Model: "test-model",
		ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	observations := service.Observe(context.Background())

	result, err := runTaskServiceForObservationTest(service)
	if err != nil || result == "" {
		t.Fatalf("task result=%q err=%v", result, err)
	}

	nested := receiveNestedChildEvents(t, observations, 5)
	want := []core.EventType{
		core.EventUserMessage,
		core.EventThinkingDelta,
		core.EventTextDelta,
		core.EventUsage,
		core.EventCompleted,
	}
	for index, eventType := range want {
		if nested[index].Type != eventType {
			t.Fatalf("nested[%d]=%#v, want %s", index, nested[index], eventType)
		}
	}
	if nested[2].Text != "answer" || nested[3].Usage == nil || nested[3].Usage.InputTokens != 7 {
		t.Fatalf("nested observations = %#v", nested)
	}
}

func runTaskServiceForObservationTest(service *tasks.ToolService) (string, error) {
	registry := toolpkg.NewRegistry()
	if err := tasks.RegisterTools(registry, service); err != nil {
		return "", err
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeBypass, ModeSource: permissions.SourceCliArg})
	if err != nil {
		return "", err
	}
	runner := toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})
	result, err := runner.Run(context.Background(), "task_run", json.RawMessage(`{"prompt":"inspect"}`))
	if err != nil {
		return "", err
	}
	return result.Content[0].Text, nil
}

func receiveNestedChildEvents(t *testing.T, observations <-chan core.Event, count int) []core.Event {
	t.Helper()
	result := make([]core.Event, 0, count)
	deadline := time.After(time.Second)
	for len(result) < count {
		select {
		case observation := <-observations:
			if observation.Type == core.EventSubagentEvent && observation.Subagent != nil && observation.Subagent.Event != nil {
				result = append(result, *observation.Subagent.Event)
			}
		case <-deadline:
			t.Fatalf("timed out after %d nested observations", len(result))
		}
	}
	return result
}
