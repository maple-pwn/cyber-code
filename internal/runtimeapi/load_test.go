package runtimeapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"cyber-code/internal/productprotocol"
)

func TestRuntimeLoadConcurrentTasksRemainIsolated(t *testing.T) {
	t.Parallel()
	server := newTestLocalServer(t)
	const taskCount = 12
	var wait sync.WaitGroup
	errors := make(chan error, taskCount)
	for index := 1; index <= taskCount; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			envelope := mustLocalEnvelope(t, fmt.Sprintf("concurrent-%d", index), map[string]any{
				"type": "task.create", "objective": fmt.Sprintf("Concurrent task %d", index), "runtimeId": "runtime-1",
			})
			response := server.handleAuthorizedForClient(context.Background(), LocalRequest{
				ID: fmt.Sprintf("request-%d", index), Type: "command", Command: &envelope,
			}, fmt.Sprintf("controller-%d", index))
			if response.Receipt == nil || response.Receipt.Status != "accepted" {
				errors <- fmt.Errorf("task %d receipt = %+v", index, response)
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	for index := 1; index <= taskCount; index++ {
		events := server.Handle(context.Background(), LocalRequest{
			ID: "events", Type: "events", Bearer: "launch-secret", TaskID: fmt.Sprintf("task-%d", index),
		})
		if len(events.Events) != 2 || events.Events[0].TaskID != fmt.Sprintf("task-%d", index) {
			t.Fatalf("task %d events = %+v", index, events.Events)
		}
	}
}

func TestRuntimeLoadLargeEvidenceRoundTripsWithoutMutation(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-load", "operator", nil)
	if _, err := service.CreateTask(context.Background(), "task-large", "Large Evidence"); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("0123456789abcdef", 32*1024)
	evidence := productprotocol.ImmutableEvidence{
		ID: "evidence-large", TaskID: "task-large", Kind: "terminal", Summary: "bounded 512 KiB payload",
		Data: map[string]any{"stdout": payload},
	}
	if _, err := service.CommitEvidence(context.Background(), evidence, productprotocol.EventSourceRef{ToolCallID: "tool-large"}); err != nil {
		t.Fatal(err)
	}
	_, state, err := store.Load(context.Background(), "task-large")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := state.Evidence[evidence.ID].Data["stdout"].(string)
	if !ok || got != payload {
		t.Fatalf("large Evidence did not round trip: bytes=%d", len(got))
	}
}
