package cli

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

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/ui"
)

func TestComposeRuntimeBootstrapConfirmationAllowsWrite(t *testing.T) {
	workspace := t.TempDir()
	stateDir := t.TempDir()
	target := filepath.Join(workspace, "approved.txt")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			arguments := fmt.Sprintf(`{"path":%q,"content":"confirmed write"}`, target)
			_, _ = fmt.Fprintf(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"write-1\",\"function\":{\"name\":\"write_file\",\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", arguments)
			return
		}
		if !bytes.Contains(body, []byte("file written")) {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(response, `{"error":{"message":"write result missing"}}`)
			return
		}
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"allowed\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	configText := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: COMPOSITION_PERMISSION_KEY\n", server.URL)
	if err := os.WriteFile(configFile, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSITION_PERMISSION_KEY", "secret")
	var prompt bytes.Buffer
	confirmer := newInteractiveConfirmer(ui.NewPermissionBridge(), newBootstrapConfirmer(strings.NewReader("yes\n"), &prompt))
	built, err := composeRuntime(context.Background(), compositionOptions{
		ConfigFile: configFile, StateDir: stateDir, Cwd: workspace, Confirmer: confirmer,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Shutdown(context.Background())
	for event := range built.Run(context.Background(), "write the file") {
		if event.Type == core.EventError {
			t.Fatalf("runtime error: %v", event.Err)
		}
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "confirmed write" {
		t.Fatalf("written content=%q error=%v", content, err)
	}
	if calls.Load() != 2 || !strings.Contains(prompt.String(), "write") || !strings.Contains(prompt.String(), "approved.txt") {
		t.Fatalf("calls=%d confirmation prompt=%q", calls.Load(), prompt.String())
	}
	var records []permissions.AuditRecord
	if err := readStateFile(filepath.Join(stateDir, "audit.json"), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Behavior != permissions.PermissionBehaviorAllow || records[0].Tool != "write_file" {
		t.Fatalf("audit records=%#v", records)
	}
}
