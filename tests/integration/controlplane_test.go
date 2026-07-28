package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"cyber-code/internal/cli"
)

func TestCLIControlPlaneHelpUsesSharedRuntimeWithoutProviderTurn(t *testing.T) {
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		response.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	root := t.TempDir()
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: deepseek-v4-pro\n    api_key_env: CONTROL_PLANE_API_KEY\n", server.URL)
	configFile := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTROL_PLANE_API_KEY", "control-plane-secret")

	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "/help"}, cli.ExecuteOptions{ConfigFile: configFile, StateDir: filepath.Join(root, "state")})
	if code != 0 || stderr.Len() != 0 || providerCalls.Load() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q provider calls=%d", code, stdout.String(), stderr.String(), providerCalls.Load())
	}
	for _, expected := range []string{"/context", "/hooks", "/model", "/permissions", "/skills", "/status", "/tasks"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("help missing %q: %q", expected, stdout.String())
		}
	}
}
