package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"cyber-code/internal/cli"
)

func TestAnthropicCLICompletesReadToolRound(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(target, []byte("anthropic tool content"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var secondRequest atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			_, _ = fmt.Fprintf(response, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"read-1\",\"name\":\"read_file\",\"input\":{\"path\":%q}}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", target)
			return
		}
		secondRequest.Store(string(body))
		if !bytes.Contains(body, []byte("anthropic tool content")) || !bytes.Contains(body, []byte("read-1")) {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(response, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"anthropic ok\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: anthropic\npermission_mode: default\nprofiles:\n  anthropic:\n    provider: anthropic\n    base_url: %s\n    model: claude-test\n    api_key_env: INTEGRATION_ANTHROPIC_API_KEY\n", server.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_ANTHROPIC_API_KEY", "integration-secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--cwd", workspace, "read note"}, cli.ExecuteOptions{ConfigFile: configFile, StateDir: t.TempDir()})
	if code != 0 || stdout.String() != "anthropic ok\n" || stderr.Len() != 0 || calls.Load() != 2 {
		requestBody, _ := secondRequest.Load().(string)
		t.Fatalf("code=%d stdout=%q stderr=%q calls=%d second_request=%s", code, stdout.String(), stderr.String(), calls.Load(), requestBody)
	}
}
