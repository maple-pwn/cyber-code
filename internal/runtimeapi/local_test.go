package runtimeapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLocalServerRequiresBearerAndNegotiatesLocalHandshake(t *testing.T) {
	t.Parallel()
	server := newTestLocalServer(t)
	unauthorized := server.Handle(context.Background(), LocalRequest{ID: "1", Type: "health", Bearer: "wrong"})
	if unauthorized.Type != "error" || unauthorized.ErrorCode != "unauthorized" {
		t.Fatalf("unauthorized response = %+v", unauthorized)
	}
	response := server.Handle(context.Background(), LocalRequest{
		ID: "2", Type: "handshake", Bearer: "launch-secret",
		Handshake: &HandshakeRequest{SupportedProtocolVersions: []int{ProtocolVersion}, AfterCursor: 0},
	})
	if response.Type != "handshake" || response.Handshake == nil || response.Handshake.Source.Mode != SourceModeLocal || response.Handshake.RuntimeID != "runtime-1" {
		t.Fatalf("handshake response = %+v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("launch-secret")) {
		t.Fatalf("response leaked bearer: %s", encoded)
	}
}

func TestLocalServerRejectsSourceIdentityDifferentFromAuthority(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-authority", "operator", nil)
	_, err = NewLocalServer(LocalServerOptions{
		Service: service, Bearer: "launch-secret", Role: "owner", Workspace: "/workspace",
		Source: SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-forged", Principal: "operator", Capabilities: []string{"events"}},
	})
	if err == nil {
		t.Fatal("local server accepted source identity different from authority")
	}
}

func TestLocalServerRejectsControlCharactersInIdentity(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-1", "operator", time.Now)
	for name, options := range map[string]LocalServerOptions{
		"runtime ID": {
			Service: service, Bearer: "launch-secret", Role: "owner",
			Source: SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-1\nforged", Principal: "operator", Capabilities: []string{"events"}},
		},
		"principal": {
			Service: service, Bearer: "launch-secret", Role: "owner",
			Source: SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-1", Principal: "operator\rforged", Capabilities: []string{"events"}},
		},
		"role": {
			Service: service, Bearer: "launch-secret", Role: "owner\tforged",
			Source: SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-1", Principal: "operator", Capabilities: []string{"events"}},
		},
		"capability": {
			Service: service, Bearer: "launch-secret", Role: "owner",
			Source: SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-1", Principal: "operator", Capabilities: []string{"events\x00forged"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewLocalServer(options); err == nil {
				t.Fatal("NewLocalServer accepted a control character in runtime identity")
			}
		})
	}
}

func TestLocalServerPersistsCommandsAndReplaysEventsAfterCursor(t *testing.T) {
	t.Parallel()
	server := newTestLocalServer(t)
	envelope := mustLocalEnvelope(t, "idem-1", map[string]any{
		"type": "task.create", "objective": "Audit workspace", "runtimeId": "runtime-1", "workspace": "/workspace",
	})
	first := server.Handle(context.Background(), LocalRequest{ID: "1", Type: "command", Bearer: "launch-secret", Command: &envelope})
	if first.Receipt == nil || first.Receipt.Status != "accepted" {
		t.Fatalf("command receipt = %+v", first)
	}
	duplicate := server.Handle(context.Background(), LocalRequest{ID: "2", Type: "command", Bearer: "launch-secret", Command: &envelope})
	if duplicate.Receipt == nil || *duplicate.Receipt != *first.Receipt {
		t.Fatalf("duplicate receipt = %+v, want %+v", duplicate.Receipt, first.Receipt)
	}
	events := server.Handle(context.Background(), LocalRequest{ID: "3", Type: "events", Bearer: "launch-secret", TaskID: "task-1", AfterCursor: 0})
	if events.Type != "events" || len(events.Events) != 2 || events.Events[0].Type != "task.created" || events.Events[1].Type != "scope.proposed" {
		t.Fatalf("replayed events = %+v", events)
	}
	if after := server.Handle(context.Background(), LocalRequest{ID: "4", Type: "events", Bearer: "launch-secret", TaskID: "task-1", AfterCursor: 1}); len(after.Events) != 1 || after.Events[0].Cursor != 2 {
		t.Fatalf("events after cursor = %+v", after)
	}
	implicit := server.Handle(context.Background(), LocalRequest{ID: "4b", Type: "events", Bearer: "launch-secret", AfterCursor: 1})
	if len(implicit.Events) != 1 || implicit.Events[0].TaskID != "task-1" {
		t.Fatalf("implicit active task events = %+v", implicit)
	}
	snapshot := server.Handle(context.Background(), LocalRequest{ID: "5", Type: "snapshot", Bearer: "launch-secret", TaskID: "task-1"})
	if snapshot.Snapshot == nil || snapshot.Snapshot.Cursor != 2 || snapshot.Snapshot.State.CommittedCursor != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestLocalServerAllowsSubscriptionAndSnapshotBeforeFirstTask(t *testing.T) {
	t.Parallel()

	server := newTestLocalServer(t)
	events := server.Handle(context.Background(), LocalRequest{
		ID: "before-events", Type: "events", Bearer: "launch-secret", AfterCursor: 0,
	})
	if events.Type != "events" || len(events.Events) != 0 {
		t.Fatalf("events before first task = %+v", events)
	}
	encodedEvents, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encodedEvents, []byte(`"events":[]`)) {
		t.Fatalf("empty events response omitted protocol field: %s", encodedEvents)
	}
	snapshot := server.Handle(context.Background(), LocalRequest{
		ID: "before-snapshot", Type: "snapshot", Bearer: "launch-secret",
	})
	if snapshot.Type != "snapshot" || snapshot.Snapshot == nil || snapshot.Snapshot.Cursor != 0 ||
		snapshot.Snapshot.State.CommittedCursor != 0 {
		t.Fatalf("snapshot before first task = %+v", snapshot)
	}
}

func TestLocalServerRejectsIdempotencySubstitutionAndRuntimeMismatch(t *testing.T) {
	t.Parallel()
	server := newTestLocalServer(t)
	first := mustLocalEnvelope(t, "idem-1", map[string]any{"type": "task.create", "objective": "Audit", "runtimeId": "runtime-1"})
	if response := server.Handle(context.Background(), LocalRequest{ID: "1", Type: "command", Bearer: "launch-secret", Command: &first}); response.Receipt == nil || response.Receipt.Status != "accepted" {
		t.Fatalf("first command = %+v", response)
	}
	substitution := mustLocalEnvelope(t, "idem-1", map[string]any{"type": "task.cancel"})
	response := server.Handle(context.Background(), LocalRequest{ID: "2", Type: "command", Bearer: "launch-secret", Command: &substitution})
	if response.Receipt == nil || response.Receipt.Status != "rejected" || response.Receipt.ErrorCode != "idempotency_conflict" {
		t.Fatalf("substituted command = %+v", response)
	}
	mismatch := mustLocalEnvelope(t, "idem-2", map[string]any{"type": "task.create", "objective": "Audit", "runtimeId": "other-runtime"})
	response = server.Handle(context.Background(), LocalRequest{ID: "3", Type: "command", Bearer: "launch-secret", Command: &mismatch})
	if response.Receipt == nil || response.Receipt.Status != "rejected" || response.Receipt.ErrorCode != "runtime_mismatch" {
		t.Fatalf("runtime mismatch = %+v", response)
	}
}

func TestLocalServerBindsIdempotencyToControllerAndAuthority(t *testing.T) {
	t.Parallel()
	server := newTestLocalServer(t)
	envelope := mustLocalEnvelope(t, "shared-key", map[string]any{
		"type": "task.create", "objective": "Controller A task", "runtimeId": "runtime-1",
	})

	first := server.handleAuthorizedForClient(context.Background(), LocalRequest{
		ID: "controller-a", Type: "command", Command: &envelope,
	}, "controller-a")
	if first.Receipt == nil || first.Receipt.Status != "accepted" {
		t.Fatalf("first controller receipt = %+v", first)
	}

	replay := server.handleAuthorizedForClient(context.Background(), LocalRequest{
		ID: "controller-b", Type: "command", Command: &envelope,
	}, "controller-b")
	if replay.Receipt == nil || replay.Receipt.Status != "rejected" || replay.Receipt.ErrorCode != "idempotency_conflict" {
		t.Fatalf("cross-controller replay = %+v", replay)
	}

	server.role = "auditor"
	authorityReplay := server.handleAuthorizedForClient(context.Background(), LocalRequest{
		ID: "changed-authority", Type: "command", Command: &envelope,
	}, "controller-a")
	if authorityReplay.Receipt == nil || authorityReplay.Receipt.Status != "rejected" || authorityReplay.Receipt.ErrorCode != "idempotency_conflict" {
		t.Fatalf("cross-authority replay = %+v", authorityReplay)
	}
}

func TestLocalServerPreservesIdempotencyAcrossRestart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := newTestLocalServerAt(t, root)
	envelope := mustLocalEnvelope(t, "idem-restart", map[string]any{
		"type": "task.create", "objective": "Audit", "runtimeId": "runtime-1",
	})
	before := first.Handle(context.Background(), LocalRequest{ID: "1", Type: "command", Bearer: "launch-secret", Command: &envelope})
	if before.Receipt == nil || before.Receipt.Status != "accepted" {
		t.Fatalf("first receipt = %+v", before)
	}

	restarted := newTestLocalServerAt(t, root)
	after := restarted.Handle(context.Background(), LocalRequest{ID: "2", Type: "command", Bearer: "launch-secret", Command: &envelope})
	if after.Receipt == nil || *after.Receipt != *before.Receipt {
		t.Fatalf("restart receipt = %+v, want %+v", after.Receipt, before.Receipt)
	}
	events := restarted.Handle(context.Background(), LocalRequest{ID: "3", Type: "events", Bearer: "launch-secret", TaskID: "task-1"})
	if len(events.Events) != 2 {
		t.Fatalf("restart duplicated events: %+v", events.Events)
	}
}

func TestLocalServerServeUsesStrictBoundedNDJSONWithoutListener(t *testing.T) {
	t.Parallel()
	server := newTestLocalServer(t)
	input := strings.NewReader(strings.Join([]string{
		`{"id":"1","type":"health","bearer":"launch-secret"}`,
		`{"id":"2","type":"health","bearer":"launch-secret","unknown":true}`,
	}, "\n") + "\n")
	var output bytes.Buffer
	if err := server.Serve(context.Background(), input, &output); err == nil {
		t.Fatal("Serve accepted an unknown request field")
	}
	scanner := bufio.NewScanner(&output)
	if !scanner.Scan() {
		t.Fatal("missing health response")
	}
	var health LocalResponse
	if err := json.Unmarshal(scanner.Bytes(), &health); err != nil || health.Type != "health" || health.Ready == nil || !*health.Ready {
		t.Fatalf("health response = %s, %v", scanner.Bytes(), err)
	}
	if strings.Contains(output.String(), "launch-secret") {
		t.Fatalf("stdio output leaked bearer: %q", output.String())
	}
}

func TestLocalServerDoesNotExposeInternalErrorsAsProtocolCodes(t *testing.T) {
	t.Parallel()
	if code := localErrorCode(errors.New("open /private/secret/path: permission denied")); code != "runtime_command_failed" {
		t.Fatalf("internal error code = %q", code)
	}
}

func newTestLocalServer(t *testing.T) *LocalServer {
	t.Helper()
	return newTestLocalServerAt(t, t.TempDir())
}

func newTestLocalServerAt(t *testing.T, root string) *LocalServer {
	t.Helper()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service := NewService(store, "runtime-1", "operator", func() time.Time { return now })
	server, err := NewLocalServer(LocalServerOptions{
		Service: service, Bearer: "launch-secret", Role: "owner", Workspace: "/workspace",
		Source: SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-1", Principal: "operator", Capabilities: []string{"events", "snapshot", "commands"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func mustLocalEnvelope(t *testing.T, key string, command any) CommandEnvelope {
	t.Helper()
	raw, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	return CommandEnvelope{IdempotencyKey: key, Command: raw}
}
