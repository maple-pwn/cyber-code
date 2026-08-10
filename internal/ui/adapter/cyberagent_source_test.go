package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyber-code/internal/cyberagent"
	"cyber-code/internal/runtimeapi"
)

func TestCyberAgentSourceCreatesStreamsAndReattachesSession(t *testing.T) {
	t.Parallel()

	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(response, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/capabilities":
			writeJSON(response, `{"product":"cyber-agent","runtime_version":"0.1.0","protocol_version":1,"capabilities":["session.events.v1"]}`)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions":
			creates.Add(1)
			writeJSON(response, securitySessionSnapshot("security-task-fixed", "session-fixed", 0, ""))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sessions/session-fixed":
			writeJSON(response, securitySessionSnapshot("security-task-fixed", "session-fixed", 1, "source-event-1"))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sessions/session-fixed/events":
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(response, "data: "+securitySessionEvent()+"\n\n")
		default:
			http.Error(response, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	stateDir := t.TempDir()
	newSource := func() Source {
		source, _, err := NewCyberAgentSource(CyberAgentSourceOptions{
			StateDir: stateDir, Workspace: filepath.Join(stateDir, "workspace"), Location: "remote", Endpoint: server.URL,
			TokenProvider: func(context.Context) (string, error) { return "test-token", nil }, HTTPClient: server.Client(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first := newSource()
	if _, err := first.Handshake(ctx, runtimeapi.HandshakeRequest{SupportedProtocolVersions: []int{1}, SupportedCapabilities: []string{"events", "snapshot", "commands"}}); err != nil {
		t.Fatal(err)
	}
	received := make(chan json.RawMessage, 1)
	stop, err := first.Subscribe(ctx, 0, func(event json.RawMessage) { received <- event })
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Send(ctx, Command{Type: CommandTaskCreate, Objective: "Assess the authorized lab"}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-received:
		if !strings.Contains(string(event), `"type":"task.created"`) {
			t.Fatalf("projected event = %s", event)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for projected cyber-agent event")
	}
	identity := first.(IdentitySource).RuntimeIdentity()
	if identity.SessionID != "session-fixed" || identity.Version != "0.1.0" || identity.Authority != "cyber-agent" {
		t.Fatalf("runtime identity = %#v", identity)
	}
	stop()
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}

	second := newSource()
	defer second.Close(context.Background())
	if identity = second.(IdentitySource).RuntimeIdentity(); identity.SessionID != "session-fixed" {
		t.Fatalf("reattached identity = %#v", identity)
	}
	if creates.Load() != 1 {
		t.Fatalf("session create requests = %d, want 1", creates.Load())
	}
}

func TestCyberAgentSourceUploadsWorkspaceInputsBeforeCreatingSession(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "api.yaml"), []byte("openapi: 3.0.0"), 0o600); err != nil {
		t.Fatal(err)
	}
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/capabilities":
			writeJSON(response, `{"product":"cyber-agent","runtime_version":"0.1.0","protocol_version":1,"capabilities":["session.events.v1"]}`)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/inputs":
			uploaded = true
			if request.URL.Query().Get("filename") != "api.yaml" || request.Header.Get("Content-Type") != "application/yaml" {
				t.Fatalf("input request = %s %q", request.URL.RawQuery, request.Header.Get("Content-Type"))
			}
			writeJSON(response, `{"schema_version":1,"upload_id":"input_0123456789abcdef0123456789abcdef","filename":"api.yaml","media_type":"application/yaml","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":14,"source_location":"inputs/aa/api.yaml","parser_status":"parsed","parent_upload_id":null,"parser_error":null}`)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions":
			if !uploaded {
				t.Fatal("session created before input upload")
			}
			var submission cyberagent.TaskSubmission
			if err := json.NewDecoder(request.Body).Decode(&submission); err != nil {
				t.Fatal(err)
			}
			if submission.Kind != "input_manifest" || len(submission.InputIDs) != 1 || submission.Content != "Assess the attached API" {
				t.Fatalf("submission = %#v", submission)
			}
			writeJSON(response, securitySessionSnapshot("security-task-input", "session-input", 0, ""))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sessions/session-input":
			writeJSON(response, securitySessionSnapshot("security-task-input", "session-input", 0, ""))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sessions/session-input/events":
			response.Header().Set("Content-Type", "text/event-stream")
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	source, _, err := NewCyberAgentSource(CyberAgentSourceOptions{StateDir: t.TempDir(), Workspace: workspace, Location: "remote", Endpoint: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close(context.Background())
	if err := source.Send(context.Background(), Command{Type: CommandTaskCreate, Objective: "Assess the attached API", InputPaths: []string{"api.yaml"}}); err != nil {
		t.Fatal(err)
	}
}

func TestCyberAgentSourceRejectsInputOutsideWorkspace(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := &cyberAgentSource{options: CyberAgentSourceOptions{Workspace: workspace}}
	if _, _, _, err := source.loadInput(outside); err == nil || !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("loadInput error = %v", err)
	}
}

func writeJSON(response http.ResponseWriter, body string) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprint(response, body)
}

func securitySessionSnapshot(taskID, sessionID string, sequence int, eventID string) string {
	return fmt.Sprintf(`{"schema_version":1,"session_id":%q,"task_id":%q,"revision":1,"status":"active","harness_ref":"harness://network-assessment","harness_version":"1.0.0","skill_refs":[],"skill_versions":[],"skill_digests":[],"capability_lease":null,"working_plan":[],"child_run_refs":[],"graph_ref":null,"artifact_refs":[],"event_cursor":{"session_id":%q,"sequence":%d,"event_id":%q},"unified_state_ref":null,"unified_state_version":null,"pending_interaction":null,"pending_action":null,"pending_action_state":null,"in_flight_action_ref":null,"pending_subagent":null,"boundary_approvals":[],"resume_turn":null,"finish_confirmation_pending":false,"interaction_outcomes":[],"memory_summary":null,"conversation_history":[],"raw_tool_outputs":[],"last_response":null,"actions_used":0,"llm_calls_used":0,"tool_calls_used":0,"updated_at":"2026-08-09T12:00:00Z"}`, sessionID, taskID, sessionID, sequence, eventID)
}

func securitySessionEvent() string {
	return `{"event_id":"source-event-1","task_id":"security-task-fixed","session_id":"session-fixed","sequence":1,"topic":"session.created","payload":{"schema_version":1,"session_id":"session-fixed","task_id":"security-task-fixed","revision":1,"status":"active"},"emitted_by":"system","emitted_at":"2026-08-09T12:00:00Z","causation_id":null}`
}
