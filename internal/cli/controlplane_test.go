package cli

import (
	"context"
	"strings"
	"testing"

	"cyber-code/internal/controlplane"
)

func TestRegisterGitCommandsDispatchesBoundedWorkflows(t *testing.T) {
	registry := controlplane.NewRegistry()
	service := &fakeGitWorkflow{diff: "diff output", review: "review output", commit: "commit output"}
	if err := registerGitCommands(registry, service); err != nil {
		t.Fatal(err)
	}
	for command, want := range map[string]string{
		"/diff":                 "diff output",
		"/review":               "review output",
		"/commit commit safely": "commit output",
	} {
		events, err := registry.Dispatch(context.Background(), command)
		if err != nil || len(events) == 0 || !strings.Contains(events[0].Text, want) {
			t.Fatalf("%s events = %#v, error = %v", command, events, err)
		}
	}
	if service.message != "commit safely" {
		t.Fatalf("commit message = %q", service.message)
	}
}

type fakeGitWorkflow struct {
	diff, review, commit string
	message              string
}

func (workflow *fakeGitWorkflow) Diff(context.Context) (string, error)   { return workflow.diff, nil }
func (workflow *fakeGitWorkflow) Review(context.Context) (string, error) { return workflow.review, nil }
func (workflow *fakeGitWorkflow) Commit(_ context.Context, message string) (string, error) {
	workflow.message = message
	return workflow.commit, nil
}
