package integration

import (
	"bytes"
	"context"
	"testing"

	"cyber-code/internal/bridge"
	"cyber-code/internal/core"
	"cyber-code/internal/protocol"
)

type protocolRuntime struct{}

func (protocolRuntime) Run(_ context.Context, prompt string) <-chan core.Event {
	output := make(chan core.Event, 1)
	output <- core.Event{Type: core.EventTextDelta, Text: prompt}
	close(output)
	return output
}
func (protocolRuntime) SessionID() string       { return "integration" }
func (protocolRuntime) History() []core.Message { return nil }

func TestLocalProtocolAndMCPBridgeContracts(t *testing.T) {
	runtime := protocolRuntime{}
	server, err := protocol.NewServer(runtime, 0)
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBufferString(`{"version":1,"id":"1","type":"start","prompt":"ping"}` + "\n")
	var output bytes.Buffer
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"type":"event"`)) {
		t.Fatalf("protocol output = %s", output.String())
	}
	mcpServer, err := bridge.NewMCPServer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	mcpInput := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")
	output.Reset()
	if err := mcpServer.Serve(context.Background(), mcpInput, &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("cyber_code_start")) {
		t.Fatalf("MCP output = %s", output.String())
	}
}
