package productstate_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
)

type fixtureManifest struct {
	Fixtures []fixtureManifestEntry `json:"fixtures"`
}

type fixtureManifestEntry struct {
	Name                  string                `json:"name"`
	File                  string                `json:"file"`
	ExpectedOutcomes      []string              `json:"expectedOutcomes"`
	ExpectedTerminalState expectedTerminalState `json:"expectedTerminalState"`
	StateSHA256           string                `json:"stateSha256"`
}

type expectedTerminalState struct {
	CommittedCursor int    `json:"committedCursor"`
	TaskStatus      string `json:"taskStatus"`
	TimelineLength  int    `json:"timelineLength"`
	RawEventCount   int    `json:"rawEventCount"`
}

type eventFixture struct {
	Name   string            `json:"name"`
	Events []json.RawMessage `json:"events"`
}

func TestFixtureProjectionMatchesTypeScriptStateDigests(t *testing.T) {
	t.Parallel()

	root := fixtureRoot(t)
	var manifest fixtureManifest
	readJSON(t, filepath.Join(root, "manifest.json"), &manifest)

	for _, entry := range manifest.Fixtures {
		entry := entry
		t.Run(entry.Name, func(t *testing.T) {
			t.Parallel()
			var fixture eventFixture
			readJSON(t, filepath.Join(root, entry.File), &fixture)

			state := productstate.Initial()
			outcomes := make([]string, 0, len(fixture.Events))
			for _, raw := range fixture.Events {
				event, err := productprotocol.Validate(raw)
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				result, err := productstate.Project(state, event)
				if err != nil {
					outcomes = append(outcomes, "error:"+err.Error())
					continue
				}
				outcomes = append(outcomes, projectionOutcome(result))
				state = result.State
			}

			if !equalStrings(outcomes, entry.ExpectedOutcomes) {
				t.Fatalf("outcomes = %v, want %v", outcomes, entry.ExpectedOutcomes)
			}
			status := ""
			if state.Task != nil {
				status = state.Task.Status
			}
			gotTerminal := expectedTerminalState{
				CommittedCursor: state.CommittedCursor,
				TaskStatus:      status,
				TimelineLength:  len(state.Timeline),
				RawEventCount:   len(state.RawEvents),
			}
			if gotTerminal != entry.ExpectedTerminalState {
				t.Fatalf("terminal state = %+v, want %+v", gotTerminal, entry.ExpectedTerminalState)
			}
			canonical, err := productprotocol.CanonicalJSON(state)
			if err != nil {
				t.Fatalf("CanonicalJSON() error = %v", err)
			}
			digest := sha256.Sum256(canonical)
			if got := hex.EncodeToString(digest[:]); got != entry.StateSHA256 {
				t.Fatalf("state digest = %s, want %s\n%s", got, entry.StateSHA256, canonical)
			}
		})
	}
}

func TestProjectDoesNotMutateOrAliasPreviousState(t *testing.T) {
	t.Parallel()

	evidence := mustEvent(t, 1, "evidence.committed", map[string]any{"evidence": map[string]any{
		"id": "evidence-1", "taskId": "task-1", "kind": "http", "summary": "Observed",
		"data": map[string]any{"path": "/login"},
	}}, nil)
	first, err := productstate.Project(productstate.Initial(), evidence)
	if err != nil {
		t.Fatal(err)
	}
	storedPayloadByte := first.State.Timeline[0].Payload[0]
	evidence.Payload[0] = 'x'
	if first.State.Timeline[0].Payload[0] != storedPayloadByte {
		t.Fatal("stored event aliases caller payload")
	}
	second, err := productstate.Project(first.State, mustEvent(t, 2, "task.started", map[string]any{"title": "Assessment"}, nil))
	if err != nil {
		t.Fatal(err)
	}

	second.State.Evidence["evidence-1"].Data["path"] = "/changed"
	second.State.Timeline[0].Payload[0] = 'x'
	if got := first.State.Evidence["evidence-1"].Data["path"]; got != "/login" {
		t.Fatalf("previous evidence mutated to %v", got)
	}
	if first.State.Timeline[0].Payload[0] == 'x' {
		t.Fatal("previous timeline payload aliases the next state")
	}
}

func TestProjectEnforcesFindingTransitions(t *testing.T) {
	t.Parallel()

	state := productstate.Initial()
	created := mustEvent(t, 1, "finding.created", map[string]any{"finding": map[string]any{
		"id": "finding-1", "title": "Finding", "severity": "high", "status": "candidate",
		"confidence": "medium", "evidenceIds": []string{},
	}}, nil)
	result, err := productstate.Project(state, created)
	if err != nil {
		t.Fatal(err)
	}
	state = result.State

	if _, err := productstate.Project(state, mustEvent(t, 2, "finding.confirmed", map[string]any{"findingId": "finding-1"}, nil)); !errors.Is(err, productstate.ErrInvalidFindingTransition) {
		t.Fatalf("direct confirmation error = %v", err)
	}
	for cursor, eventType := range []string{"finding.verifying", "finding.confirmed", "finding.mitigated"} {
		result, err = productstate.Project(state, mustEvent(t, cursor+2, eventType, map[string]any{"findingId": "finding-1"}, nil))
		if err != nil {
			t.Fatalf("%s error = %v", eventType, err)
		}
		state = result.State
	}
	if state.Findings["finding-1"].Status != "mitigated" {
		t.Fatalf("finding status = %q", state.Findings["finding-1"].Status)
	}
}

func TestProjectEnforcesApprovalResolutionAndExpiry(t *testing.T) {
	t.Parallel()

	requested := mustEvent(t, 1, "approval.requested", map[string]any{"challenge": map[string]any{
		"id": "approval-1", "agentId": "agent-1", "action": "verify", "target": "lab",
		"parameterDigest": "sha256:abc", "risk": "medium", "expiresAt": "2026-08-03T12:00:10Z",
	}}, nil)
	result, err := productstate.Project(productstate.Initial(), requested)
	if err != nil {
		t.Fatal(err)
	}
	resolved := mustEvent(t, 2, "approval.resolved", map[string]any{"challengeId": "approval-1", "decision": "deny"}, nil)
	result, err = productstate.Project(result.State, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := productstate.Project(result.State, mustEvent(t, 3, "approval.resolved", map[string]any{"challengeId": "approval-1", "decision": "deny"}, map[string]any{"eventId": "event-approval-resolved-again"})); !errors.Is(err, productstate.ErrApprovalAlreadyResolved) {
		t.Fatalf("second resolution error = %v", err)
	}

	expired := mustEvent(t, 1, "approval.requested", map[string]any{"challenge": map[string]any{
		"id": "approval-2", "agentId": "agent-1", "action": "verify", "target": "lab",
		"parameterDigest": "sha256:def", "risk": "medium", "expiresAt": "2026-08-03T12:00:01Z",
	}}, nil)
	result, err = productstate.Project(productstate.Initial(), expired)
	if err != nil {
		t.Fatal(err)
	}
	late := mustEvent(t, 2, "approval.resolved", map[string]any{"challengeId": "approval-2", "decision": "deny"}, map[string]any{"occurredAt": "2026-08-03T12:00:02Z"})
	if _, err := productstate.Project(result.State, late); !errors.Is(err, productstate.ErrApprovalExpired) {
		t.Fatalf("expired resolution error = %v", err)
	}
}

func TestProjectEnforcesLeaseAndCursorMonotonicity(t *testing.T) {
	t.Parallel()

	acquired := mustEvent(t, 1, "control.acquired", map[string]any{"lease": map[string]any{"clientId": "client-1", "revision": 1}}, nil)
	result, err := productstate.Project(productstate.Initial(), acquired)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := productstate.Project(result.State, mustEvent(t, 2, "control.transferred", map[string]any{"lease": map[string]any{"clientId": "client-2", "revision": 1}}, nil)); !errors.Is(err, productstate.ErrNonMonotonicLeaseRevision) {
		t.Fatalf("lease error = %v", err)
	}
	if _, err := productstate.Project(result.State, mustEvent(t, 1, "task.paused", map[string]any{}, map[string]any{"eventId": "stale-event"})); !errors.Is(err, productstate.ErrStaleCursor) {
		t.Fatalf("stale cursor error = %v", err)
	}
}

func projectionOutcome(result productstate.ProjectionResult) string {
	if result.Kind == productstate.ProjectionResyncRequired {
		return "resync-required:" + strconv.Itoa(result.ExpectedCursor)
	}
	return string(result.Kind)
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "product-events"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func mustEvent(t *testing.T, cursor int, eventType string, payload any, overrides map[string]any) productprotocol.Event {
	t.Helper()
	raw := map[string]any{
		"schemaVersion": 1, "eventId": "event-" + eventType, "taskId": "task-1", "cursor": cursor,
		"occurredAt": "2026-08-03T12:00:01Z", "type": eventType,
		"source": map[string]any{"runtimeId": "scenario-local"}, "payload": payload,
	}
	for key, value := range overrides {
		raw[key] = value
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	event, err := productprotocol.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
