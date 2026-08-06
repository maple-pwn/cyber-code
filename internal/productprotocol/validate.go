package productprotocol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"time"
	"unicode"
)

var (
	ErrUnsupportedSchemaVersion = errors.New("unsupported_schema_version")
	ErrInvalidEvent             = errors.New("invalid_event")
)

const maxSafeJSONInteger = 1<<53 - 1

var knownEventTypes = map[string]struct{}{
	"task.created": {}, "task.started": {}, "task.paused": {}, "task.resumed": {},
	"task.cancel.requested": {}, "task.cancelled": {}, "task.completed": {},
	"task.failed": {}, "task.blocked": {}, "scope.proposed": {}, "scope.confirmed": {},
	"runtime.capabilities.updated": {}, "control.acquired": {}, "control.transferred": {},
	"control.released": {}, "approval.requested": {}, "approval.resolved": {},
	"question.requested": {}, "question.resolved": {}, "agent.started": {},
	"agent.progressed": {}, "agent.completed": {}, "agent.failed": {}, "tool.started": {},
	"tool.completed": {}, "tool.failed": {}, "evidence.committed": {}, "finding.created": {},
	"finding.verifying": {}, "finding.confirmed": {}, "finding.rejected": {},
	"finding.mitigated": {}, "report.drafted": {}, "report.edited": {},
	"report.validation.failed": {}, "report.validated": {}, "report.frozen": {},
	"report.exported": {},
	"terminal.opened": {}, "terminal.output": {}, "terminal.input.accepted": {},
	"terminal.resized": {}, "terminal.exited": {},
	"editor.draft.opened": {}, "editor.draft.saved": {}, "editor.patch.applied": {},
	"editor.patch.verified": {}, "editor.draft.discarded": {},
}

func Validate(raw json.RawMessage) (Event, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return Event{}, ErrInvalidEvent
	}
	if !validJSONValue(object) {
		return Event{}, ErrInvalidEvent
	}
	schemaVersion, ok := safeInteger(object["schemaVersion"])
	if !ok {
		return Event{}, ErrInvalidEvent
	}
	if schemaVersion != SchemaVersion {
		return Event{}, ErrUnsupportedSchemaVersion
	}
	cursor, ok := safeInteger(object["cursor"])
	if !ok || cursor < 1 {
		return Event{}, ErrInvalidEvent
	}
	eventID, ok := requiredString(object["eventId"])
	if !ok {
		return Event{}, ErrInvalidEvent
	}
	taskID, ok := requiredString(object["taskId"])
	if !ok {
		return Event{}, ErrInvalidEvent
	}
	occurredAt, ok := requiredString(object["occurredAt"])
	if !ok || !validTimestamp(occurredAt) {
		return Event{}, ErrInvalidEvent
	}
	eventType, ok := requiredString(object["type"])
	if !ok {
		return Event{}, ErrInvalidEvent
	}
	source, ok := validSource(object["source"])
	if !ok {
		return Event{}, ErrInvalidEvent
	}
	payload, ok := object["payload"].(map[string]any)
	if !ok {
		return Event{}, ErrInvalidEvent
	}
	if _, known := knownEventTypes[eventType]; known && !validKnownPayload(eventType, payload) {
		return Event{}, ErrInvalidEvent
	}
	payloadJSON, err := encodeJSON(payload)
	if err != nil {
		return Event{}, ErrInvalidEvent
	}
	kind := EventKindUnknown
	if _, known := knownEventTypes[eventType]; known {
		kind = EventKindKnown
	}
	return Event{
		SchemaVersion: schemaVersion,
		EventID:       eventID,
		TaskID:        taskID,
		Cursor:        cursor,
		OccurredAt:    occurredAt,
		Type:          eventType,
		Source:        source,
		Payload:       payloadJSON,
		Kind:          kind,
	}, nil
}

func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := encodeJSON(value)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeCanonicalValue(encoded)
	if err != nil {
		return nil, err
	}
	return encodeJSON(decoded)
}

func encodeJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func decodeObject(raw json.RawMessage) (map[string]any, error) {
	value, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalidEvent
	}
	return object, nil
}

func decodeValue(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decodeOne(decoder)
}

func decodeCanonicalValue(raw []byte) (any, error) {
	return decodeOne(json.NewDecoder(bytes.NewReader(raw)))
}

func decodeOne(decoder *json.Decoder) (any, error) {
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidEvent
	}
	return value, nil
}

func validSource(value any) (EventSourceRef, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return EventSourceRef{}, false
	}
	runtimeID, ok := nonEmptyString(object["runtimeId"])
	if !ok {
		return EventSourceRef{}, false
	}
	source := EventSourceRef{RuntimeID: runtimeID}
	if value, exists := object["agentId"]; exists {
		if source.AgentID, ok = nonEmptyString(value); !ok {
			return EventSourceRef{}, false
		}
	}
	if value, exists := object["toolCallId"]; exists {
		if source.ToolCallID, ok = nonEmptyString(value); !ok {
			return EventSourceRef{}, false
		}
	}
	return source, true
}

func validKnownPayload(eventType string, payload map[string]any) bool {
	switch eventType {
	case "task.created", "task.started":
		return hasString(payload, "title")
	case "task.failed", "task.blocked":
		return hasString(payload, "reason")
	case "scope.proposed", "scope.confirmed":
		return validScope(payload["scope"])
	case "runtime.capabilities.updated":
		return isStringSlice(payload["capabilities"])
	case "control.acquired", "control.transferred":
		return validLease(payload["lease"])
	case "control.released":
		return hasString(payload, "clientId")
	case "approval.requested":
		return validChallenge(payload["challenge"])
	case "approval.resolved":
		decision, ok := nonEmptyString(payload["decision"])
		return hasString(payload, "challengeId") && ok && (decision == "allow_once" || decision == "deny")
	case "question.requested":
		return hasString(payload, "questionId") && hasString(payload, "prompt")
	case "question.resolved":
		return hasString(payload, "questionId") && hasString(payload, "answer")
	case "agent.started":
		return validAgent(payload["agent"])
	case "agent.progressed":
		return hasString(payload, "agentId") && isNumber(payload["progress"])
	case "agent.completed":
		return hasString(payload, "agentId")
	case "agent.failed":
		return hasString(payload, "agentId") && hasString(payload, "reason")
	case "tool.started":
		return hasString(payload, "callId") && hasString(payload, "name")
	case "tool.completed":
		_, success := payload["success"].(bool)
		return hasString(payload, "callId") && success && isStringSlice(payload["evidenceIds"])
	case "tool.failed":
		return hasString(payload, "callId") && hasString(payload, "reason")
	case "evidence.committed":
		return validEvidence(payload["evidence"])
	case "finding.created":
		return validFinding(payload["finding"])
	case "finding.verifying", "finding.confirmed", "finding.mitigated":
		return hasString(payload, "findingId")
	case "finding.rejected":
		return hasString(payload, "findingId") && hasString(payload, "reason")
	case "report.drafted", "report.edited":
		return validReport(payload["report"])
	case "report.validation.failed":
		return hasString(payload, "reportId") && hasString(payload, "reason")
	case "report.validated":
		return hasString(payload, "reportId")
	case "report.frozen":
		version, ok := safeInteger(payload["version"])
		return hasString(payload, "reportId") && ok && version > 0
	case "report.exported":
		return hasString(payload, "reportId") && hasString(payload, "format")
	case "terminal.opened":
		return exactKeys(payload, []string{"session"}) && validTerminalSession(payload["session"])
	case "terminal.output":
		sequence, sequenceOK := safeInteger(payload["sequence"])
		byteLength, lengthOK := safeInteger(payload["byteLength"])
		data, dataOK := payload["data"].(string)
		decoded, decodeErr := base64.StdEncoding.DecodeString(data)
		return exactKeys(payload, []string{"sessionId", "sequence", "data", "byteLength"}) &&
			hasAuditableString(payload, "sessionId") && sequenceOK && sequence > 0 && lengthOK && byteLength > 0 && byteLength <= 1024*1024 &&
			dataOK && decodeErr == nil && len(decoded) == byteLength
	case "terminal.input.accepted":
		sequence, sequenceOK := safeInteger(payload["sequence"])
		byteLength, lengthOK := safeInteger(payload["byteLength"])
		sha256, digestOK := payload["sha256"].(string)
		return exactKeys(payload, []string{"sessionId", "sequence", "byteLength", "sha256"}) &&
			hasAuditableString(payload, "sessionId") && sequenceOK && sequence > 0 && lengthOK && byteLength > 0 && byteLength <= 1024*1024 &&
			digestOK && validSHA256(sha256)
	case "terminal.resized":
		columns, columnsOK := safeInteger(payload["columns"])
		rows, rowsOK := safeInteger(payload["rows"])
		return exactKeys(payload, []string{"sessionId", "columns", "rows"}) && hasAuditableString(payload, "sessionId") &&
			columnsOK && validTerminalDimension(columns) && rowsOK && validTerminalDimension(rows)
	case "terminal.exited":
		_, exitCodeOK := safeInteger(payload["exitCode"])
		return exactKeys(payload, []string{"sessionId", "exitCode", "reason"}) && hasAuditableString(payload, "sessionId") &&
			exitCodeOK && hasAuditableString(payload, "reason")
	case "editor.draft.opened":
		return exactKeys(payload, []string{"draft"}) && validEditorDraft(payload["draft"])
	case "editor.draft.saved":
		revision, revisionOK := safeInteger(payload["revision"])
		proposedLength, lengthOK := safeInteger(payload["proposedByteLength"])
		baseHash, baseOK := payload["baseSha256"].(string)
		proposedHash, proposedOK := payload["proposedSha256"].(string)
		return exactKeys(payload, []string{"draftId", "revision", "baseSha256", "proposedSha256", "proposedByteLength"}) && hasAuditableString(payload, "draftId") &&
			revisionOK && revision > 0 && baseOK && validSHA256(baseHash) && proposedOK && validSHA256(proposedHash) && lengthOK && validEditorByteLength(proposedLength)
	case "editor.patch.applied":
		baseHash, baseOK := payload["baseSha256"].(string)
		proposedHash, proposedOK := payload["proposedSha256"].(string)
		resultHash, resultOK := payload["resultSha256"].(string)
		revision, revisionOK := safeInteger(payload["revision"])
		return exactKeys(payload, []string{"draftId", "revision", "baseSha256", "proposedSha256", "resultSha256", "reviewer"}) && hasAuditableString(payload, "draftId") && revisionOK && revision > 0 &&
			baseOK && validSHA256(baseHash) && proposedOK && validSHA256(proposedHash) && resultOK && validSHA256(resultHash) && hasAuditableString(payload, "reviewer")
	case "editor.patch.verified":
		revision, revisionOK := safeInteger(payload["revision"])
		_, successOK := payload["success"].(bool)
		return exactKeys(payload, []string{"draftId", "revision", "verificationId", "success", "evidenceIds"}) && hasAuditableString(payload, "draftId") && revisionOK && revision > 0 && hasAuditableString(payload, "verificationId") && successOK && isStringSlice(payload["evidenceIds"])
	case "editor.draft.discarded":
		return exactKeys(payload, []string{"draftId", "reason"}) && hasAuditableString(payload, "draftId") && hasAuditableString(payload, "reason")
	default:
		return len(payload) == 0
	}
}

func validTerminalSession(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || !exactKeys(object, []string{"id", "profileId", "processId", "workingDirectory", "scopeId", "ownerClientId", "leaseRevision", "columns", "rows", "outputLimitBytes"}) {
		return false
	}
	for _, key := range []string{"id", "processId", "workingDirectory", "scopeId", "ownerClientId"} {
		if !hasAuditableString(object, key) {
			return false
		}
	}
	profileID, profileOK := object["profileId"].(string)
	if !profileOK || !validTerminalIdentifier(profileID) {
		return false
	}
	leaseRevision, leaseOK := safeInteger(object["leaseRevision"])
	columns, columnsOK := safeInteger(object["columns"])
	rows, rowsOK := safeInteger(object["rows"])
	outputLimit, outputOK := safeInteger(object["outputLimitBytes"])
	return leaseOK && leaseRevision > 0 && columnsOK && validTerminalDimension(columns) && rowsOK && validTerminalDimension(rows) &&
		outputOK && outputLimit > 0 && outputLimit <= 64*1024*1024
}

func validTerminalDimension(value int) bool { return value > 0 && value <= 1000 }

func validEditorByteLength(value int) bool { return value >= 0 && value <= 64*1024*1024 }

func validEditorDraft(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || !exactKeys(object, []string{"id", "path", "scopeId", "ownerClientId", "leaseRevision", "baseSha256", "baseByteLength", "encoding", "evidenceReferences"}) {
		return false
	}
	for _, key := range []string{"id", "path", "scopeId", "ownerClientId"} {
		if !hasAuditableString(object, key) {
			return false
		}
	}
	leaseRevision, leaseOK := safeInteger(object["leaseRevision"])
	baseLength, lengthOK := safeInteger(object["baseByteLength"])
	baseHash, hashOK := object["baseSha256"].(string)
	references, referencesOK := object["evidenceReferences"].([]any)
	if !leaseOK || leaseRevision <= 0 || !lengthOK || !validEditorByteLength(baseLength) || !hashOK || !validSHA256(baseHash) || object["encoding"] != "utf-8" || !referencesOK {
		return false
	}
	for _, reference := range references {
		if !validEditorReference(reference) {
			return false
		}
	}
	return true
}

func validEditorReference(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || !exactKeys(object, []string{"findingId", "evidenceId", "startLine", "endLine"}) || !hasAuditableString(object, "findingId") || !hasAuditableString(object, "evidenceId") {
		return false
	}
	startLine, startOK := safeInteger(object["startLine"])
	endLine, endOK := safeInteger(object["endLine"])
	return startOK && startLine > 0 && endOK && endLine >= startLine
}

func validTerminalIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func exactKeys(object map[string]any, keys []string) bool {
	if len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	return true
}

func hasAuditableString(object map[string]any, key string) bool {
	value, ok := nonEmptyString(object[key])
	if !ok {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}

func validScope(value any) bool {
	object, ok := value.(map[string]any)
	return ok && hasString(object, "id") && hasString(object, "principal") &&
		hasString(object, "workspace") && hasString(object, "validity") &&
		isStringSlice(object["targets"]) && isStringSlice(object["allowedActions"]) &&
		isStringSlice(object["deniedActions"]) && hasString(object, "riskCeiling")
}

func validAgent(value any) bool {
	object, ok := value.(map[string]any)
	return ok && hasString(object, "id") && hasString(object, "name") && hasString(object, "status")
}

func validEvidence(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || !hasString(object, "id") || !hasString(object, "taskId") ||
		!hasString(object, "kind") || !hasString(object, "summary") {
		return false
	}
	_, ok = object["data"].(map[string]any)
	return ok
}

func validFinding(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || !hasString(object, "id") || !hasString(object, "title") ||
		!hasString(object, "severity") || !hasString(object, "confidence") ||
		!isStringSlice(object["evidenceIds"]) {
		return false
	}
	status, ok := nonEmptyString(object["status"])
	return ok && (status == "candidate" || status == "verifying" || status == "confirmed" || status == "rejected" || status == "mitigated")
}

func validLease(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || !hasString(object, "clientId") {
		return false
	}
	revision, ok := safeInteger(object["revision"])
	return ok && revision > 0
}

func validChallenge(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"id", "agentId", "action", "target", "parameterDigest", "risk", "expiresAt"} {
		if !hasString(object, key) {
			return false
		}
	}
	expiresAt, _ := nonEmptyString(object["expiresAt"])
	return validTimestamp(expiresAt)
}

func validReport(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || !hasString(object, "id") || !hasString(object, "taskId") {
		return false
	}
	version, ok := safeInteger(object["version"])
	status, statusOK := nonEmptyString(object["status"])
	_, narrative := object["narrative"].(string)
	_, recommendations := object["recommendations"].(string)
	_, humanNotes := object["humanNotes"].(string)
	_, findings := object["findings"].([]any)
	return ok && version >= 0 && statusOK && (status == "draft" || status == "frozen") &&
		narrative && recommendations && humanNotes && findings
}

func requiredString(raw any) (string, bool) {
	if message, ok := raw.(json.RawMessage); ok {
		var value string
		if json.Unmarshal(message, &value) != nil || value == "" {
			return "", false
		}
		return value, true
	}
	return nonEmptyString(raw)
}

func nonEmptyString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok && text != ""
}

func hasString(object map[string]any, key string) bool {
	_, ok := nonEmptyString(object[key])
	return ok
}

func isStringSlice(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if _, ok := nonEmptyString(item); !ok {
			return false
		}
	}
	return true
}

func isNumber(value any) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	parsed, err := number.Float64()
	return err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed)
}

func validJSONValue(value any) bool {
	switch typed := value.(type) {
	case nil, string, bool:
		return true
	case json.Number:
		return isNumber(typed)
	case []any:
		for _, item := range typed {
			if !validJSONValue(item) {
				return false
			}
		}
		return true
	case map[string]any:
		for _, item := range typed {
			if !validJSONValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func safeInteger(value any) (int, bool) {
	if raw, ok := value.(json.RawMessage); ok {
		decoded, err := decodeValue(raw)
		if err != nil {
			return 0, false
		}
		value = decoded
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(string(number), 64)
	if err != nil || math.Trunc(parsed) != parsed || math.Abs(parsed) > maxSafeJSONInteger {
		return 0, false
	}
	return int(parsed), true
}

func validTimestamp(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}
