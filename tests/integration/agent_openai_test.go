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

	"claude-code-go/internal/cli"
)

func TestOpenAICompatibleCLIReturnsDeniedToolResultToModel(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			lowerBody := bytes.ToLower(body)
			if !bytes.Contains(lowerBody, []byte("cyber-code")) || !bytes.Contains(lowerBody, []byte("independent coding agent")) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(response, `{"error":{"message":"cyber-code system identity missing"}}`)
				return
			}
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"shell-1\",\"function\":{\"name\":\"shell\",\"arguments\":\"{\\\"command\\\":\\\"echo unsafe\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		if !bytes.Contains(bytes.ToLower(body), []byte("permission")) || !bytes.Contains(body, []byte("shell-1")) {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"denied ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: deepseek\npermission_mode: default\nprofiles:\n  deepseek:\n    provider: openai-compatible\n    base_url: %s\n    model: deepseek-v4-pro\n    api_key_env: INTEGRATION_OPENAI_API_KEY\n", server.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_OPENAI_API_KEY", "integration-secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "run shell"}, cli.ExecuteOptions{ConfigFile: configFile, StateDir: t.TempDir()})
	if code != 0 || stdout.String() != "denied ok\n" || stderr.Len() != 0 || calls.Load() != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q calls=%d", code, stdout.String(), stderr.String(), calls.Load())
	}
}
