package cyberagent

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"cyber-code/internal/productprotocol"
)

func ProjectEvent(source EventEnvelope, runtimeID string) (productprotocol.Event, error) {
	if strings.TrimSpace(runtimeID) == "" || source.EventID == "" || source.TaskID == "" || source.SessionID == "" || source.Sequence < 1 || source.Topic == "" || source.EmittedAt.IsZero() {
		return productprotocol.Event{}, fmt.Errorf("cyber-agent event envelope is incomplete")
	}
	typeName, payload, agentID, toolCallID, known, err := mapSourcePayload(source)
	if err != nil {
		return productprotocol.Event{}, fmt.Errorf("map cyber-agent %s event: %w", source.Topic, err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return productprotocol.Event{}, err
	}
	kind := productprotocol.EventKindUnknown
	if known {
		kind = productprotocol.EventKindKnown
	}
	event := productprotocol.Event{
		SchemaVersion: productprotocol.SchemaVersion, EventID: source.EventID, TaskID: source.TaskID,
		Cursor: source.Sequence, OccurredAt: source.EmittedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Type: typeName, Source: productprotocol.EventSourceRef{RuntimeID: runtimeID, AgentID: agentID, ToolCallID: toolCallID},
		Payload: encoded, Kind: kind,
		Origin: &productprotocol.EventOrigin{SourceSessionID: source.SessionID, SourceSequence: source.Sequence, SourceEventID: source.EventID, SourceTopic: source.Topic},
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return productprotocol.Event{}, err
	}
	validated, err := productprotocol.Validate(raw)
	if err != nil {
		return productprotocol.Event{}, fmt.Errorf("validate mapped product event: %w", err)
	}
	validated.Origin = event.Origin
	return validated, nil
}

func mapSourcePayload(source EventEnvelope) (string, any, string, string, bool, error) {
	switch source.Topic {
	case "session.created":
		var value struct {
			SessionID string `json:"session_id"`
			TaskID    string `json:"task_id"`
			Status    string `json:"status"`
			Revision  int    `json:"revision"`
			Schema    int    `json:"schema_version"`
		}
		if err := decodeMappedPayload(source, &value); err != nil || value.SessionID != source.SessionID || value.TaskID != source.TaskID {
			return "", nil, "", "", false, bindingError(err)
		}
		if value.Schema != 1 || value.Revision < 1 || !validSessionStatus(value.Status) {
			return "", nil, "", "", false, fmt.Errorf("invalid session.created payload")
		}
		return "task.created", map[string]any{"title": "Security task " + source.TaskID}, "", "", true, nil
	case "session.status", "session.terminal":
		var value struct {
			Schema   int    `json:"schema_version"`
			Revision int    `json:"revision"`
			Status   string `json:"status"`
		}
		if err := decodeMappedPayload(source, &value); err != nil {
			return "", nil, "", "", false, err
		}
		if value.Schema != 1 || value.Revision < 1 || !validSessionStatus(value.Status) {
			return "", nil, "", "", false, fmt.Errorf("invalid %s payload", source.Topic)
		}
		switch value.Status {
		case "active":
			return "task.started", map[string]any{"title": "Security task " + source.TaskID}, "", "", true, nil
		case "waiting_interaction":
			return "task.paused", map[string]any{}, "", "", true, nil
		case "completed":
			return "task.completed", map[string]any{}, "", "", true, nil
		case "cancelled":
			return "task.cancelled", map[string]any{}, "", "", true, nil
		default:
			return "task.failed", map[string]any{"reason": "cyber-agent session failed"}, "", "", true, nil
		}
	case "plan.created", "plan.revised":
		var value struct {
			RunID           string   `json:"run_id"`
			RuntimeRevision int      `json:"runtime_revision"`
			PlanRevision    int      `json:"plan_revision"`
			StepIDs         []string `json:"step_ids"`
		}
		if err := decodeMappedPayload(source, &value); err != nil {
			return "", nil, "", "", false, err
		}
		return source.Topic, map[string]any{"plan": productprotocol.PlanState{RunID: value.RunID, Revision: value.PlanRevision, StepIDs: value.StepIDs}}, "", "", true, nil
	case "agent.dispatched":
		var value struct {
			DispatchIDs []string   `json:"dispatch_ids"`
			AgentRefs   []string   `json:"agent_refs"`
			HarnessRefs []*string  `json:"harness_refs"`
			SkillRefs   [][]string `json:"skill_refs"`
		}
		if err := decodeMappedPayload(source, &value); err != nil {
			return "", nil, "", "", false, err
		}
		if len(value.DispatchIDs) == 0 || len(value.DispatchIDs) != len(value.AgentRefs) {
			return "", nil, "", "", false, fmt.Errorf("dispatch and agent references do not match")
		}
		agents := make([]productprotocol.AgentState, 0, len(value.AgentRefs))
		for index, agentRef := range value.AgentRefs {
			agents = append(agents, productprotocol.AgentState{ID: agentRef, Name: agentRef, Status: "running", AgentID: value.DispatchIDs[index]})
		}
		return "agent.dispatched", map[string]any{"agents": agents}, "", "", true, nil
	case "subagent.lifecycle":
		var value struct {
			Schema      int    `json:"schema_version"`
			Revision    int    `json:"revision"`
			SubagentRef string `json:"subagent_ref"`
			State       string `json:"state"`
		}
		if err := decodeMappedPayload(source, &value); err != nil {
			return "", nil, "", "", false, err
		}
		if value.Schema != 1 || value.Revision < 1 || value.SubagentRef == "" || value.State == "" {
			return "", nil, "", "", false, fmt.Errorf("invalid subagent lifecycle payload")
		}
		agent := productprotocol.AgentState{ID: value.SubagentRef, Name: value.SubagentRef, Status: value.State, AgentID: value.SubagentRef}
		return "agent.dispatched", map[string]any{"agents": []productprotocol.AgentState{agent}}, value.SubagentRef, "", true, nil
	case "agent.result":
		var value struct {
			DispatchID      string   `json:"dispatch_id"`
			AgentRef        string   `json:"agent_ref"`
			HarnessRef      *string  `json:"harness_ref"`
			HarnessVersion  *string  `json:"harness_version"`
			SkillRefs       []string `json:"skill_refs"`
			SkillVersions   []string `json:"skill_versions"`
			SkillDigests    []string `json:"skill_digests"`
			Status          string   `json:"status"`
			FailureCategory *string  `json:"failure_category"`
			SafeBoundary    bool     `json:"safe_boundary"`
		}
		if err := decodeMappedPayload(source, &value); err != nil {
			return "", nil, "", "", false, err
		}
		if value.DispatchID == "" || value.AgentRef == "" || !slices.Contains([]string{"succeeded", "failed", "blocked", "cancelled"}, value.Status) {
			return "", nil, "", "", false, fmt.Errorf("invalid agent result payload")
		}
		if value.Status == "succeeded" {
			return "agent.completed", map[string]any{"agentId": value.AgentRef}, value.AgentRef, "", true, nil
		}
		reason := value.Status
		if value.FailureCategory != nil && *value.FailureCategory != "" {
			reason = *value.FailureCategory
		}
		return "agent.failed", map[string]any{"agentId": value.AgentRef, "reason": reason}, value.AgentRef, "", true, nil
	case "tool.receipt":
		var value struct {
			RunID           string   `json:"run_id"`
			RuntimeRevision int      `json:"runtime_revision"`
			ReceiptIndex    int      `json:"receipt_index"`
			ActionID        string   `json:"action_id"`
			CallID          string   `json:"call_id"`
			ToolRef         string   `json:"tool_ref"`
			Success         bool     `json:"success"`
			Effect          string   `json:"effect"`
			EvidenceRefs    []string `json:"evidence_refs"`
			ArtifactRefs    []string `json:"artifact_refs"`
		}
		if err := decodeMappedPayload(source, &value); err != nil {
			return "", nil, "", "", false, err
		}
		receipt := productprotocol.ToolReceiptState{CallID: value.CallID, ActionID: value.ActionID, Tool: value.ToolRef, Success: value.Success, Effect: value.Effect, EvidenceIDs: value.EvidenceRefs, ArtifactRefs: value.ArtifactRefs}
		return "tool.receipt", map[string]any{"receipt": receipt}, "", value.CallID, true, nil
	case "evidence.committed":
		var value struct {
			RunID           string   `json:"run_id"`
			RuntimeRevision int      `json:"runtime_revision"`
			ProgressIndex   int      `json:"progress_index"`
			Kind            string   `json:"kind"`
			SourceRef       string   `json:"source_ref"`
			EvidenceRefs    []string `json:"evidence_refs"`
			ArtifactRefs    []string `json:"artifact_refs"`
		}
		if err := decodeMappedPayload(source, &value); err != nil {
			return "", nil, "", "", false, err
		}
		references := make([]productprotocol.EvidenceReferenceState, 0, len(value.EvidenceRefs))
		for _, id := range value.EvidenceRefs {
			references = append(references, productprotocol.EvidenceReferenceState{ID: id, Kind: value.Kind, SourceRef: value.SourceRef, ArtifactRefs: value.ArtifactRefs})
		}
		if len(references) == 0 {
			return unknownMapping(source)
		}
		return "evidence.references.committed", map[string]any{"references": references}, "", "", true, nil
	case "scope.proposed", "scope.confirmed":
		var value securityScopePayload
		if err := decodeMappedPayload(source, &value); err != nil || value.SessionID != source.SessionID || value.TaskID != source.TaskID {
			return "", nil, "", "", false, bindingError(err)
		}
		targets, digest, err := canonicalScope(value.Targets)
		if err != nil || value.Schema != 1 || value.ScopeID == "" || value.InteractionID == "" || value.ScopeDigest != digest {
			return "", nil, "", "", false, fmt.Errorf("invalid Scope product payload")
		}
		scope := productprotocol.ScopeSnapshot{ID: value.ScopeID, Principal: "cyber-agent", Workspace: "security-runtime", Validity: "session", Targets: targets, AllowedActions: []string{}, DeniedActions: []string{}, RiskCeiling: "runtime-authoritative"}
		return source.Topic, map[string]any{"scope": scope}, "", "", true, nil
	case "artifact.available":
		var value securityArtifactPayload
		if err := decodeMappedPayload(source, &value); err != nil || value.SessionID != source.SessionID || value.TaskID != source.TaskID {
			return "", nil, "", "", false, bindingError(err)
		}
		if value.Schema != 1 || value.ArtifactID == "" || value.ArtifactRef == "" || value.Kind == "" {
			return "", nil, "", "", false, fmt.Errorf("invalid Artifact product payload")
		}
		artifact := productprotocol.ArtifactState{ID: value.ArtifactID, Reference: value.ArtifactRef, Kind: value.Kind, SHA256: value.SHA256, EvidenceIDs: value.EvidenceRefs}
		return "artifact.available", map[string]any{"artifact": artifact}, "", "", true, nil
	case "finding.created", "finding.verifying", "finding.confirmed", "finding.rejected":
		var value securityFindingPayload
		if err := decodeMappedPayload(source, &value); err != nil || value.SessionID != source.SessionID || value.TaskID != source.TaskID {
			return "", nil, "", "", false, bindingError(err)
		}
		expectedStatus := strings.TrimPrefix(source.Topic, "finding.")
		if value.Schema != 1 || value.FindingID == "" || value.Title == "" || !slices.Contains([]string{"info", "low", "medium", "high", "critical"}, value.Severity) || value.Status != expectedStatus {
			return "", nil, "", "", false, fmt.Errorf("invalid Finding product payload")
		}
		if source.Topic == "finding.created" {
			finding := productprotocol.FindingState{ID: value.FindingID, Title: value.Title, Severity: value.Severity, Status: "candidate", Confidence: "runtime", EvidenceIDs: value.EvidenceRefs}
			return source.Topic, map[string]any{"finding": finding}, "", "", true, nil
		}
		payload := map[string]any{"findingId": value.FindingID}
		if source.Topic == "finding.rejected" {
			payload["reason"] = "rejected by cyber-agent verification"
		}
		return source.Topic, payload, "", "", true, nil
	case "report.drafted", "report.frozen":
		var value securityReportPayload
		if err := decodeMappedPayload(source, &value); err != nil || value.SessionID != source.SessionID || value.TaskID != source.TaskID {
			return "", nil, "", "", false, bindingError(err)
		}
		expectedStatus := strings.TrimPrefix(source.Topic, "report.")
		if value.Schema != 1 || value.ReportID == "" || value.Status != expectedStatus {
			return "", nil, "", "", false, fmt.Errorf("invalid Report product payload")
		}
		if source.Topic == "report.drafted" {
			report := productprotocol.ReportState{ID: value.ReportID, TaskID: value.TaskID, Status: "draft", Findings: []productprotocol.ReportFinding{}, FindingIDs: value.FindingIDs, EvidenceIDs: value.EvidenceRefs, ArtifactRefs: value.ArtifactRefs}
			return source.Topic, map[string]any{"report": report}, "", "", true, nil
		}
		return source.Topic, map[string]any{"reportId": value.ReportID, "version": 1}, "", "", true, nil
	case "interaction.requested", "interaction.responded", "interaction.cancelled":
		var value struct {
			InteractionID string  `json:"interaction_id"`
			Kind          string  `json:"kind"`
			ActionRef     *string `json:"action_ref"`
			Outcome       string  `json:"outcome"`
			Schema        int     `json:"schema_version"`
			Revision      int     `json:"revision"`
		}
		if err := decodeMappedPayload(source, &value); err != nil {
			return "", nil, "", "", false, err
		}
		if value.Schema != 1 || value.Revision < 1 || value.InteractionID == "" {
			return "", nil, "", "", false, fmt.Errorf("invalid interaction payload")
		}
		status := "pending"
		if source.Topic == "interaction.responded" {
			status = "responded"
		} else if source.Topic == "interaction.cancelled" {
			status = "cancelled"
		}
		actionRef := ""
		if value.ActionRef != nil {
			actionRef = *value.ActionRef
		}
		if value.Kind == "" {
			value.Kind = "interaction"
		}
		interaction := productprotocol.InteractionState{ID: value.InteractionID, Kind: value.Kind, ActionRef: actionRef, Status: status, Outcome: value.Outcome}
		return source.Topic, map[string]any{"interaction": interaction}, "", "", true, nil
	default:
		return unknownMapping(source)
	}
}

func validSessionStatus(status string) bool {
	return slices.Contains([]string{"active", "waiting_interaction", "completed", "failed", "cancelled"}, status)
}

func canonicalScope(targets []string) ([]string, string, error) {
	normalized := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			return nil, "", fmt.Errorf("blank Scope target")
		}
		if _, exists := seen[target]; exists {
			return nil, "", fmt.Errorf("duplicate Scope target")
		}
		seen[target] = struct{}{}
		normalized = append(normalized, target)
	}
	if len(normalized) == 0 {
		return nil, "", fmt.Errorf("Scope targets are required")
	}
	sort.Strings(normalized)
	encoded, _ := json.Marshal(normalized)
	digest := sha256.Sum256(encoded)
	return normalized, fmt.Sprintf("%x", digest[:]), nil
}

type securityScopePayload struct {
	Schema        int      `json:"schema_version"`
	SessionID     string   `json:"session_id"`
	TaskID        string   `json:"task_id"`
	ScopeID       string   `json:"scope_id"`
	Targets       []string `json:"targets"`
	ScopeDigest   string   `json:"scope_digest"`
	InteractionID string   `json:"interaction_id"`
}

type securityFindingPayload struct {
	Schema       int      `json:"schema_version"`
	SessionID    string   `json:"session_id"`
	TaskID       string   `json:"task_id"`
	FindingID    string   `json:"finding_id"`
	Title        string   `json:"title"`
	Severity     string   `json:"severity"`
	Status       string   `json:"status"`
	EvidenceRefs []string `json:"evidence_refs"`
	ArtifactRefs []string `json:"artifact_refs"`
}

type securityArtifactPayload struct {
	Schema       int      `json:"schema_version"`
	SessionID    string   `json:"session_id"`
	TaskID       string   `json:"task_id"`
	ArtifactID   string   `json:"artifact_id"`
	ArtifactRef  string   `json:"artifact_ref"`
	Kind         string   `json:"kind"`
	SHA256       string   `json:"sha256"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type securityReportPayload struct {
	Schema       int      `json:"schema_version"`
	SessionID    string   `json:"session_id"`
	TaskID       string   `json:"task_id"`
	ReportID     string   `json:"report_id"`
	Status       string   `json:"status"`
	FindingIDs   []string `json:"finding_ids"`
	EvidenceRefs []string `json:"evidence_refs"`
	ArtifactRefs []string `json:"artifact_refs"`
}

func decodeMappedPayload(source EventEnvelope, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(source.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON content")
	}
	return nil
}

func bindingError(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("payload task/session binding does not match event envelope")
}

func unknownMapping(source EventEnvelope) (string, any, string, string, bool, error) {
	var payload any
	decoder := json.NewDecoder(bytes.NewReader(source.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return "", nil, "", "", false, err
	}
	return source.Topic, payload, "", "", false, nil
}
