package mission

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"cyber-code/internal/productprotocol"
)

func (model *Model) View() string {
	if model.panel == panelTasks {
		return model.renderAgentTasks()
	}
	if model.panel == panelAgent {
		return model.renderAgentDetail()
	}

	header := model.renderHeader()
	stream := model.renderStream()
	inspector := model.renderInspector()
	mainWidth := max(40, model.width*2/3)
	inspectorWidth := max(24, model.width-mainWidth-1)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(mainWidth).Render(stream),
		inspectorStyle.Width(inspectorWidth).Render(inspector),
	)
	footer := model.renderConnection() + "\n" + model.renderInput()
	return frameStyle.Width(max(20, model.width-2)).Render(header + "\n" + body + "\n" + footer)
}

func (model *Model) renderInput() string {
	safeInput := *model.input
	safeInput.SetValue(clean(model.input.Value))
	return safeInput.View()
}

func (model *Model) renderHeader() string {
	parts := []string{brandStyle.Render("CYBER")}
	if model.demo {
		parts = append(parts, warningStyle.Render("DEMO"))
	}
	parts = append(parts, strings.ToUpper(clean(model.runtime)))
	if model.state.Task == nil {
		parts = append(parts, "NO ACTIVE TASK")
	} else {
		parts = append(parts,
			clean(model.state.Task.Title),
			statusStyle(model.state.Task.Status).Render(strings.ToUpper(clean(model.state.Task.Status))),
			fmt.Sprintf("cursor %d", model.state.CommittedCursor),
		)
	}
	active := 0
	for _, agent := range model.state.Agents {
		if agent.Status == "running" {
			active++
		}
	}
	parts = append(parts, fmt.Sprintf("Agents %d/%d active", active, len(model.state.Agents)))
	return strings.Join(parts, "  ")
}

func (model *Model) renderStream() string {
	lines := []string{sectionStyle.Render("NARRATIVE STREAM")}
	if len(model.state.Timeline) == 0 {
		lines = append(lines, dimStyle.Render("No committed events"))
	}
	for _, event := range model.state.Timeline {
		lines = append(lines, fmt.Sprintf("%s  %s", eventClock(event.OccurredAt), clean(eventNarrative(event))))
	}
	if approvalID := model.pendingApprovalID(); approvalID != "" {
		lines = append(lines, "", model.renderApproval(model.state.Approvals[approvalID]))
	}
	return strings.Join(lines, "\n")
}

func eventNarrative(event productprotocol.Event) string {
	switch event.Type {
	case "tool.started":
		var payload struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Name != "" {
			return "TOOL  " + payload.Name
		}
	case "tool.completed":
		var payload struct {
			CallID string `json:"callId"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.CallID != "" {
			return "TOOL  " + payload.CallID + " completed"
		}
	case "tool.failed":
		var payload struct {
			CallID string `json:"callId"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil {
			return "TOOL FAILED  " + strings.TrimSpace(payload.CallID+" "+payload.Reason)
		}
	case "evidence.committed":
		var payload struct {
			Evidence productprotocol.ImmutableEvidence `json:"evidence"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Evidence.Summary != "" {
			return "EVIDENCE  " + payload.Evidence.Summary
		}
	case "finding.created":
		var payload struct {
			Finding productprotocol.FindingState `json:"finding"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Finding.Title != "" {
			return "FINDING  " + payload.Finding.Title + " · " + strings.ToUpper(payload.Finding.Severity)
		}
	case "approval.requested":
		var payload struct {
			Challenge productprotocol.ApprovalChallenge `json:"challenge"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Challenge.ID != "" {
			return "APPROVAL  " + strings.TrimSpace(payload.Challenge.Action+" "+payload.Challenge.Target)
		}
	case "agent.started":
		var payload struct {
			Agent productprotocol.AgentState `json:"agent"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Agent.Name != "" {
			return "AGENT  " + payload.Agent.Name + " started"
		}
	case "task.created", "task.started":
		var payload struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Title != "" {
			return strings.ToUpper(strings.TrimPrefix(event.Type, "task.")) + "  " + payload.Title
		}
	}
	return event.Type
}

func (model *Model) renderInspector() string {
	lines := []string{sectionStyle.Render("AGENTS · SCOPE · EVIDENCE")}
	for _, id := range model.agentIDs() {
		agent := model.state.Agents[id]
		progress := ""
		if agent.Progress != nil {
			progress = fmt.Sprintf(" %.0f%%", *agent.Progress)
		}
		lines = append(lines, fmt.Sprintf("%s  %s%s", clean(agent.Name), strings.ToUpper(clean(agent.Status)), progress))
		if agent.CurrentAction != "" {
			lines = append(lines, "  "+dimStyle.Render(clean(agent.CurrentAction)))
		}
	}
	lines = append(lines, "", sectionStyle.Render("Scope"))
	if model.state.Scope == nil {
		lines = append(lines, dimStyle.Render("Not confirmed"))
	} else {
		lines = append(lines,
			"workspace: "+clean(model.state.Scope.Workspace),
			"targets: "+clean(strings.Join(model.state.Scope.Targets, ", ")),
			"risk ceiling: "+strings.ToUpper(clean(model.state.Scope.RiskCeiling)),
		)
	}
	lines = append(lines, "", sectionStyle.Render("Evidence"))
	for _, id := range sortedEvidenceIDs(model.state.Evidence) {
		evidence := model.state.Evidence[id]
		lines = append(lines, fmt.Sprintf("%s  %s", clean(evidence.ID), clean(evidence.Summary)))
	}
	lines = append(lines, "", sectionStyle.Render("Finding"))
	for _, id := range sortedFindingIDs(model.state.Findings) {
		finding := model.state.Findings[id]
		lines = append(lines, fmt.Sprintf("%s  %s · %s · %s",
			clean(finding.Title), strings.ToUpper(clean(finding.Severity)), strings.ToUpper(clean(finding.Status)), confidenceLabel(finding.Confidence)))
	}
	lines = append(lines, "", sectionStyle.Render("Report"))
	if model.state.Report == nil {
		lines = append(lines, dimStyle.Render("NOT STARTED"))
	} else {
		lines = append(lines, strings.ToUpper(clean(model.state.Report.Status)))
	}
	return strings.Join(lines, "\n")
}

func (model *Model) renderApproval(approval productprotocol.ApprovalState) string {
	controls := "[Tab] Focus  Review / Deny"
	if model.approvalFocused {
		controls = "[R] Review  [D] Deny  [Esc] Composer"
	}
	if model.reviewedApprovalID == approval.ID {
		controls = "[C] Confirm allow once  [D] Deny  [Esc] Composer"
	}
	content := strings.Join([]string{
		warningStyle.Render("APPROVAL REQUIRED"),
		"Agent: " + clean(approval.AgentID),
		"Action: " + clean(strings.TrimSpace(approval.Action+" "+approval.Target)),
		"Risk: " + strings.ToUpper(clean(approval.Risk)),
		controls,
	}, "\n")
	return approvalStyle.Render(content)
}

func (model *Model) renderConnection() string {
	status := strings.ToUpper(clean(model.connection))
	style := dimStyle
	if status == "LIVE" || status == "CONNECTED" {
		style = liveStyle
	} else if status == "OFFLINE" || status == "DISCONNECTED" {
		style = dangerStyle
	}
	line := style.Render(status) + "  " + clean(model.runtime) + " runtime"
	if detail := clean(model.connectionDetail); detail != "" {
		line += "  " + detail
	}
	return line + "  Ctrl+T Agents  PgUp/PgDn Scroll"
}

func (model *Model) renderAgentTasks() string {
	lines := []string{brandStyle.Render("CYBER / AGENT TASKS"), dimStyle.Render("Ctrl+T or Esc Parent  Enter Detail  Up/Down Select"), ""}
	ids := model.agentIDs()
	if len(ids) == 0 {
		lines = append(lines, "No agents")
	}
	for index, id := range ids {
		agent := model.state.Agents[id]
		prefix := "  "
		if index == model.selectedAgent {
			prefix = selectedStyle.Render("> ")
		}
		progress := ""
		if agent.Progress != nil {
			progress = fmt.Sprintf("  %.0f%%", *agent.Progress)
		}
		lines = append(lines, fmt.Sprintf("%s%s  %s%s  %s", prefix, clean(agent.Name), strings.ToUpper(clean(agent.Status)), progress, clean(agent.CurrentAction)))
	}
	return frameStyle.Width(max(20, model.width-2)).Render(strings.Join(lines, "\n"))
}

func (model *Model) renderAgentDetail() string {
	agent, exists := model.state.Agents[model.activeAgentID]
	if !exists {
		return model.renderAgentTasks()
	}
	progress := "not reported"
	if agent.Progress != nil {
		progress = fmt.Sprintf("%.0f%%", *agent.Progress)
	}
	lines := []string{
		brandStyle.Render("CYBER / AGENT DETAIL"),
		dimStyle.Render("[/] Switch Agent  Esc Parent  Ctrl+T Close"), "",
		"Agent: " + clean(agent.Name),
		"ID: " + clean(agent.ID),
		"Status: " + strings.ToUpper(clean(agent.Status)),
		"Progress: " + progress,
		"Current action: " + clean(agent.CurrentAction),
	}
	return frameStyle.Width(max(20, model.width-2)).Render(strings.Join(lines, "\n"))
}

func (model *Model) pendingApprovalID() string {
	ids := make([]string, 0, len(model.state.Approvals))
	for id, approval := range model.state.Approvals {
		if approval.Decision == "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func sortedEvidenceIDs(values map[string]productprotocol.ImmutableEvidence) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedFindingIDs(values map[string]productprotocol.FindingState) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func confidenceLabel(value string) string {
	switch strings.ToLower(clean(value)) {
	case "high":
		return "●●● HIGH"
	case "medium":
		return "●●○ MEDIUM"
	default:
		return "●○○ LOW"
	}
}

func statusStyle(status string) lipgloss.Style {
	switch strings.ToLower(status) {
	case "running", "completed":
		return liveStyle
	case "failed", "blocked", "cancelled":
		return dangerStyle
	default:
		return warningStyle
	}
}

func eventClock(value string) string {
	value = clean(value)
	if index := strings.Index(value, "T"); index >= 0 && len(value) >= index+6 {
		return value[index+1 : index+6]
	}
	return value
}

func clean(value string) string {
	value = ansi.Strip(value)
	var builder strings.Builder
	for _, current := range value {
		if isBidiControl(current) {
			continue
		}
		if current == '\t' {
			builder.WriteByte(' ')
			continue
		}
		if current == '\n' || !unicode.IsControl(current) {
			builder.WriteRune(current)
		}
	}
	return builder.String()
}

func isBidiControl(value rune) bool {
	return value == '\u200e' || value == '\u200f' ||
		(value >= '\u202a' && value <= '\u202e') ||
		(value >= '\u2066' && value <= '\u2069')
}
