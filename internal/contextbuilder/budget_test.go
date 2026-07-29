package contextbuilder

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cyber-code/internal/core"
)

func TestBudgetExcludesLowerPrioritySourcesBeforeRequiredIdentity(t *testing.T) {
	estimate := func(text string) int { return len(text) }
	builder, err := New(Options{
		ContextWindow:  900,
		ReservedOutput: 100,
		EstimateText:   estimate,
		Sources: []Source{
			{ID: "important", Kind: SourceRuntime, Priority: 10, Required: true, Content: "required"},
			{ID: "large", Kind: SourceProject, Priority: 20, Content: strings.Repeat("p", 2000)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Sources) != 3 || !plan.Sources[2].Truncated {
		t.Fatalf("sources = %+v, want truncated project source", plan.Sources)
	}
	if len(plan.Diagnostics) == 0 || plan.Diagnostics[0].SourceID != "large" {
		t.Fatalf("diagnostics = %+v", plan.Diagnostics)
	}
	if plan.MaxOutputTokens != 100 {
		t.Fatalf("max output tokens = %d, want 100", plan.MaxOutputTokens)
	}
	if !strings.Contains(plan.System[0].Text, "cyber-code") || plan.System[1].Text == "" {
		t.Fatalf("required system blocks were lost: %+v", plan.System)
	}
}

func TestBudgetFailsWhenRequiredMessagesAndSourcesDoNotFit(t *testing.T) {
	builder, err := New(Options{
		ContextWindow:  20,
		ReservedOutput: 10,
		EstimateText:   func(text string) int { return len(text) },
		Sources:        []Source{{ID: "runtime", Kind: SourceRuntime, Required: true, Content: "required runtime"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = builder.Build(context.Background(), BuildInput{Messages: []core.Message{{
		Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "required user message"}},
	}}})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("error = %v, want ErrBudgetExceeded", err)
	}
}

func TestBudgetCanBeMeasuredBeforeCompactionWithoutDroppingContent(t *testing.T) {
	builder, err := New(Options{
		ContextWindow: 20, ReservedOutput: 10,
		EstimateText: func(text string) int { return len(text) },
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), BuildInput{
		AllowOverBudget: true,
		Messages:        []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: strings.Repeat("history", 100)}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Messages) != 1 || !strings.Contains(plan.Messages[0].Content[0].Text, "history") || plan.EstimatedTokens <= 20 {
		t.Fatalf("pre-compact plan = %+v", plan)
	}
}

func TestBudgetDoesNotSplitToolCallResultPair(t *testing.T) {
	builder, err := New(Options{ContextWindow: 4096, ReservedOutput: 512})
	if err != nil {
		t.Fatal(err)
	}
	messages := []core.Message{
		{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "call-1", Name: "read", Arguments: []byte(`{}`)}}}},
		{Role: core.RoleTool, Content: []core.ContentBlock{{Type: core.ContentToolResult, ToolResult: &core.ToolResult{ToolCallID: "call-1", Content: []core.ContentBlock{{Type: core.ContentText, Text: "result"}}}}}},
	}
	plan, err := builder.Build(context.Background(), BuildInput{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Messages) != 2 || plan.Messages[0].Content[0].ToolCall.ID != plan.Messages[1].Content[0].ToolResult.ToolCallID {
		t.Fatalf("tool pair changed: %+v", plan.Messages)
	}
}

func TestBudgetKeepsHigherPrioritySourcesButEmitsLowToHigh(t *testing.T) {
	estimate := func(text string) int { return len(text) }
	identityOnly, err := New(Options{EstimateText: estimate})
	if err != nil {
		t.Fatal(err)
	}
	base, err := identityOnly.Build(context.Background(), BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	window := base.EstimatedTokens + 260
	builder, err := New(Options{
		ContextWindow: window, EstimateText: estimate,
		Sources: []Source{
			{ID: "low", Kind: SourceProject, Priority: 20, Content: strings.Repeat("low", 100)},
			{ID: "high", Kind: SourceRuntime, Priority: 70, Content: "HIGH PRIORITY"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, block := range plan.System {
		joined += block.Text
	}
	if !strings.Contains(joined, "HIGH PRIORITY") {
		t.Fatalf("high priority source was discarded: %+v", plan)
	}
	if len(plan.Sources) == 3 && plan.Sources[1].ID != "low" {
		t.Fatalf("emission order is not low-to-high: %+v", plan.Sources)
	}
}

func TestBudgetPlanReportsUtilizationAndGovernanceThresholds(t *testing.T) {
	estimate := func(text string) int { return len(text) }
	baseline, err := New(Options{EstimateText: estimate})
	if err != nil {
		t.Fatal(err)
	}
	basePlan, err := baseline.Build(context.Background(), BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	inputLimit := basePlan.EstimatedTokens * 4 / 3
	builder, err := New(Options{
		ContextWindow: inputLimit + 100, ReservedOutput: 100, EstimateText: estimate,
		WarningThreshold: 0.70, CompactThreshold: 0.90,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	wantRatio := float64(plan.EstimatedTokens) / float64(inputLimit)
	if plan.Budget.ContextWindow != inputLimit+100 || plan.Budget.InputLimit != inputLimit || plan.Budget.ReservedOutput != 100 {
		t.Fatalf("budget limits = %#v", plan.Budget)
	}
	if plan.Budget.UtilizationRatio != wantRatio {
		t.Fatalf("utilization = %f, want %f", plan.Budget.UtilizationRatio, wantRatio)
	}
	if !plan.Budget.WarningExceeded || plan.Budget.CompactExceeded {
		t.Fatalf("threshold state = %#v", plan.Budget)
	}
}

func TestBudgetRejectsInvalidGovernanceThresholds(t *testing.T) {
	for _, options := range []Options{
		{WarningThreshold: -0.1, CompactThreshold: 0.9},
		{WarningThreshold: 0.9, CompactThreshold: 0.8},
		{WarningThreshold: 0.8, CompactThreshold: 1.1},
	} {
		if _, err := New(options); err == nil {
			t.Fatalf("accepted invalid thresholds: %#v", options)
		}
	}
}
