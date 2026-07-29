package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/core"
)

type fakeRuntime struct {
	prompt string
}

func (runtime *fakeRuntime) Run(ctx context.Context, prompt string) <-chan core.Event {
	runtime.prompt = prompt
	output := make(chan core.Event, 2)
	output <- core.Event{Type: core.EventTextDelta, Text: prompt}
	output <- core.Event{Type: core.EventCompleted, FinishReason: "done"}
	close(output)
	return output
}
func (runtime *fakeRuntime) SessionID() string       { return "session-1" }
func (runtime *fakeRuntime) History() []core.Message { return []core.Message{{Role: core.RoleUser}} }

type ideRuntime struct {
	fakeRuntime
	ide *IDEContext
}

func (runtime *ideRuntime) RunWithIDEContext(ctx context.Context, prompt string, ide *IDEContext) <-chan core.Event {
	runtime.ide = ide
	return runtime.Run(ctx, prompt)
}

func TestIDESendsWorkspaceFocusSelectionAndDiagnosticsToRuntime(t *testing.T) {
	runtime := &ideRuntime{}
	server, err := NewServer(runtime, 0)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{
		Version: Version,
		ID:      "ide-turn",
		Type:    "start",
		Prompt:  "fix this",
		IDEContext: &IDEContext{
			Workspace:   "/workspace",
			Focus:       "main.go",
			Selection:   &IDESelection{Path: "main.go", Start: IDEPosition{Line: 3, Character: 2}, End: IDEPosition{Line: 5, Character: 8}, Text: "broken()"},
			Diagnostics: []IDEDiagnostic{{Path: "main.go", Message: "undefined: broken", Severity: "error", Start: IDEPosition{Line: 3, Character: 2}}},
		},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(append(encoded, '\n')), &output); err != nil {
		t.Fatal(err)
	}
	if runtime.ide == nil || runtime.ide.Workspace != "/workspace" || runtime.ide.Focus != "main.go" {
		t.Fatalf("IDE context = %#v", runtime.ide)
	}
	if runtime.ide.Selection == nil || runtime.ide.Selection.Text != "broken()" || len(runtime.ide.Diagnostics) != 1 {
		t.Fatalf("IDE details = %#v", runtime.ide)
	}
}

func TestIDEFallbackAddsBoundedUntrustedContextForLegacyRuntime(t *testing.T) {
	runtime := &fakeRuntime{}
	server, err := NewServer(runtime, 0)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Version: Version, Type: "start", Prompt: "fix this", IDEContext: &IDEContext{
		Workspace: "/workspace", Focus: "main.go",
		Diagnostics: []IDEDiagnostic{{Path: "main.go", Message: "undefined: value", Severity: "error"}},
	}}
	encoded, _ := json.Marshal(request)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(append(encoded, '\n')), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runtime.prompt, "fix this") || !strings.Contains(runtime.prompt, "main.go") || !strings.Contains(runtime.prompt, "untrusted editor context") {
		t.Fatalf("prompt = %q", runtime.prompt)
	}
}

func TestCodecRejectsOversizedAndWrongVersion(t *testing.T) {
	codec := NewCodec(32)
	var request Request
	if err := codec.Decode(bufioReader(strings.NewReader(strings.Repeat("x", 40))), &request); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("oversized error = %v", err)
	}
	data, _ := json.Marshal(Request{Version: Version + 1, Type: "status"})
	if err := codec.Decode(bufioReader(bytes.NewReader(append(data, '\n'))), &request); err == nil || !strings.Contains(err.Error(), "unsupported protocol version") {
		t.Fatalf("version error = %v", err)
	}
}

func TestServerStartsTurnAndReportsStatus(t *testing.T) {
	runtime := &fakeRuntime{}
	server, err := NewServer(runtime, 0)
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBufferString(`{"version":1,"id":"s1","type":"start","prompt":"hello"}` + "\n" + `{"version":1,"id":"q","type":"status"}` + "\n")
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Serve(ctx, input, &output); err != nil {
		t.Fatal(err)
	}
	if runtime.prompt != "hello" {
		t.Fatalf("prompt = %q", runtime.prompt)
	}
	if !strings.Contains(output.String(), `"type":"accepted"`) || !strings.Contains(output.String(), `"text":"hello"`) {
		t.Fatalf("output = %s", output.String())
	}
}

type blockingWriter struct {
	release <-chan struct{}
	started chan<- struct{}
}

func (writer blockingWriter) Write(data []byte) (int, error) {
	writer.started <- struct{}{}
	<-writer.release
	return len(data), nil
}

func TestConnectionOutputRejectsSlowConsumer(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	output := newConnectionOutput(blockingWriter{release: release, started: started}, NewCodec(0), 1)
	defer func() { close(release); output.Close() }()
	if err := output.Send(Response{Type: "first"}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := output.Send(Response{Type: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := output.Send(Response{Type: "third"}); !errors.Is(err, ErrSlowConsumer) {
		t.Fatalf("error=%v", err)
	}
}

func TestConnectionOutputCloseDoesNotWaitForBlockedWriter(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	output := newConnectionOutput(blockingWriter{release: release, started: started}, NewCodec(0), 1)
	if err := output.Send(Response{Type: "first"}); err != nil {
		t.Fatal(err)
	}
	<-started
	done := make(chan error, 1)
	go func() { done <- output.Close() }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		close(release)
		t.Fatal("close blocked on slow writer")
	}
	close(release)
}

type cancelRuntime struct{}

func (cancelRuntime) Run(ctx context.Context, _ string) <-chan core.Event {
	output := make(chan core.Event)
	go func() { <-ctx.Done(); close(output) }()
	return output
}
func (cancelRuntime) SessionID() string       { return "cancel" }
func (cancelRuntime) History() []core.Message { return nil }

func TestServerConfirmsTurnCancellation(t *testing.T) {
	server, err := NewServer(cancelRuntime{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBufferString(`{"version":1,"id":"turn","type":"start","prompt":"wait"}` + "\n" + `{"version":1,"id":"cancel","type":"cancel"}` + "\n")
	var output bytes.Buffer
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"type":"turn_finished"`) || !strings.Contains(output.String(), `"canceled":true`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestServerCanServeNewConnectionAfterDisconnect(t *testing.T) {
	server, err := NewServer(&fakeRuntime{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		var output bytes.Buffer
		input := bytes.NewBufferString(`{"version":1,"type":"status"}` + "\n")
		if err := server.Serve(context.Background(), input, &output); err != nil || !strings.Contains(output.String(), `"type":"status"`) {
			t.Fatalf("connection %d output=%s err=%v", index, output.String(), err)
		}
	}
}

func bufioReader(reader io.Reader) *bufio.Reader { return bufio.NewReader(reader) }
