package runtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cyber-code/internal/productprotocol"
)

type testRuntimeDrainer struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (drainer *testRuntimeDrainer) Drain(ctx context.Context) error {
	drainer.calls++
	close(drainer.started)
	select {
	case <-drainer.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestServiceDrainCoordinatesRegisteredWorkers(t *testing.T) {
	service := newTestService(t, time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC))
	drainer := &testRuntimeDrainer{started: make(chan struct{}), release: make(chan struct{})}
	if err := service.RegisterDrainer(drainer); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- service.Drain(context.Background()) }()
	<-drainer.started
	if err := service.RegisterDrainer(&testRuntimeDrainer{}); !errors.Is(err, ErrRuntimeDraining) {
		t.Fatalf("register while draining error=%v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("drain returned before worker: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(drainer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := service.Drain(context.Background()); err != nil || drainer.calls != 1 {
		t.Fatalf("second drain error=%v calls=%d", err, drainer.calls)
	}
}

func TestServiceDerivesApprovalDigestAndEnforcesConfirmedScope(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, now)
	scope := productprotocol.ScopeSnapshot{
		ID: "scope-1", Principal: "operator", Workspace: "/lab", Validity: "task",
		Targets: []string{"juice-shop.lab"}, AllowedActions: []string{"verify"}, DeniedActions: []string{"delete"}, RiskCeiling: "medium",
	}
	if _, err := service.ProposeScope(context.Background(), "task-1", scope); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestApproval(context.Background(), ApprovalRequest{
		TaskID: "task-1", ChallengeID: "approval-1", AgentID: "agent-1", Action: "verify", Target: "juice-shop.lab",
		Parameters: map[string]any{"username": "admin", "attempt": 1}, Risk: "medium", ExpiresAt: now.Add(time.Minute),
	}); !errors.Is(err, ErrScopeNotConfirmed) {
		t.Fatalf("unconfirmed scope error = %v", err)
	}
	if _, err := service.ConfirmScope(context.Background(), "task-1", "scope-1"); err != nil {
		t.Fatal(err)
	}
	event, err := service.RequestApproval(context.Background(), ApprovalRequest{
		TaskID: "task-1", ChallengeID: "approval-1", AgentID: "agent-1", Action: "verify", Target: "juice-shop.lab",
		Parameters: map[string]any{"attempt": 1, "username": "admin"}, Risk: "medium", ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	challenge := approvalChallenge(t, event)
	want, err := ParameterDigest("verify", "juice-shop.lab", map[string]any{"username": "admin", "attempt": 1})
	if err != nil {
		t.Fatal(err)
	}
	if challenge.ParameterDigest != want || challenge.ParameterDigest == "" {
		t.Fatalf("parameter digest = %q, want %q", challenge.ParameterDigest, want)
	}

	for _, request := range []ApprovalRequest{
		{TaskID: "task-1", ChallengeID: "approval-2", AgentID: "agent-1", Action: "delete", Target: "juice-shop.lab", Parameters: map[string]any{}, Risk: "medium", ExpiresAt: now.Add(time.Minute)},
		{TaskID: "task-1", ChallengeID: "approval-3", AgentID: "agent-1", Action: "verify", Target: "outside.lab", Parameters: map[string]any{}, Risk: "medium", ExpiresAt: now.Add(time.Minute)},
	} {
		if _, err := service.RequestApproval(context.Background(), request); !errors.Is(err, ErrApprovalOutOfScope) {
			t.Fatalf("out-of-scope request error = %v", err)
		}
	}
}

func TestServiceOwnsEventSourceIdentityAndTimestamp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, now)
	event, err := service.Emit(context.Background(), DraftEvent{
		TaskID: "task-1", Type: "task.created", OccurredAt: now.Add(-time.Hour),
		Source: productprotocol.EventSourceRef{RuntimeID: "forged-runtime"}, Payload: map[string]any{"title": "Audit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Source.RuntimeID != "runtime-1" || event.OccurredAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("service accepted forged authority metadata: %+v", event)
	}
}

func TestServiceRejectsExpiredAndReplayedApprovalResponses(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, now)
	confirmTestScope(t, service)
	if _, err := service.RequestApproval(context.Background(), ApprovalRequest{
		TaskID: "task-1", ChallengeID: "approval-1", AgentID: "agent-1", Action: "verify", Target: "lab",
		Parameters: map[string]any{"path": "/login"}, Risk: "low", ExpiresAt: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	service.SetClock(func() time.Time { return now.Add(2 * time.Second) })
	if _, err := service.ResolveApproval(context.Background(), "task-1", "approval-1", "allow_once"); !errors.Is(err, ErrApprovalExpired) {
		t.Fatalf("expired response error = %v", err)
	}

	service.SetClock(func() time.Time { return now })
	if _, err := service.RequestApproval(context.Background(), ApprovalRequest{
		TaskID: "task-1", ChallengeID: "approval-2", AgentID: "agent-1", Action: "verify", Target: "lab",
		Parameters: map[string]any{}, Risk: "low", ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveApproval(context.Background(), "task-1", "approval-2", "allow_once"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveApproval(context.Background(), "task-1", "approval-2", "allow_once"); !errors.Is(err, ErrApprovalReplay) {
		t.Fatalf("replayed response error = %v", err)
	}
}

func TestServiceRejectsApprovalAfterScopeRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, now)
	confirmTestScope(t, service)
	if _, err := service.RequestApproval(context.Background(), ApprovalRequest{
		TaskID: "task-1", ChallengeID: "approval-1", AgentID: "agent-1", Action: "verify", Target: "lab",
		Parameters: map[string]any{"path": "/login"}, Risk: "low", ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	revised := productprotocol.ScopeSnapshot{
		ID: "scope-2", Principal: "operator", Workspace: "/lab", Validity: "task",
		Targets: []string{"other.lab"}, AllowedActions: []string{"read"}, DeniedActions: []string{"verify"}, RiskCeiling: "low",
	}
	if _, err := service.ProposeScope(context.Background(), "task-1", revised); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveApproval(context.Background(), "task-1", "approval-1", "allow_once"); !errors.Is(err, ErrApprovalOutOfScope) {
		t.Fatalf("response after scope revision error = %v", err)
	}
}

func TestServiceCommitsEvidenceFindingReportAndControlLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, now)
	if _, err := service.CreateTask(context.Background(), "task-1", "Audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartTask(context.Background(), "task-1", "Authorized audit"); err != nil {
		t.Fatal(err)
	}
	evidence := productprotocol.ImmutableEvidence{ID: "e-1", TaskID: "task-1", Kind: "terminal", Summary: "exit 0", Data: map[string]any{"stdout": "ok"}}
	if _, err := service.CommitEvidence(context.Background(), evidence, productprotocol.EventSourceRef{RuntimeID: "runtime-1", ToolCallID: "call-1"}); err != nil {
		t.Fatal(err)
	}
	finding := productprotocol.FindingState{ID: "f-1", Title: "Finding", Severity: "low", Status: "candidate", Confidence: "medium", EvidenceIDs: []string{"e-1"}}
	if _, err := service.CreateFinding(context.Background(), "task-1", finding); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionFinding(context.Background(), "task-1", "f-1", "verifying", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionFinding(context.Background(), "task-1", "f-1", "confirmed", ""); err != nil {
		t.Fatal(err)
	}
	report := productprotocol.ReportState{ID: "r-1", TaskID: "task-1", Version: 0, Status: "draft", Findings: []productprotocol.ReportFinding{}}
	if _, err := service.DraftReport(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	report.Version = 1
	report.Narrative = "Reviewed"
	if _, err := service.EditReport(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if _, err := service.FreezeReport(context.Background(), "task-1", "r-1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TakeControl(context.Background(), "task-1", "client-1", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TakeControl(context.Background(), "task-1", "client-2", 0); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("stale lease error = %v", err)
	}
	_, snapshot, err := service.Store().Load(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Evidence["e-1"].Kind != "terminal" || snapshot.Findings["f-1"].Status != "confirmed" || snapshot.Report == nil || snapshot.Report.Status != "frozen" || snapshot.Report.Version != 2 || snapshot.ControlLease.ClientID != "client-1" {
		t.Fatalf("incomplete authority snapshot: %+v", snapshot)
	}
}

func newTestService(t *testing.T, now time.Time) *Service {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store, "runtime-1", "operator", func() time.Time { return now })
}

func confirmTestScope(t *testing.T, service *Service) {
	t.Helper()
	scope := productprotocol.ScopeSnapshot{ID: "scope-1", Principal: "operator", Workspace: "/lab", Validity: "task", Targets: []string{"lab"}, AllowedActions: []string{"verify"}, DeniedActions: []string{}, RiskCeiling: "medium"}
	if _, err := service.ProposeScope(context.Background(), "task-1", scope); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmScope(context.Background(), "task-1", scope.ID); err != nil {
		t.Fatal(err)
	}
}

func approvalChallenge(t *testing.T, event productprotocol.Event) productprotocol.ApprovalChallenge {
	t.Helper()
	var payload struct {
		Challenge productprotocol.ApprovalChallenge `json:"challenge"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Challenge
}
