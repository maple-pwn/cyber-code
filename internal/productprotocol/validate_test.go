package productprotocol_test

import (
	"encoding/json"
	"errors"
	"testing"

	"cyber-code/internal/productprotocol"
)

func TestValidateAcceptsEveryKnownSchemaV1Event(t *testing.T) {
	t.Parallel()

	scope := map[string]any{
		"id": "scope-1", "principal": "operator", "workspace": "/lab", "validity": "task",
		"targets": []string{"lab"}, "allowedActions": []string{"read"},
		"deniedActions": []string{"destroy"}, "riskCeiling": "medium",
	}
	challenge := map[string]any{
		"id": "approval-1", "agentId": "agent-1", "action": "verify", "target": "lab",
		"parameterDigest": "sha256:abc", "risk": "medium", "expiresAt": "2026-08-03T12:10:00Z",
	}
	finding := map[string]any{
		"id": "finding-1", "title": "Finding", "severity": "high", "status": "candidate",
		"confidence": "medium", "evidenceIds": []string{"evidence-1"},
	}
	report := map[string]any{
		"id": "report-1", "taskId": "task-1", "version": 0, "status": "draft",
		"narrative": "", "recommendations": "", "humanNotes": "", "findings": []any{},
	}
	payloads := map[string]any{
		"task.created":                 map[string]any{"title": "Assessment"},
		"task.started":                 map[string]any{"title": "Assessment"},
		"task.paused":                  map[string]any{},
		"task.resumed":                 map[string]any{},
		"task.cancel.requested":        map[string]any{},
		"task.cancelled":               map[string]any{},
		"task.completed":               map[string]any{},
		"task.failed":                  map[string]any{"reason": "failed"},
		"task.blocked":                 map[string]any{"reason": "blocked"},
		"scope.proposed":               map[string]any{"scope": scope},
		"scope.confirmed":              map[string]any{"scope": scope},
		"runtime.capabilities.updated": map[string]any{"capabilities": []string{"observe"}},
		"control.acquired":             map[string]any{"lease": map[string]any{"clientId": "client-1", "revision": 1}},
		"control.transferred":          map[string]any{"lease": map[string]any{"clientId": "client-2", "revision": 2}},
		"control.released":             map[string]any{"clientId": "client-1"},
		"approval.requested":           map[string]any{"challenge": challenge},
		"approval.resolved":            map[string]any{"challengeId": "approval-1", "decision": "deny"},
		"question.requested":           map[string]any{"questionId": "question-1", "prompt": "Proceed?"},
		"question.resolved":            map[string]any{"questionId": "question-1", "answer": "Yes"},
		"agent.started":                map[string]any{"agent": map[string]any{"id": "agent-1", "name": "Recon", "status": "running"}},
		"agent.progressed":             map[string]any{"agentId": "agent-1", "progress": 50},
		"agent.completed":              map[string]any{"agentId": "agent-1"},
		"agent.failed":                 map[string]any{"agentId": "agent-1", "reason": "failed"},
		"tool.started":                 map[string]any{"callId": "call-1", "name": "read"},
		"tool.completed":               map[string]any{"callId": "call-1", "success": true, "evidenceIds": []string{"evidence-1"}},
		"tool.failed":                  map[string]any{"callId": "call-1", "reason": "failed"},
		"evidence.committed": map[string]any{"evidence": map[string]any{
			"id": "evidence-1", "taskId": "task-1", "kind": "http", "summary": "Observed", "data": map[string]any{},
		}},
		"finding.created":          map[string]any{"finding": finding},
		"finding.verifying":        map[string]any{"findingId": "finding-1"},
		"finding.confirmed":        map[string]any{"findingId": "finding-1"},
		"finding.rejected":         map[string]any{"findingId": "finding-1", "reason": "not reproducible"},
		"finding.mitigated":        map[string]any{"findingId": "finding-1"},
		"report.drafted":           map[string]any{"report": report},
		"report.edited":            map[string]any{"report": report},
		"report.validation.failed": map[string]any{"reportId": "report-1", "reason": "invalid"},
		"report.validated":         map[string]any{"reportId": "report-1"},
		"report.frozen":            map[string]any{"reportId": "report-1", "version": 1},
		"report.exported":          map[string]any{"reportId": "report-1", "format": "pdf"},
	}

	for eventType, payload := range payloads {
		eventType, payload := eventType, payload
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()
			event, err := productprotocol.Validate(rawEvent(t, 1, eventType, payload, nil))
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if event.Kind != productprotocol.EventKindKnown || event.Type != eventType {
				t.Fatalf("Validate() = kind %q, type %q", event.Kind, event.Type)
			}
		})
	}
}

func TestValidateRetainsUnknownJSONEvent(t *testing.T) {
	t.Parallel()

	event, err := productprotocol.Validate(rawEvent(t, 1, "runtime.future.capability", map[string]any{
		"enabled": true,
		"nested":  map[string]any{"values": []any{"one", nil, 3}},
	}, nil))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if event.Kind != productprotocol.EventKindUnknown {
		t.Fatalf("Kind = %q, want %q", event.Kind, productprotocol.EventKindUnknown)
	}
	if string(event.Payload) == "" {
		t.Fatal("unknown payload was discarded")
	}
}

func TestCanonicalJSONMatchesJavaScriptNumberNormalization(t *testing.T) {
	t.Parallel()

	canonical, err := productprotocol.CanonicalJSON(json.RawMessage(`{"value":1.0,"nested":{"b":2,"a":"value"}}`))
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if got, want := string(canonical), `{"nested":{"a":"value","b":2},"value":1}`; got != want {
		t.Fatalf("CanonicalJSON() = %s, want %s", got, want)
	}
}

func TestValidateRejectsMalformedEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  json.RawMessage
		want error
	}{
		{name: "schema", raw: rawEvent(t, 1, "task.started", map[string]any{"title": "x"}, map[string]any{"schemaVersion": 2}), want: productprotocol.ErrUnsupportedSchemaVersion},
		{name: "cursor", raw: rawEvent(t, 1, "task.started", map[string]any{"title": "x"}, map[string]any{"cursor": 0}), want: productprotocol.ErrInvalidEvent},
		{name: "source", raw: rawEvent(t, 1, "task.started", map[string]any{"title": "x"}, map[string]any{"source": map[string]any{}}), want: productprotocol.ErrInvalidEvent},
		{name: "payload", raw: rawEvent(t, 1, "scope.confirmed", map[string]any{"scope": map[string]any{"targets": []string{"lab"}}}, nil), want: productprotocol.ErrInvalidEvent},
		{name: "non-finite JavaScript number", raw: json.RawMessage(`{"schemaVersion":1,"eventId":"event-1","taskId":"task-1","cursor":1,"occurredAt":"2026-08-03T12:00:01Z","type":"runtime.future","source":{"runtimeId":"scenario-local"},"payload":{"value":1e999}}`), want: productprotocol.ErrInvalidEvent},
		{name: "trailing", raw: json.RawMessage(`{"schemaVersion":1} {}`), want: productprotocol.ErrInvalidEvent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := productprotocol.Validate(test.raw); !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func rawEvent(t *testing.T, cursor int, eventType string, payload any, overrides map[string]any) json.RawMessage {
	t.Helper()
	event := map[string]any{
		"schemaVersion": 1,
		"eventId":       "event-1",
		"taskId":        "task-1",
		"cursor":        cursor,
		"occurredAt":    "2026-08-03T12:00:01Z",
		"type":          eventType,
		"source":        map[string]any{"runtimeId": "scenario-local"},
		"payload":       payload,
	}
	for key, value := range overrides {
		event[key] = value
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
