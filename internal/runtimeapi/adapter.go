package runtimeapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"cyber-code/internal/core"
	"cyber-code/internal/productprotocol"
)

// Adapter projects only structured core lifecycle signals. Text and thinking
// deltas are intentionally excluded from the authority event stream.
type Adapter struct {
	runtimeID string
	taskID    string
	mu        sync.Mutex
	tools     map[string]string
}

func NewAdapter(runtimeID, taskID string) *Adapter {
	return &Adapter{runtimeID: runtimeID, taskID: taskID, tools: make(map[string]string)}
}

func (a *Adapter) Project(ctx context.Context, service *Service, event core.Event) ([]productprotocol.Event, error) {
	if service == nil {
		return nil, fmt.Errorf("runtime service is required")
	}
	drafts := a.Adapt(event)
	projected := make([]productprotocol.Event, 0, len(drafts))
	for _, draft := range drafts {
		committed, err := service.Emit(ctx, draft)
		if err != nil {
			return projected, err
		}
		projected = append(projected, committed)
	}
	return projected, nil
}

func (a *Adapter) Adapt(event core.Event) []DraftEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	source := productprotocol.EventSourceRef{RuntimeID: a.runtimeID}
	draft := DraftEvent{TaskID: a.taskID, Source: source}
	switch event.Type {
	case core.EventToolCall:
		if event.ToolCall == nil {
			return nil
		}
		a.tools[event.ToolCall.ID] = event.ToolCall.Name
		draft.Type = "tool.started"
		draft.Source.ToolCallID = event.ToolCall.ID
		draft.Payload = map[string]any{"callId": event.ToolCall.ID, "name": event.ToolCall.Name}
	case core.EventToolResult:
		callID := event.ToolCallID
		if event.ToolResult != nil && event.ToolResult.ToolCallID != "" {
			callID = event.ToolResult.ToolCallID
		}
		if callID == "" || event.ToolResult == nil {
			return nil
		}
		draft.Source.ToolCallID = callID
		toolName := a.tools[callID]
		var terminalEvidence *DraftEvent
		var terminalEvidenceID string
		if toolName == "shell" {
			output := toolResultText(event.ToolResult)
			digest := sha256.Sum256([]byte(a.taskID + "\x00" + callID + "\x00" + output))
			terminalEvidenceID = "evidence-terminal-" + hex.EncodeToString(digest[:12])
			evidence := productprotocol.ImmutableEvidence{
				ID: terminalEvidenceID, TaskID: a.taskID, Kind: "terminal", Summary: terminalSummary(event.ToolResult.IsError),
				Data: map[string]any{"toolCallId": callID, "output": output, "isError": event.ToolResult.IsError},
			}
			candidate := DraftEvent{TaskID: a.taskID, Type: "evidence.committed", Source: draft.Source, Payload: map[string]any{"evidence": evidence}}
			terminalEvidence = &candidate
		}
		if event.ToolResult.IsError {
			draft.Type = "tool.failed"
			reason := toolResultText(event.ToolResult)
			if strings.TrimSpace(reason) == "" {
				reason = "tool execution failed"
			}
			draft.Payload = map[string]any{"callId": callID, "reason": reason}
		} else {
			draft.Type = "tool.completed"
			evidenceIDs := []string{}
			if terminalEvidenceID != "" {
				evidenceIDs = append(evidenceIDs, terminalEvidenceID)
			}
			draft.Payload = map[string]any{"callId": callID, "success": true, "evidenceIds": evidenceIDs}
		}
		delete(a.tools, callID)
		if terminalEvidence != nil {
			return []DraftEvent{*terminalEvidence, draft}
		}
	case core.EventSubagentStarted:
		if event.Subagent == nil || event.Subagent.TaskID == "" {
			return nil
		}
		draft.Type = "agent.started"
		draft.Source.AgentID = event.Subagent.TaskID
		name := event.Subagent.Agent
		if name == "" {
			name = "Subagent"
		}
		draft.Payload = map[string]any{"agent": productprotocol.AgentState{ID: event.Subagent.TaskID, Name: name, Status: "running", CurrentAction: event.Subagent.Description}}
	case core.EventSubagentStatus:
		if event.Subagent == nil || event.Subagent.TaskID == "" {
			return nil
		}
		draft.Source.AgentID = event.Subagent.TaskID
		switch event.Subagent.Status {
		case "running", "in_progress":
			draft.Type = "agent.progressed"
			draft.Payload = map[string]any{"agentId": event.Subagent.TaskID, "progress": 0.5, "currentAction": event.Subagent.RecentTool}
		case "completed":
			draft.Type = "agent.completed"
			draft.Payload = map[string]any{"agentId": event.Subagent.TaskID}
		case "failed", "cancelled":
			draft.Type = "agent.failed"
			reason := event.Subagent.Description
			if reason == "" {
				reason = event.Subagent.Status
			}
			draft.Payload = map[string]any{"agentId": event.Subagent.TaskID, "reason": reason}
		default:
			return nil
		}
	case core.EventCompleted:
		draft.Type = "task.completed"
		draft.Payload = map[string]any{}
	case core.EventError:
		draft.Type = "task.failed"
		reason := "runtime failed"
		if event.Err != nil {
			reason = event.Err.UserMessage()
			if reason == "" {
				reason = fmt.Sprint(event.Err)
			}
		}
		draft.Payload = map[string]any{"reason": reason}
	default:
		return nil
	}
	return []DraftEvent{draft}
}

func toolResultText(result *core.ToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content))
	for _, block := range result.Content {
		if block.Type == core.ContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func terminalSummary(failed bool) string {
	if failed {
		return "Terminal command failed"
	}
	return "Terminal command completed"
}
