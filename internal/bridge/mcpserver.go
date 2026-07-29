package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"cyber-code/internal/core"
	"cyber-code/internal/protocol"
)

// MCPServer exposes the bounded session surface through MCP stdio.
type MCPServer struct {
	runtime protocol.Runtime
}

func NewMCPServer(runtime protocol.Runtime) (*MCPServer, error) {
	if runtime == nil {
		return nil, errors.New("MCP server runtime is required")
	}
	return &MCPServer{runtime: runtime}, nil
}

func (server *MCPServer) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := bufio.NewScanner(input)
	reader.Buffer(make([]byte, 4096), protocol.MaxMessageBytes)
	for reader.Scan() {
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(reader.Bytes(), &request); err != nil {
			return errors.New("invalid MCP request")
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "cyber-code", "version": "1"}}
		case "tools/list":
			response["result"] = map[string]any{"tools": []any{
				map[string]any{"name": "cyber_code_start", "description": "Run a cyber-code prompt", "inputSchema": map[string]any{"type": "object", "required": []string{"prompt"}, "properties": map[string]any{"prompt": map[string]string{"type": "string"}}}},
				map[string]any{"name": "cyber_code_status", "description": "Read cyber-code session status", "inputSchema": map[string]any{"type": "object"}},
			}}
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				response["error"] = map[string]any{"code": -32602, "message": "invalid arguments"}
				break
			}
			result, err := server.callTool(ctx, params.Name, params.Arguments)
			if err != nil {
				response["error"] = map[string]any{"code": -32000, "message": err.Error()}
				break
			}
			response["result"] = result
		default:
			response["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		data, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "%s\n", data); err != nil {
			return err
		}
	}
	return reader.Err()
}

func (server *MCPServer) callTool(ctx context.Context, name string, arguments map[string]any) (any, error) {
	switch name {
	case "cyber_code_status":
		return map[string]any{"session_id": server.runtime.SessionID(), "history_messages": len(server.runtime.History())}, nil
	case "cyber_code_start":
		prompt, _ := arguments["prompt"].(string)
		if strings.TrimSpace(prompt) == "" {
			return nil, errors.New("prompt is required")
		}
		var text strings.Builder
		for event := range server.runtime.Run(ctx, prompt) {
			if event.Type == core.EventTextDelta || event.Type == core.EventAssistantMessage {
				text.WriteString(event.Text)
			}
			if event.Type == core.EventError && event.Err != nil {
				return nil, event.Err
			}
		}
		return map[string]any{"content": []map[string]string{{"type": "text", "text": text.String()}}}, nil
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
