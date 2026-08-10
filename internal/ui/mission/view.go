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
	if model.panel == panelInspector {
		return model.renderInspectorPanel()
	}

	contentWidth := max(1, model.width-2)
	inputLines := fitLines(strings.Split(model.renderInput(), "\n"), contentWidth)
	footer := append([]string{fitLine(model.renderConnection(), contentWidth)}, inputLines...)
	bodyHeight := max(1, model.height-2-2-len(footer))
	body := model.renderResponsiveBody(contentWidth, bodyHeight)
	lines := []string{fitLine(model.renderHeader(), contentWidth), fitLine(model.renderControls(), contentWidth)}
	lines = append(lines, body...)
	lines = append(lines, footer...)
	return model.renderFrame(lines)
}

func (model *Model) renderInput() string {
	safeInput := *model.input
	safeInput.SetValue(clean(model.input.Value))
	return safeInput.View()
}

func (model *Model) renderResponsiveBody(width, height int) []string {
	stream := fitSection(strings.Split(model.renderStream(), "\n"), width, height)
	if model.width < 110 {
		return padLines(stream, height)
	}
	mainWidth := max(1, width*2/3)
	inspectorWidth := max(1, width-mainWidth-1)
	stream = fitSection(strings.Split(model.renderStream(), "\n"), mainWidth, height)
	inspector := fitSection(strings.Split(model.renderInspector(), "\n"), inspectorWidth, height)
	lines := make([]string, height)
	for index := range lines {
		left, right := "", ""
		if index < len(stream) {
			left = stream[index]
		}
		if index < len(inspector) {
			right = inspector[index]
		}
		lines[index] = padLine(left, mainWidth) + dimStyle.Render("│") + fitLine(right, inspectorWidth)
	}
	return lines
}

func padLines(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func (model *Model) renderControls() string {
	pause := "Pause"
	if model.state.Task != nil && model.state.Task.Status == "paused" {
		pause = "Resume"
	}
	if model.width < 100 {
		return "[F5]" + pause + " [F8]Cancel [F4]Scope [F6]Take [F2]Inspect [^T]Agents"
	}
	return "[F5] " + pause + "  [F8] Cancel  [F4] Scope  [F6] Takeover  [F2] Inspector  [Ctrl+T] Agents"
}

func (model *Model) renderHeader() string {
	parts := []string{brandStyle.Render("CYBER")}
	if model.demo {
		parts = append(parts, warningStyle.Render("DEMO"))
	}
	if model.sourceMode != "" && !(model.demo && model.sourceMode == "demo") {
		parts = append(parts, strings.ToUpper(clean(model.sourceMode)))
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
	lines = append(lines, "", sectionStyle.Render("Asset Graph"))
	lines = append(lines, fmt.Sprintf("nodes: %d  edges: %d", len(model.state.AssetNodes), len(model.state.AssetEdges)))
	for _, id := range sortedAssetNodeIDs(model.state.AssetNodes) {
		node := model.state.AssetNodes[id]
		label := node.Label
		if strings.TrimSpace(label) == "" {
			label = node.ID
		}
		lines = append(lines, fmt.Sprintf("%s  %s · %s", clean(label), strings.ToUpper(clean(node.Kind)), strings.ToUpper(clean(node.Status))))
	}
	for _, id := range sortedAssetEdgeIDs(model.state.AssetEdges) {
		edge := model.state.AssetEdges[id]
		arrow := "->"
		if !edge.Directed {
			arrow = "--"
		}
		lines = append(lines, fmt.Sprintf("  %s %s %s", clean(edge.SourceID), arrow, clean(edge.TargetID)))
	}
	lines = append(lines, "", sectionStyle.Render("Finding"))
	for _, id := range sortedFindingIDs(model.state.Findings) {
		finding := model.state.Findings[id]
		lines = append(lines,
			clean(finding.Title),
			fmt.Sprintf("%s · %s · %s", strings.ToUpper(clean(finding.Severity)), strings.ToUpper(clean(finding.Status)), confidenceLabel(finding.Confidence)),
		)
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
	if status == "LIVE" || status == "CONNECTED" || status == "HEALTHY" {
		style = liveStyle
	} else if status == "OFFLINE" || status == "DISCONNECTED" {
		style = dangerStyle
	}
	source := clean(model.runtime)
	if model.sourceMode != "" {
		source = strings.ToUpper(clean(model.sourceMode)) + " · " + source
	}
	line := style.Render(status) + "  " + source + " runtime"
	if model.authority != "" || model.runtimeVersion != "" || model.sessionID != "" {
		identity := "Security Runtime - " + clean(model.runtime)
		if model.runtimeVersion != "" {
			identity += " " + clean(model.runtimeVersion)
		}
		location := model.runtimeLocation
		if location == "" {
			location = model.sourceMode
		}
		if location != "" {
			location = strings.ToUpper(location[:1]) + location[1:]
			identity += "  " + clean(location) + " - " + titleStatus(status)
		}
		if model.sessionID != "" {
			identity += "  Session: " + clean(model.sessionID)
		}
		if model.authority != "" {
			identity += "  Authority: " + clean(model.authority)
		}
		line = identity
	}
	if detail := clean(model.connectionDetail); detail != "" {
		line += "  " + detail
	}
	return line + "  Ctrl+T Agents  PgUp/PgDn Scroll"
}

func titleStatus(status string) string {
	status = strings.ToLower(status)
	if status == "" {
		return "Unknown"
	}
	return strings.ToUpper(status[:1]) + status[1:]
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
	return model.renderFrame(lines)
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
	return model.renderFrame(lines)
}

func (model *Model) renderInspectorPanel() string {
	lines := []string{
		brandStyle.Render("CYBER / INSPECTOR"),
		dimStyle.Render("F2 or Esc Parent"),
		"",
	}
	lines = append(lines, strings.Split(model.renderInspector(), "\n")...)
	return model.renderFrame(lines)
}

func (model *Model) renderFrame(lines []string) string {
	width, height := max(1, model.width-2), max(1, model.height-2)
	lines = padLines(fitSection(lines, width, height), height)
	view := frameStyle.Width(width).Render(strings.Join(lines, "\n"))
	view = constrainView(view, model.width, model.height)
	if model.noColor {
		return ansi.Strip(view)
	}
	return view
}

func fitSection(lines []string, width, height int) []string {
	lines = fitLines(lines, width)
	if len(lines) <= height {
		return lines
	}
	if height <= 1 {
		return lines[:height]
	}
	result := make([]string, 0, height)
	result = append(result, lines[0])
	result = append(result, lines[len(lines)-(height-1):]...)
	return result
}

func fitLines(lines []string, width int) []string {
	result := make([]string, len(lines))
	for index, line := range lines {
		result[index] = fitLine(line, width)
	}
	return result
}

func fitLine(line string, width int) string {
	return ansi.Truncate(line, max(0, width), "")
}

func padLine(line string, width int) string {
	line = fitLine(line, width)
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}

func constrainView(view string, width, height int) string {
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(fitLines(lines, width), "\n")
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

func sortedAssetNodeIDs(values map[string]productprotocol.AssetNodeState) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedAssetEdgeIDs(values map[string]productprotocol.AssetEdgeState) []string {
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
