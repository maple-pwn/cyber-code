package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyber-code/internal/contextbuilder"
	"cyber-code/internal/permissions"
	"cyber-code/internal/platform"
)

func TestBuildContextBuilderLoadsUserAndProjectHierarchy(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	project := filepath.Join(root, "project")
	current := filepath.Join(project, "service")
	writeContextInstruction(t, filepath.Join(state, "instructions.md"), "user instruction")
	writeContextInstruction(t, filepath.Join(project, ".git", "keep"), "git marker")
	writeContextInstruction(t, filepath.Join(project, "CYBER.md"), "root instruction")
	writeContextInstruction(t, filepath.Join(current, ".cyber-code", "instructions.md"), "nested instruction")

	builder, err := buildContextBuilder(current, state)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), contextbuilder.BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	joined := systemText(plan)
	for _, expected := range []string{"user instruction", "root instruction", "nested instruction"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("context missing %q: %s", expected, joined)
		}
	}
}

func TestBuildContextBuilderDoesNotInjectSkillInstructions(t *testing.T) {
	root := t.TempDir()
	writeContextInstruction(t, filepath.Join(root, ".cyber-code", "skills", "review", "SKILL.md"), "SECRET SKILL BODY")

	builder, err := buildContextBuilder(root, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), contextbuilder.BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(systemText(plan), "SECRET SKILL BODY") {
		t.Fatalf("skill instructions were injected eagerly: %+v", plan.System)
	}
}

func TestBuildContextBuilderUsesConfiguredGovernanceThresholds(t *testing.T) {
	root := t.TempDir()
	builder, err := buildContextBuilderWithThresholds(root, filepath.Join(root, "state"), 0.65, 0.85)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), contextbuilder.BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Budget.WarningThreshold != 0.65 || plan.Budget.CompactThreshold != 0.85 {
		t.Fatalf("governance thresholds = %#v", plan.Budget)
	}
}

func TestBuildContextBuilderIncludesPermissionAndSandboxRuntime(t *testing.T) {
	root := t.TempDir()
	runtimeSource := runtimeEnvironmentSource(permissions.PermissionModeDefault, platform.SandboxCapability{
		Mode: platform.SandboxBestEffort, Backend: "process-group", ProcessTree: true,
	})
	builder, err := buildContextBuilderWithThresholds(root, filepath.Join(root, "state"), 0.65, 0.85, runtimeSource)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(context.Background(), contextbuilder.BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	joined := systemText(plan)
	for _, want := range []string{"permission mode: default", "sandbox mode: best-effort", "backend: process-group", "Do not repeatedly retry"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("runtime context missing %q: %s", want, joined)
		}
	}
}

func systemText(plan contextbuilder.Plan) string {
	parts := make([]string, len(plan.System))
	for index := range plan.System {
		parts[index] = plan.System[index].Text
	}
	return strings.Join(parts, "\n")
}

func writeContextInstruction(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
