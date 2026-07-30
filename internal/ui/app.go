package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/product"
	"cyber-code/internal/tool/builtin"
	"cyber-code/internal/ui/components"
)

type Runner interface {
	Run(context.Context, string) <-chan core.Event
}

// Observer exposes independent background events, such as sub-agent turns.
type Observer interface {
	Observe(context.Context) <-chan core.Event
}

type ModelOptions struct {
	Width         int
	Height        int
	InitialPrompt string
	VimMode       bool
	Workspace     string
	CommandNames  []string
}

type Message struct {
	Role    string
	Content string
}

type ToolPresentation struct {
	ID       string
	Name     string
	Status   string
	Input    map[string]interface{}
	Output   string
	FilePath string
	IsError  bool
}

type Model struct {
	runner   Runner
	observer Observer
	ctx      context.Context
	cancel   context.CancelFunc

	Messages        []Message
	Input           *components.InputModel
	Processing      bool
	StatusText      string
	Err             error
	Width           int
	Height          int
	Ready           bool
	Permission      *components.PermissionDialog
	QuestionSelect  *components.SelectDialog
	QuestionInput   *components.InputDialog
	Tools           []ToolPresentation
	Usage           core.Usage
	ProcessingView  *components.ProcessingModel
	ScrollOffset    int
	Completions     []string
	CompletionIndex int

	events          <-chan core.Event
	turnCancel      context.CancelFunc
	assistantIndex  int
	permissionReply chan<- permissions.Decision
	questionReply   chan<- QuestionAnswer
	initialPrompt   string
	toolIndexes     map[string]int
	workspace       string
	commandNames    []string
	observation     <-chan core.Event
	subagents       map[string]*SubagentView
	activeSubagent  string
	showTaskList    bool
	taskList        *components.TaskListModel
}

type turnStartedMsg struct{ events <-chan core.Event }
type runtimeEventMsg struct {
	event core.Event
	open  bool
}
type observationStartedMsg struct{ events <-chan core.Event }
type observationEventMsg struct {
	event core.Event
	open  bool
}
type permissionRespondedMsg struct{}
type processingTickMsg struct{}
type setVimModeMsg struct{ enabled bool }
type clearConversationMsg struct{}

const processingTickInterval = 100 * time.Millisecond

type PermissionRequestMsg struct {
	Request permissions.Request
	Respond chan<- permissions.Decision
}

type QuestionAnswer struct {
	Value string
	Err   error
}

type QuestionRequestMsg struct {
	Question builtin.Question
	Respond  chan<- QuestionAnswer
}

type PermissionBridge struct {
	mu   sync.RWMutex
	send func(tea.Msg)
}

type QuestionBridge struct {
	mu   sync.RWMutex
	send func(tea.Msg)
}

// ControlBridge sends control-plane UI state changes to the active Bubble Tea
// program without exposing the Model to command handlers.
type ControlBridge struct {
	mu   sync.RWMutex
	send func(tea.Msg)
}

func NewControlBridge() *ControlBridge { return &ControlBridge{} }

func (bridge *ControlBridge) Attach(send func(tea.Msg)) {
	bridge.mu.Lock()
	bridge.send = send
	bridge.mu.Unlock()
}

func (bridge *ControlBridge) Detach() { bridge.Attach(nil) }

func (bridge *ControlBridge) SetVimMode(enabled bool) error {
	if bridge == nil {
		return fmt.Errorf("UI control is unavailable")
	}
	bridge.mu.RLock()
	send := bridge.send
	bridge.mu.RUnlock()
	if send == nil {
		return fmt.Errorf("UI control is unavailable")
	}
	send(setVimModeMsg{enabled: enabled})
	return nil
}

func (bridge *ControlBridge) ClearConversation() error {
	if bridge == nil {
		return fmt.Errorf("UI control is unavailable")
	}
	bridge.mu.RLock()
	send := bridge.send
	bridge.mu.RUnlock()
	if send == nil {
		return fmt.Errorf("UI control is unavailable")
	}
	send(clearConversationMsg{})
	return nil
}

func NewQuestionBridge() *QuestionBridge { return &QuestionBridge{} }

func (bridge *QuestionBridge) Attach(send func(tea.Msg)) {
	bridge.mu.Lock()
	bridge.send = send
	bridge.mu.Unlock()
}

func (bridge *QuestionBridge) Detach() { bridge.Attach(nil) }

func (bridge *QuestionBridge) Ask(ctx context.Context, question builtin.Question) (string, error) {
	bridge.mu.RLock()
	send := bridge.send
	bridge.mu.RUnlock()
	if send == nil {
		return "", builtin.ErrInteractiveInputUnavailable
	}
	reply := make(chan QuestionAnswer, 1)
	send(QuestionRequestMsg{Question: question, Respond: reply})
	select {
	case answer := <-reply:
		return answer.Value, answer.Err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func NewPermissionBridge() *PermissionBridge { return &PermissionBridge{} }

func (bridge *PermissionBridge) Attach(send func(tea.Msg)) {
	bridge.mu.Lock()
	bridge.send = send
	bridge.mu.Unlock()
}

func (bridge *PermissionBridge) Detach() {
	bridge.Attach(nil)
}

func (bridge *PermissionBridge) Attached() bool {
	if bridge == nil {
		return false
	}
	bridge.mu.RLock()
	defer bridge.mu.RUnlock()
	return bridge.send != nil
}

func (bridge *PermissionBridge) Confirm(ctx context.Context, request permissions.Request) (permissions.Decision, error) {
	bridge.mu.RLock()
	send := bridge.send
	bridge.mu.RUnlock()
	return NewPermissionConfirmer(send)(ctx, request)
}

func NewModel(runner Runner, options ModelOptions) *Model {
	if options.Width <= 0 {
		options.Width = 80
	}
	if options.Height <= 0 {
		options.Height = 24
	}
	if strings.TrimSpace(options.Workspace) == "" {
		options.Workspace, _ = os.Getwd()
	}
	ctx, cancel := context.WithCancel(context.Background())
	input := components.NewInput(">", "Type your message...", options.Width)
	input.SetVimEnabled(options.VimMode)
	return &Model{
		runner: runner, observer: observerForRunner(runner), ctx: ctx, cancel: cancel, Messages: []Message{}, Input: input, Width: options.Width, Height: options.Height,
		Ready: true, assistantIndex: -1, initialPrompt: options.InitialPrompt,
		toolIndexes: make(map[string]int), subagents: make(map[string]*SubagentView), workspace: options.Workspace,
		commandNames:   append([]string(nil), options.CommandNames...),
		ProcessingView: components.NewProcessingIndicator("Working"),
		taskList:       components.NewTaskList(),
	}
}

func InitialModel() Model { return *NewModel(nil, ModelOptions{}) }

func (model *Model) Init() tea.Cmd {
	observation := startObservation(model.observer, model.ctx)
	if strings.TrimSpace(model.initialPrompt) == "" {
		return observation
	}
	model.Input.SetValue(model.initialPrompt)
	model.initialPrompt = ""
	enter := func() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
	if observation == nil {
		return enter
	}
	return tea.Batch(observation, enter)
}

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.Width, model.Height, model.Ready = message.Width, message.Height, true
		model.Input.Width = max(20, message.Width)
		if model.Permission != nil {
			model.Permission.Width = max(24, min(60, message.Width-4))
		}
	case tea.KeyMsg:
		if model.QuestionSelect != nil || model.QuestionInput != nil {
			return model.updateQuestion(message)
		}
		if model.Permission != nil {
			return model.updatePermission(message)
		}
		if model.handleSubagentNavigation(message) {
			return model, nil
		}
		switch message.Type {
		case tea.KeyCtrlC:
			if model.Processing && model.turnCancel != nil {
				model.turnCancel()
				model.StatusText = "Canceling"
				return model, nil
			}
			model.cancel()
			return model, tea.Quit
		case tea.KeyPgUp:
			model.scrollBy(model.pageSize())
			return model, nil
		case tea.KeyPgDown:
			model.scrollBy(-model.pageSize())
			return model, nil
		case tea.KeyUp:
			if len(model.Completions) > 0 {
				model.CompletionIndex = (model.CompletionIndex - 1 + len(model.Completions)) % len(model.Completions)
				return model, nil
			}
			return model, model.Input.Update(message)
		case tea.KeyDown:
			if len(model.Completions) > 0 {
				model.CompletionIndex = (model.CompletionIndex + 1) % len(model.Completions)
				return model, nil
			}
			return model, model.Input.Update(message)
		case tea.KeyEnter:
			if message.Alt {
				return model, model.Input.Update(message)
			}
			if len(model.Completions) > 0 {
				selected := model.Completions[min(model.CompletionIndex, len(model.Completions)-1)]
				if model.Input.Value != selected {
					model.applyCompletion()
					return model, nil
				}
				model.closeCompletions()
			}
			if isExitCommand(model.Input.Value) {
				model.cancel()
				return model, tea.Quit
			}
			if model.Processing || strings.TrimSpace(model.Input.Value) == "" {
				return model, nil
			}
			prompt := model.Input.Value
			model.Input.Clear()
			model.closeCompletions()
			model.ScrollOffset = 0
			model.Tools = nil
			model.toolIndexes = make(map[string]int)
			model.Messages = append(model.Messages, Message{Role: "user", Content: prompt})
			model.Processing, model.StatusText, model.Err = true, "Working", nil
			turnCtx, cancel := context.WithCancel(model.ctx)
			model.turnCancel = cancel
			return model, startTurn(model.runner, turnCtx, prompt)
		case tea.KeyTab:
			model.applyCompletion()
			return model, nil
		case tea.KeyShiftTab:
			if len(model.Completions) > 0 {
				model.CompletionIndex = (model.CompletionIndex - 1 + len(model.Completions)) % len(model.Completions)
			}
			return model, nil
		case tea.KeyEsc:
			if len(model.Completions) > 0 {
				model.closeCompletions()
				return model, nil
			}
			return model, model.Input.Update(message)
		default:
			command := model.Input.Update(message)
			model.refreshCompletions()
			return model, command
		}
	case tea.MouseMsg:
		switch message.Type {
		case tea.MouseWheelUp:
			model.scrollBy(3)
		case tea.MouseWheelDown:
			model.scrollBy(-3)
		}
		return model, nil
	case turnStartedMsg:
		model.events = message.events
		if message.events == nil {
			model.fail(fmt.Errorf("runtime returned no event stream"))
			return model, nil
		}
		return model, waitForRuntimeEvent(message.events)
	case observationStartedMsg:
		model.observation = message.events
		if message.events == nil {
			return model, nil
		}
		return model, waitForObservationEvent(message.events)
	case observationEventMsg:
		if !message.open {
			model.observation = nil
			return model, nil
		}
		model.applyObservation(message.event)
		return model, waitForObservationEvent(model.observation)
	case runtimeEventMsg:
		if !message.open {
			model.finish()
			return model, nil
		}
		oldLineCount := model.scrollableLineCount()
		terminal := model.applyEvent(message.event)
		if model.ScrollOffset > 0 {
			model.ScrollOffset += max(0, model.scrollableLineCount()-oldLineCount)
		}
		if terminal {
			return model, nil
		}
		return model, waitForRuntimeEvent(model.events)
	case processingTickMsg:
		if !model.Processing {
			return model, nil
		}
		model.ProcessingView.Update()
		return model, waitForRuntimeEvent(model.events)
	case setVimModeMsg:
		model.SetVimMode(message.enabled)
		return model, nil
	case clearConversationMsg:
		model.Messages = nil
		model.Tools = nil
		model.toolIndexes = make(map[string]int)
		model.assistantIndex = -1
		return model, nil
	case PermissionRequestMsg:
		model.Permission = components.NewPermissionDialog(message.Request.Tool, permissionDescription(message.Request))
		model.permissionReply = message.Respond
		return model, nil
	case QuestionRequestMsg:
		model.questionReply = message.Respond
		if len(message.Question.Options) > 0 {
			model.QuestionSelect = components.NewSelectDialog(message.Question.Prompt, message.Question.Options)
		} else {
			model.QuestionInput = components.NewInputDialog("Question", message.Question.Prompt, "Type your answer")
		}
		return model, nil
	case permissionRespondedMsg:
		return model, nil
	}
	return model, nil
}

func isExitCommand(input string) bool {
	switch strings.TrimSpace(input) {
	case "/exit", "/quit":
		return true
	default:
		return false
	}
}

func (model *Model) applyCompletion() {
	if len(model.Completions) > 0 {
		value := model.Completions[min(model.CompletionIndex, len(model.Completions)-1)]
		if !strings.HasSuffix(value, string(os.PathSeparator)) {
			value += " "
		}
		model.Input.SetValue(value)
		model.closeCompletions()
		return
	}
	commands := model.commandNames
	if len(commands) == 0 {
		commands = defaultCommandNames
	}
	suggestions := completeInput(model.Input.Value, model.Input.CursorPos, commands, model.workspace)
	if len(suggestions) == 0 {
		return
	}
	value := suggestions[0]
	if len(suggestions) == 1 && !strings.HasSuffix(value, string(os.PathSeparator)) {
		value += " "
	}
	model.Input.SetValue(value)
	model.closeCompletions()
}

func (model *Model) refreshCompletions() {
	value := model.Input.Value
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \t\r\n") {
		model.closeCompletions()
		return
	}
	commands := model.commandNames
	if len(commands) == 0 {
		commands = defaultCommandNames
	}
	model.Completions = completeInput(value, model.Input.CursorPos, commands, model.workspace)
	if model.CompletionIndex >= len(model.Completions) {
		model.CompletionIndex = 0
	}
}

func (model *Model) closeCompletions() {
	model.Completions = nil
	model.CompletionIndex = 0
}

func (model *Model) pageSize() int { return max(1, model.Height-5) }

func (model *Model) scrollBy(delta int) {
	if view := model.subagents[model.activeSubagent]; view != nil {
		view.ScrollOffset = max(0, view.ScrollOffset+delta)
		return
	}
	model.ScrollOffset = max(0, model.ScrollOffset+delta)
}

func (model *Model) updateQuestion(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	answer := QuestionAnswer{}
	finished := false
	if model.QuestionSelect != nil {
		updated, _ := model.QuestionSelect.Update(key)
		model.QuestionSelect = updated.(*components.SelectDialog)
		if model.QuestionSelect.Closed {
			answer.Value = model.QuestionSelect.Result
			finished = true
		}
	} else if model.QuestionInput != nil {
		updated, _ := model.QuestionInput.Update(key)
		model.QuestionInput = updated.(*components.InputDialog)
		if key.Type == tea.KeyEnter {
			answer.Value, finished = model.QuestionInput.Value, true
		} else if key.Type == tea.KeyEsc {
			answer.Err, finished = fmt.Errorf("user canceled the question"), true
		}
	}
	if !finished {
		return model, nil
	}
	reply := model.questionReply
	model.QuestionSelect, model.QuestionInput, model.questionReply = nil, nil, nil
	return model, func() tea.Msg {
		if reply != nil {
			select {
			case reply <- answer:
			case <-model.ctx.Done():
			}
		}
		return permissionRespondedMsg{}
	}
}

func startTurn(runner Runner, ctx context.Context, prompt string) tea.Cmd {
	return func() tea.Msg {
		if runner == nil {
			return runtimeEventMsg{event: core.Event{Type: core.EventError, Err: &core.Error{
				Kind: core.ErrorKindConfiguration, Message: "runtime is not configured",
			}}, open: true}
		}
		return turnStartedMsg{events: runner.Run(ctx, prompt)}
	}
}

func waitForRuntimeEvent(events <-chan core.Event) tea.Cmd {
	return func() tea.Msg {
		select {
		case event, open := <-events:
			return runtimeEventMsg{event: event, open: open}
		case <-time.After(processingTickInterval):
			return processingTickMsg{}
		}
	}
}

func startObservation(observer Observer, ctx context.Context) tea.Cmd {
	if observer == nil {
		return nil
	}
	return func() tea.Msg { return observationStartedMsg{events: observer.Observe(ctx)} }
}

func waitForObservationEvent(events <-chan core.Event) tea.Cmd {
	return func() tea.Msg {
		event, open := <-events
		return observationEventMsg{event: event, open: open}
	}
}

func observerForRunner(runner Runner) Observer {
	observer, _ := runner.(Observer)
	return observer
}

func (model *Model) applyEvent(event core.Event) bool {
	switch event.Type {
	case core.EventTextDelta:
		model.appendAssistantDelta(event.Text)
	case core.EventAssistantMessage:
		if text := coreMessageText(event.Message); text != "" && model.assistantIndex < 0 {
			model.Messages = append(model.Messages, Message{Role: "assistant", Content: text})
		}
	case core.EventToolCall:
		if event.ToolCall != nil {
			model.recordToolCall(event.ToolCall)
		}
	case core.EventToolResult:
		if event.ToolResult != nil {
			model.recordToolResult(event.ToolResult)
		}
	case core.EventUsage:
		if event.Usage != nil {
			model.Usage.InputTokens += event.Usage.InputTokens
			model.Usage.OutputTokens += event.Usage.OutputTokens
			model.Usage.CacheReadInputTokens += event.Usage.CacheReadInputTokens
			model.Usage.CacheCreationInputTokens += event.Usage.CacheCreationInputTokens
		}
	case core.EventWarning:
		model.Messages = append(model.Messages, Message{Role: "system", Content: event.Text})
	case core.EventCompleted:
		model.finish()
		return true
	case core.EventError:
		if event.Err == nil {
			model.fail(fmt.Errorf("runtime failed"))
		} else {
			model.fail(event.Err)
		}
		return true
	}
	return false
}

func (model *Model) recordToolCall(call *core.ToolCall) {
	input := make(map[string]interface{})
	if len(call.Arguments) > 0 {
		_ = json.Unmarshal(call.Arguments, &input)
	}
	if index, ok := model.toolIndexes[call.ID]; ok {
		model.Tools[index].Name = call.Name
		model.Tools[index].Status = "running"
		model.Tools[index].Input = input
		return
	}
	model.toolIndexes[call.ID] = len(model.Tools)
	model.Tools = append(model.Tools, ToolPresentation{ID: call.ID, Name: call.Name, Status: "running", Input: input, FilePath: toolFilePath(input)})
}

func (model *Model) recordToolResult(result *core.ToolResult) {
	index, ok := model.toolIndexes[result.ToolCallID]
	if !ok {
		return
	}
	status := "succeeded"
	if result.IsError {
		status = "failed"
	}
	model.Tools[index].Status = status
	model.Tools[index].Output = toolResultText(result)
	model.Tools[index].IsError = result.IsError
	if result.Diff != nil {
		model.Tools[index].FilePath = result.Diff.Path
	}
}

func (model *Model) appendAssistantDelta(text string) {
	if model.assistantIndex < 0 {
		model.Messages = append(model.Messages, Message{Role: "assistant"})
		model.assistantIndex = len(model.Messages) - 1
	}
	model.Messages[model.assistantIndex].Content += text
}

func (model *Model) finish() {
	model.Processing, model.StatusText, model.events, model.turnCancel = false, "", nil, nil
	model.assistantIndex = -1
}

func (model *Model) fail(err error) {
	model.Err = err
	model.Messages = append(model.Messages, Message{Role: "error", Content: err.Error()})
	model.finish()
}

func (model *Model) updatePermission(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, _ := model.Permission.Update(key)
	model.Permission = updated.(*components.PermissionDialog)
	if !model.Permission.Closed {
		return model, nil
	}
	behavior := permissions.PermissionBehaviorDeny
	if model.Permission.Result == "Allow" || model.Permission.Result == "Allow Always" {
		behavior = permissions.PermissionBehaviorAllow
	}
	reply := model.permissionReply
	decision := permissions.Decision{Behavior: behavior, Reason: "interactive permission response"}
	decision.RememberSession = model.Permission.Result == "Allow Always"
	model.Permission, model.permissionReply = nil, nil
	return model, func() tea.Msg {
		if reply != nil {
			select {
			case reply <- decision:
			case <-model.ctx.Done():
			}
		}
		return permissionRespondedMsg{}
	}
}

func NewPermissionConfirmer(send func(tea.Msg)) permissions.Confirmer {
	return func(ctx context.Context, request permissions.Request) (permissions.Decision, error) {
		if send == nil {
			return permissions.Decision{Behavior: permissions.PermissionBehaviorDeny, Reason: "permission UI unavailable"}, nil
		}
		reply := make(chan permissions.Decision, 1)
		send(PermissionRequestMsg{Request: request, Respond: reply})
		select {
		case decision := <-reply:
			return decision, nil
		case <-ctx.Done():
			return permissions.Decision{}, ctx.Err()
		}
	}
}

func (model *Model) AddMessage(role, content string) {
	model.Messages = append(model.Messages, Message{Role: role, Content: content})
}

func (model *Model) View() string {
	if !model.Ready {
		return "Initializing..."
	}
	if model.showTaskList {
		return model.renderTaskListView()
	}
	if view := model.subagents[model.activeSubagent]; view != nil {
		return model.renderSubagentView(view)
	}
	width, height := max(20, model.Width), max(6, model.Height)
	header := []string{product.Name, strings.Repeat("-", width)}
	middle := model.renderScrollableContent(width)
	var overlays []string
	if model.Permission != nil {
		overlays = append(overlays, displayLines(model.Permission.View())...)
	}
	if model.QuestionSelect != nil {
		overlays = append(overlays, displayLines(model.QuestionSelect.View())...)
	}
	if model.QuestionInput != nil {
		overlays = append(overlays, displayLines(model.QuestionInput.View())...)
	}
	if model.Processing {
		model.ProcessingView.Message = model.StatusText
		overlays = append(overlays, displayLines(model.ProcessingView.View()+"  Ctrl+C cancel")...)
	}
	if len(model.Completions) > 0 {
		overlays = append(overlays, model.renderCompletions(6)...)
	}
	footer := append([]string{strings.Repeat("-", width)}, displayLines(model.Input.View())...)
	available := max(0, height-len(header)-len(footer)-len(overlays))
	if model.ScrollOffset > 0 && len(middle) > available {
		available = max(0, available-1)
		model.ScrollOffset = min(model.ScrollOffset, max(0, len(middle)-available))
		overlays = append([]string{fmt.Sprintf("↑ scrolled %d lines (PgDn/mouse wheel to return)", model.ScrollOffset)}, overlays...)
	} else {
		model.ScrollOffset = 0
	}
	model.ScrollOffset = min(model.ScrollOffset, max(0, len(middle)-available))
	middle = viewportLines(middle, available, model.ScrollOffset)
	middle = append(middle, overlays...)
	return strings.Join(append(append(header, middle...), footer...), "\n")
}

func (model *Model) renderCompletions(limit int) []string {
	count := min(limit, len(model.Completions))
	lines := []string{"commands (↑/↓ select, Tab/Enter accept):"}
	start := 0
	if model.CompletionIndex >= count {
		start = model.CompletionIndex - count + 1
	}
	for index := start; index < start+count; index++ {
		marker := "  "
		if index == model.CompletionIndex {
			marker = "> "
		}
		lines = append(lines, marker+model.Completions[index])
	}
	return lines
}

func (model *Model) renderScrollableContent(width int) []string {
	middle := renderMessages(model.Messages, width)
	middle = append(middle, renderToolPresentations(model.Tools, width, model.Processing)...)
	if model.Usage != (core.Usage{}) {
		middle = append(middle, fmt.Sprintf("tokens: input=%d output=%d cache_read=%d cache_creation=%d",
			model.Usage.InputTokens, model.Usage.OutputTokens,
			model.Usage.CacheReadInputTokens, model.Usage.CacheCreationInputTokens))
	}
	return middle
}

func (model *Model) scrollableLineCount() int {
	return len(model.renderScrollableContent(max(20, model.Width)))
}

func viewportLines(lines []string, maximum, offset int) []string {
	if maximum <= 0 || len(lines) == 0 {
		return nil
	}
	offset = min(max(0, offset), max(0, len(lines)-maximum))
	end := len(lines) - offset
	start := max(0, end-maximum)
	return lines[start:end]
}

func RunUI(runner Runner) error {
	program := tea.NewProgram(NewModel(runner, ModelOptions{}), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := program.Run()
	return err
}

// SetVimMode changes input behavior without replacing the input buffer.
func (model *Model) SetVimMode(enabled bool) {
	model.Input.SetVimEnabled(enabled)
}

func coreMessageText(message *core.Message) string {
	if message == nil {
		return ""
	}
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type == core.ContentText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

func toolResultText(result *core.ToolResult) string {
	var text strings.Builder
	for _, block := range result.Content {
		if block.Type == core.ContentText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

func toolFilePath(input map[string]interface{}) string {
	for _, key := range []string{"path", "file_path", "target_file"} {
		if path, ok := input[key].(string); ok {
			return path
		}
	}
	return ""
}

func renderToolPresentations(tools []ToolPresentation, width int, expanded bool) []string {
	var lines []string
	for _, state := range tools {
		if state.Status == "running" {
			summary := components.ToolUseSummary{ToolName: state.Name, Input: state.Input}
			lines = append(lines, summary.Render()+" (running)")
			continue
		}
		var toolErr error
		if state.IsError {
			message := state.Output
			if message == "" {
				message = "tool failed"
			}
			toolErr = fmt.Errorf("%s", message)
		}
		output := state.Output
		if !expanded && !state.IsError {
			output = ""
		} else if state.Name == "read_file" && !state.IsError {
			output = ""
		}
		rendered := components.RenderToolResult(components.ToolResultDisplay{
			ToolName: state.Name, ToolUseID: state.ID, Output: output, Error: toolErr, FilePath: state.FilePath,
		}, width)
		lines = append(lines, displayLines(rendered)...)
		if expanded && state.Name == "read_file" && state.Output != "" {
			lineCount := strings.Count(state.Output, "\n") + 1
			preview := components.FilePreview{Path: state.FilePath, Content: state.Output, StartLine: 1, EndLine: min(lineCount, 8)}
			lines = append(lines, displayLines(preview.Render(width))...)
		}
	}
	return lines
}

func permissionDescription(request permissions.Request) string {
	if target := permissions.SafeTargetSummary(request, true); target != "" {
		return target
	}
	return request.Action
}
