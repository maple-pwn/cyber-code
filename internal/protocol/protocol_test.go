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

func bufioReader(reader io.Reader) *bufio.Reader { return bufio.NewReader(reader) }
