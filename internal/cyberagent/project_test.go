package cyberagent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cyber-code/internal/productstate"
)

func TestProjectCyberAgentSecuritySessionIntoProductState(t *testing.T) {
	t.Parallel()
	events := []EventEnvelope{
		testCyberAgentEvent(1, "session.created", map[string]any{"schema_version": 1, "revision": 1, "session_id": "session-1", "task_id": "task-1", "status": "active"}),
		testCyberAgentEvent(2, "plan.created", map[string]any{"run_id": "run-1", "runtime_revision": 1, "plan_revision": 1, "step_ids": []string{"recon", "verify"}}),
		testCyberAgentEvent(3, "agent.dispatched", map[string]any{"dispatch_ids": []string{"dispatch-1"}, "agent_refs": []string{"agent:recon:1"}}),
		testCyberAgentEvent(4, "tool.receipt", map[string]any{"run_id": "run-1", "runtime_revision": 2, "receipt_index": 1, "action_id": "action-1", "call_id": "call-1", "tool_ref": "nmap", "success": true, "effect": "execute", "evidence_refs": []string{"evidence-1"}, "artifact_refs": []string{"artifact://session-1/nmap"}}),
		testCyberAgentEvent(5, "evidence.committed", map[string]any{"run_id": "run-1", "runtime_revision": 3, "progress_index": 1, "kind": "receipt", "source_ref": "receipt://call-1", "evidence_refs": []string{"evidence-1"}, "artifact_refs": []string{"artifact://session-1/nmap"}}),
		testCyberAgentEvent(6, "scope.proposed", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "scope_id": "scope-1", "targets": []string{"127.0.0.1"}, "scope_digest": testScopeDigest("127.0.0.1"), "interaction_id": "interaction-1"}),
		testCyberAgentEvent(7, "scope.confirmed", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "scope_id": "scope-1", "targets": []string{"127.0.0.1"}, "scope_digest": testScopeDigest("127.0.0.1"), "interaction_id": "interaction-1"}),
		testCyberAgentEvent(8, "artifact.available", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "artifact_id": "artifact-1", "artifact_ref": "artifact://session-1/nmap", "kind": "scan", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "evidence_refs": []string{"evidence-1"}}),
		testCyberAgentEvent(9, "finding.created", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "finding_id": "finding-1", "title": "Open service", "severity": "medium", "status": "created", "evidence_refs": []string{"evidence-1"}, "artifact_refs": []string{"artifact://session-1/nmap"}}),
		testCyberAgentEvent(10, "finding.verifying", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "finding_id": "finding-1", "title": "Open service", "severity": "medium", "status": "verifying", "evidence_refs": []string{"evidence-1"}, "artifact_refs": []string{"artifact://session-1/nmap"}}),
		testCyberAgentEvent(11, "finding.confirmed", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "finding_id": "finding-1", "title": "Open service", "severity": "medium", "status": "confirmed", "evidence_refs": []string{"evidence-1"}, "artifact_refs": []string{"artifact://session-1/nmap"}}),
		testCyberAgentEvent(12, "report.drafted", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "report_id": "report-1", "status": "drafted", "finding_ids": []string{"finding-1"}, "evidence_refs": []string{"evidence-1"}, "artifact_refs": []string{"artifact://session-1/nmap"}}),
		testCyberAgentEvent(13, "report.frozen", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "report_id": "report-1", "status": "frozen", "finding_ids": []string{"finding-1"}, "evidence_refs": []string{"evidence-1"}, "artifact_refs": []string{"artifact://session-1/nmap"}}),
		testCyberAgentEvent(14, "runtime.future.event", map[string]any{"future": true}),
	}

	state := productstate.Initial()
	for _, source := range events {
		mapped, err := ProjectEvent(source, "cyber-agent:local")
		if err != nil {
			t.Fatalf("ProjectEvent(%s): %v", source.Topic, err)
		}
		if mapped.EventID != source.EventID || mapped.Cursor != source.Sequence || mapped.Origin == nil || mapped.Origin.SourceSessionID != "session-1" || mapped.Origin.SourceTopic != source.Topic {
			t.Fatalf("mapped identity for %s = %#v", source.Topic, mapped)
		}
		result, err := productstate.Project(state, mapped)
		if err != nil {
			t.Fatalf("productstate.Project(%s): %v", source.Topic, err)
		}
		if result.Kind != productstate.ProjectionApplied {
			t.Fatalf("projection kind for %s = %s", source.Topic, result.Kind)
		}
		state = result.State
	}

	if state.Plan == nil || state.Plan.Revision != 1 || len(state.Plan.StepIDs) != 2 {
		t.Fatalf("plan = %#v", state.Plan)
	}
	if agent := state.Agents["agent:recon:1"]; agent.Status != "running" || agent.AgentID != "dispatch-1" {
		t.Fatalf("agent = %#v", agent)
	}
	if receipt := state.ToolReceipts["call-1"]; !receipt.Success || receipt.Tool != "nmap" || len(receipt.EvidenceIDs) != 1 {
		t.Fatalf("tool receipt = %#v", receipt)
	}
	if reference := state.EvidenceReferences["evidence-1"]; reference.SourceRef != "receipt://call-1" {
		t.Fatalf("evidence reference = %#v", reference)
	}
	if state.Scope == nil || state.Scope.ID != "scope-1" || len(state.Scope.Targets) != 1 {
		t.Fatalf("scope = %#v", state.Scope)
	}
	if artifact := state.Artifacts["artifact-1"]; artifact.SHA256 == "" || artifact.Reference == "" {
		t.Fatalf("artifact = %#v", artifact)
	}
	if finding := state.Findings["finding-1"]; finding.Status != "confirmed" || len(finding.EvidenceIDs) != 1 {
		t.Fatalf("finding = %#v", finding)
	}
	if state.Report == nil || state.Report.ID != "report-1" || state.Report.Status != "frozen" || len(state.Report.ArtifactRefs) != 1 {
		t.Fatalf("report = %#v", state.Report)
	}
	if len(state.RawEvents) != 1 || state.RawEvents[0].Type != "runtime.future.event" {
		t.Fatalf("raw events = %#v", state.RawEvents)
	}
}

func TestProjectCyberAgentLifecycleMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		topic   string
		payload any
		want    string
	}{
		{topic: "session.status", payload: map[string]any{"schema_version": 1, "revision": 2, "status": "active"}, want: "task.started"},
		{topic: "session.terminal", payload: map[string]any{"schema_version": 1, "revision": 3, "status": "completed"}, want: "task.completed"},
		{topic: "subagent.lifecycle", payload: map[string]any{"schema_version": 1, "revision": 2, "subagent_ref": "agent:worker:1", "state": "dispatched"}, want: "agent.dispatched"},
		{topic: "agent.result", payload: map[string]any{"dispatch_id": "dispatch-1", "agent_ref": "agent:worker:1", "status": "succeeded", "safe_boundary": true, "skill_refs": []string{}, "skill_versions": []string{}, "skill_digests": []string{}}, want: "agent.completed"},
		{topic: "interaction.requested", payload: map[string]any{"schema_version": 1, "revision": 2, "interaction_id": "interaction-1", "kind": "approval", "action_ref": "action-1"}, want: "interaction.requested"},
		{topic: "interaction.responded", payload: map[string]any{"schema_version": 1, "revision": 3, "interaction_id": "interaction-1", "outcome": "approved"}, want: "interaction.responded"},
	}
	for index, test := range tests {
		event := testCyberAgentEvent(index+1, test.topic, test.payload)
		mapped, err := ProjectEvent(event, "cyber-agent:local")
		if err != nil || mapped.Type != test.want || mapped.Kind != "known" {
			t.Fatalf("ProjectEvent(%s) = type %q kind %q, err=%v", test.topic, mapped.Type, mapped.Kind, err)
		}
	}
}

func TestProjectCyberAgentRejectsInvalidSecurityProductContract(t *testing.T) {
	t.Parallel()
	invalid := []EventEnvelope{
		testCyberAgentEvent(1, "scope.proposed", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "scope_id": "scope-1", "targets": []string{"lab"}, "scope_digest": "bad", "interaction_id": "i"}),
		testCyberAgentEvent(1, "finding.confirmed", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "finding_id": "finding-1", "title": "x", "severity": "high", "status": "created", "evidence_refs": []string{}, "artifact_refs": []string{}}),
		testCyberAgentEvent(1, "report.frozen", map[string]any{"schema_version": 1, "session_id": "session-1", "task_id": "task-1", "report_id": "report-1", "status": "drafted", "finding_ids": []string{}, "evidence_refs": []string{}, "artifact_refs": []string{}}),
	}
	for _, event := range invalid {
		if _, err := ProjectEvent(event, "cyber-agent:local"); err == nil {
			t.Fatalf("ProjectEvent accepted invalid %s payload", event.Topic)
		}
	}
}

func TestProjectCyberAgentRejectsBindingAndCursorMismatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		event EventEnvelope
	}{
		{name: "task", event: testCyberAgentEvent(1, "scope.proposed", map[string]any{"session_id": "session-1", "task_id": "other", "scope_id": "scope-1", "targets": []string{"lab"}, "interaction_id": "i"})},
		{name: "session", event: func() EventEnvelope {
			event := testCyberAgentEvent(1, "session.created", map[string]any{})
			event.SessionID = "other"
			return event
		}()},
		{name: "sequence", event: func() EventEnvelope {
			event := testCyberAgentEvent(1, "session.created", map[string]any{})
			event.Sequence = 0
			return event
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ProjectEvent(test.event, "runtime"); err == nil {
				t.Fatal("expected binding error")
			}
		})
	}
}

func testCyberAgentEvent(sequence int, topic string, payload any) EventEnvelope {
	encoded, _ := json.Marshal(payload)
	return EventEnvelope{
		EventID: "source-event-" + string(rune('a'+sequence-1)), TaskID: "task-1", SessionID: "session-1",
		Sequence: sequence, Topic: topic, Payload: encoded, EmittedBy: "system", EmittedAt: time.Date(2026, 8, 9, 12, 0, sequence, 0, time.UTC),
	}
}

func testScopeDigest(targets ...string) string {
	encoded, _ := json.Marshal(targets)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest[:])
}
