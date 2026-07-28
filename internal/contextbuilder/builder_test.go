package contextbuilder

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"cyber-code/internal/core"
	"cyber-code/internal/product"
)

func TestBuilderOrdersSourcesAndPreservesSafetyIdentity(t *testing.T) {
	builder, err := New(Options{Sources: []Source{
		{ID: "runtime", Kind: SourceRuntime, Priority: 70, Trusted: true, Content: "runtime constraints"},
		{ID: "project", Kind: SourceProject, Priority: 30, Path: "/workspace/CYBER.md", Content: "project instructions"},
		{ID: "user", Kind: SourceUser, Priority: 20, Path: "/state/instructions.md", Content: "user instructions"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := builder.Build(context.Background(), BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.System) != 4 || len(plan.Sources) != 4 {
		t.Fatalf("system/sources = %d/%d, want 4/4", len(plan.System), len(plan.Sources))
	}
	if plan.Sources[0].ID != IdentitySourceID || plan.Sources[0].Kind != SourceIdentity || !plan.Sources[0].Trusted {
		t.Fatalf("identity metadata = %+v", plan.Sources[0])
	}
	if !strings.Contains(plan.System[0].Text, product.DefaultSystemPrompt) {
		t.Fatalf("identity block = %q", plan.System[0].Text)
	}
	gotOrder := []string{plan.Sources[1].ID, plan.Sources[2].ID, plan.Sources[3].ID}
	if !reflect.DeepEqual(gotOrder, []string{"user", "project", "runtime"}) {
		t.Fatalf("source order = %v", gotOrder)
	}
	if plan.Sources[1].Trusted || plan.Sources[2].Trusted || !plan.Sources[3].Trusted {
		t.Fatalf("trust metadata was not preserved: %+v", plan.Sources)
	}
}

func TestBuilderRejectsDuplicateAndReservedSourceIDs(t *testing.T) {
	for name, sources := range map[string][]Source{
		"duplicate": {
			{ID: "same", Kind: SourceProject, Content: "one"},
			{ID: "same", Kind: SourceRuntime, Content: "two"},
		},
		"reserved": {{ID: IdentitySourceID, Kind: SourceProject, Content: "replace identity"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(Options{Sources: sources})
			if !errors.Is(err, ErrInvalidSource) {
				t.Fatalf("error = %v, want ErrInvalidSource", err)
			}
		})
	}
}

func TestBuilderClonesInputsAndOutputs(t *testing.T) {
	messages := []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "hello"}}}}
	tools := []core.ToolDefinition{{Name: "read", InputSchema: []byte(`{"type":"object"}`)}}
	builder, err := New(Options{Sources: []Source{{ID: "project", Kind: SourceProject, Content: "project"}}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), BuildInput{Messages: messages, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}

	messages[0].Content[0].Text = "mutated input"
	tools[0].Name = "mutated input"
	if plan.Messages[0].Content[0].Text != "hello" || plan.Tools[0].Name != "read" {
		t.Fatalf("plan aliases input: %+v %+v", plan.Messages, plan.Tools)
	}
	plan.Messages[0].Content[0].Text = "mutated output"
	plan.Tools[0].InputSchema[0] = 'x'

	again, err := builder.Build(context.Background(), BuildInput{Messages: []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "hello"}}}}, Tools: []core.ToolDefinition{{Name: "read", InputSchema: []byte(`{"type":"object"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Messages[0].Content[0].Text != "hello" || !strings.HasPrefix(string(again.Tools[0].InputSchema), "{") {
		t.Fatalf("builder retained output mutation: %+v %+v", again.Messages, again.Tools)
	}
}

func TestBuilderIsDeterministicForEqualPriority(t *testing.T) {
	builder, err := New(Options{Sources: []Source{
		{ID: "z", Kind: SourceProject, Priority: 30, Content: "z"},
		{ID: "a", Kind: SourceProject, Priority: 30, Content: "a"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := builder.Build(context.Background(), BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := builder.Build(context.Background(), BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equal builds differ:\n%+v\n%+v", first, second)
	}
	if first.Sources[1].ID != "a" || first.Sources[2].ID != "z" {
		t.Fatalf("equal-priority order = %+v", first.Sources)
	}
}
