package cyberagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientNegotiatesCapabilitiesAndSessionMutations(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 8)
	snapshot := testSnapshotJSON("active", 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer runtime-token" {
			http.Error(response, `{"detail":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		mu.Lock()
		requests = append(requests, request.Method+" "+request.URL.RequestURI()+" key="+request.Header.Get("Idempotency-Key"))
		mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/capabilities":
			_, _ = response.Write([]byte(`{"product":"cyber-agent","runtime_version":"0.1.0","protocol_version":1,"capabilities":["session.events.v1","session.idempotency.v1"]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions":
			if request.URL.Query().Get("agent_mode") != "multi" || request.URL.Query().Get("scope") != "127.0.0.1" {
				http.Error(response, `{"detail":"bad options"}`, http.StatusBadRequest)
				return
			}
			var submission TaskSubmission
			if err := json.NewDecoder(request.Body).Decode(&submission); err != nil || submission.TaskID != "task-client" {
				http.Error(response, `{"detail":"bad submission"}`, http.StatusBadRequest)
				return
			}
			_, _ = response.Write(snapshot)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sessions/session-client":
			_, _ = response.Write(snapshot)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions/session-client/turns":
			_, _ = response.Write(snapshot)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions/session-client/interactions":
			_, _ = response.Write(snapshot)
		case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/v1/sessions/session-client/"):
			_, _ = response.Write(snapshot)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{
		BaseURL: server.URL,
		TokenProvider: func(context.Context) (string, error) {
			return "runtime-token", nil
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	capabilities, err := client.Capabilities(ctx)
	if err != nil || capabilities.ProtocolVersion != 1 {
		t.Fatalf("Capabilities = %#v, %v", capabilities, err)
	}
	created, err := client.CreateSession(ctx, TaskSubmission{
		Kind: "natural_language", TaskID: "task-client", Content: "Inspect 127.0.0.1",
	}, CreateSessionOptions{AgentMode: "multi", Scope: []string{"127.0.0.1"}}, "create-1")
	if err != nil || created.SessionID != "session-client" {
		t.Fatalf("CreateSession = %#v, %v", created, err)
	}
	if _, err = client.Snapshot(ctx, "session-client"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err = client.SubmitTurn(ctx, "session-client", "Continue", "turn-1"); err != nil {
		t.Fatalf("SubmitTurn: %v", err)
	}
	respondedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if _, err = client.RespondInteraction(ctx, "session-client", InteractionResponse{
		InteractionID: "scope-confirm-1", SessionID: "session-client", Approved: boolPointer(true), RespondedAt: respondedAt,
	}, "interaction-1"); err != nil {
		t.Fatalf("RespondInteraction: %v", err)
	}
	for name, operation := range map[string]func(context.Context, string, string) (SessionSnapshot, error){
		"resume":  client.ResumeSession,
		"cancel":  client.CancelSession,
		"compact": client.CompactSession,
	} {
		if _, err = operation(ctx, "session-client", name+"-1"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(requests, "\n")
	for _, expected := range []string{
		"POST /v1/sessions?agent_mode=multi&scope=127.0.0.1 key=create-1",
		"POST /v1/sessions/session-client/turns key=turn-1",
		"POST /v1/sessions/session-client/interactions key=interaction-1",
		"POST /v1/sessions/session-client/resume key=resume-1",
		"POST /v1/sessions/session-client/cancel key=cancel-1",
		"POST /v1/sessions/session-client/compact key=compact-1",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("requests missing %q:\n%s", expected, joined)
		}
	}
}

func TestClientBoundsAndStrictlyDecodesResponses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		body  string
		limit int64
		want  string
	}{
		{name: "too large", body: strings.Repeat("x", 128), limit: 32, want: "exceeds"},
		{name: "unknown field", body: `{"product":"cyber-agent","runtime_version":"0.1.0","protocol_version":1,"capabilities":["session.events.v1"],"unexpected":true}`, limit: 1024, want: "unknown field"},
		{name: "trailing json", body: `{"product":"cyber-agent","runtime_version":"0.1.0","protocol_version":1,"capabilities":["session.events.v1"]}{}`, limit: 1024, want: "trailing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := NewClient(ClientOptions{BaseURL: server.URL, ResponseLimit: test.limit})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = client.Capabilities(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Capabilities error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestClientManagesSkillLifecycle(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/skills":
			_, _ = response.Write([]byte(`{"skills":[{"skill_ref":"skill://community/recon-notes","version":"1.0.0","content_digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","archive_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","trust":"user-approved","required_tools":["mcp://recon/nmap_scan"],"missing_tools":["mcp://recon/nmap_scan"],"active":false}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/v1/skills":
			if request.Header.Get("Idempotency-Key") != "skill-install-1" {
				t.Fatalf("missing idempotency key")
			}
			_, _ = response.Write([]byte(`{"skill_ref":"skill://community/recon-notes","version":"1.0.0","content_digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","archive_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","trust":"user-approved"}`))
		case request.Method == http.MethodDelete:
			_, _ = response.Write([]byte(`{"skill_ref":"skill://community/recon-notes","removed":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := client.ListSkills(context.Background(), "")
	if err != nil || len(statuses) != 1 || statuses[0].Active || len(statuses[0].MissingTools) != 1 {
		t.Fatalf("ListSkills = %#v, %v", statuses, err)
	}
	installed, err := client.InstallSkill(context.Background(), "skill://community/recon-notes", "1.0.0", "skill-install-1")
	if err != nil || installed.Version != "1.0.0" {
		t.Fatalf("InstallSkill = %#v, %v", installed, err)
	}
	removed, err := client.RemoveSkill(context.Background(), "skill://community/recon-notes")
	if err != nil || !removed.Removed {
		t.Fatalf("RemoveSkill = %#v, %v", removed, err)
	}
}

func TestClientPropagatesContextCancellationAndBoundedAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/cancel" {
			<-request.Context().Done()
			return
		}
		response.WriteHeader(http.StatusConflict)
		if strings.HasSuffix(request.URL.Path, "/oversized") {
			_, _ = fmt.Fprint(response, `{"detail":"idempotency conflict"}`+strings.Repeat("x", 128))
			return
		}
		_, _ = fmt.Fprint(response, `{"detail":"idempotency conflict"}`)
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL, ResponseLimit: 64})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = client.Snapshot(ctx, "cancel"); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Snapshot cancellation error = %v", err)
	}
	if _, err = client.Snapshot(context.Background(), "session-client"); err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("Snapshot API error = %v", err)
	}
	if _, err = client.Snapshot(context.Background(), "oversized"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Snapshot oversized error = %v", err)
	}
}

func testSnapshotJSON(status string, revision int) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":1,"session_id":"session-client","task_id":"task-client","revision":%d,"status":%q,"harness_ref":"harness://network-assessment","harness_version":"1.0.0","skill_refs":[],"skill_versions":[],"skill_digests":[],"capability_lease":null,"working_plan":[],"child_run_refs":[],"graph_ref":null,"artifact_refs":[],"event_cursor":{"session_id":"session-client","sequence":1,"event_id":"event-1"},"unified_state_ref":null,"unified_state_version":null,"pending_interaction":null,"pending_action":null,"pending_action_state":null,"in_flight_action_ref":null,"pending_subagent":null,"boundary_approvals":[],"resume_turn":null,"finish_confirmation_pending":false,"interaction_outcomes":[],"memory_summary":null,"conversation_history":[],"raw_tool_outputs":[],"last_response":null,"actions_used":0,"llm_calls_used":0,"tool_calls_used":0,"updated_at":"2026-08-09T12:00:00Z"}`, revision, status))
}

func boolPointer(value bool) *bool { return &value }
