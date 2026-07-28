package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cyber-code/internal/cli"
	"cyber-code/internal/core"
	"cyber-code/internal/frontend"
	"cyber-code/internal/platform"
	"cyber-code/internal/ui"
)

func TestCLIJSONDoctorTUIAndMediaDegradationSmoke(t *testing.T) {
	runner := &smokeRunner{events: []core.Event{{Type: core.EventTextDelta, Text: "hello"}, {Type: core.EventCompleted}}}
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--json", "prompt"}, cli.ExecuteOptions{Runner: runner, StateDir: t.TempDir(), ConfigFile: filepath.Join(t.TempDir(), "missing.yaml")})
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"type":"text_delta"`) {
		t.Fatalf("print JSON: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	code = cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"doctor", "--json"}, cli.ExecuteOptions{StateDir: t.TempDir(), ConfigFile: filepath.Join(t.TempDir(), "missing.yaml")})
	var report map[string]any
	if code != 0 || json.Unmarshal(stdout.Bytes(), &report) != nil || report["schema_version"] != float64(1) {
		t.Fatalf("doctor: code=%d output=%q", code, stdout.String())
	}

	model := ui.NewModel(runner, ui.ModelOptions{Width: 80, Height: 24})
	model.Input.SetValue("tui prompt")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*ui.Model)
	for command != nil {
		updated, command = model.Update(command())
		model = updated.(*ui.Model)
	}
	if !strings.Contains(model.View(), "hello") || runner.prompt != "tui prompt" {
		t.Fatalf("TUI view=%q prompt=%q", model.View(), runner.prompt)
	}

	missing := func(string) (string, error) { return "", exec.ErrNotFound }
	notifier := platform.NewNotifier(platform.NotifyOptions{GOOS: "linux", LookPath: missing})
	voice := platform.NewVoiceRecorder(platform.VoiceOptions{GOOS: "windows", LookPath: missing})
	if notifier.Capability().Available || voice.Capability().Available {
		t.Fatalf("unexpected media capabilities: notify=%#v voice=%#v", notifier.Capability(), voice.Capability())
	}
	if err := notifier.Notify(context.Background(), "title", "message"); !errors.Is(err, platform.ErrUnavailable) {
		t.Fatalf("notification error=%v", err)
	}
}

type smokeRunner struct {
	prompt string
	events []core.Event
}

func (runner *smokeRunner) Run(_ context.Context, prompt string) <-chan core.Event {
	runner.prompt = prompt
	events := make(chan core.Event, len(runner.events))
	for _, event := range runner.events {
		events <- event
	}
	close(events)
	return events
}

var _ frontend.Runner = (*smokeRunner)(nil)
