package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestDefaultProcessFactoryRunsManagedJSONRPCProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	factory := NewDefaultProcessFactory(DefaultProcessOptions{MaxMessageBytes: 1 << 20})
	process, err := factory.Start(context.Background(), Manifest{
		Name: "real", Root: t.TempDir(), Entrypoint: executable,
		Args: []string{"-test.run=^TestPluginProcessHelper$", "--", "plugin-helper"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var result ToolResult
	if err := process.Call(context.Background(), "tools/call", CallToolParams{Name: "echo", Arguments: json.RawMessage(`{}`)}, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "real plugin" {
		t.Fatalf("result = %#v", result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := process.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPluginProcessHelper(t *testing.T) {
	helper := false
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) && os.Args[index+1] == "plugin-helper" {
			helper = true
		}
	}
	if !helper {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int64           `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return
		}
		result, _ := json.Marshal(ToolResult{Content: []PluginContent{{Type: "text", Text: "real plugin"}}})
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": json.RawMessage(result)})
	}
}
