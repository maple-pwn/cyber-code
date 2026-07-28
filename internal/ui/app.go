package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
	"claude-code-go/internal/ui/components"
)

type Runner interface {
	Run(context.Context, string) <-chan core.Event
}

type ModelOptions struct {
	Width         int
	Height        int
	InitialPrompt string
	VimMode       bool
}

type Message struct {
	Role    string
	Content string
}

type Model struct {
	runner Runner
	ctx    context.Context
	cancel context.CancelFunc

	Messages   []Message
	Input      *components.InputModel
	Processing bool
	StatusText string
	Err        error
	Width      int
	Height     int
	Ready      bool
	Permission *components.PermissionDialog

	events          <-chan core.Event
	turnCancel      context.CancelFunc
	assistantIndex  int
	permissionReply chan<- permissions.Decision
	initialPrompt   string
}

type turnStartedMsg struct{ events <-chan core.Event }
type runtimeEventMsg struct {
	event core.Event
	open  bool
}
type permissionRespondedMsg struct{}

type PermissionRequestMsg struct {
	Request permissions.Request
	Respond chan<- permissions.Decision
}

type PermissionBridge struct {
	mu   sync.RWMutex
	send func(tea.Msg)
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
	ctx, cancel := context.WithCancel(context.Background())
	input := components.NewInput(">", "Type your message...", options.Width)
	input.SetVimEnabled(options.VimMode)
	return &Model{
		runner: runner, ctx: ctx, cancel: cancel, Messages: []Message{}, Input: input, Width: options.Width, Height: options.Height,
		Ready: true, assistantIndex: -1, initialPrompt: options.InitialPrompt,
	}
}

func InitialModel() Model { return *NewModel(nil, ModelOptions{}) }

func (model *Model) Init() tea.Cmd {
	if strings.TrimSpace(model.initialPrompt) == "" {
		return nil
	}
	model.Input.SetValue(model.initialPrompt)
	model.initialPrompt = ""
	return func() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
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
		if model.Permission != nil {
			return model.updatePermission(message)
		}
		switch message.Type {
		case tea.KeyCtrlC:
			if model.turnCancel != nil {
				model.turnCancel()
			}
			model.cancel()
			return model, tea.Quit
		case tea.KeyEnter:
			if model.Processing || strings.TrimSpace(model.Input.Value) == "" {
				return model, nil
			}
			prompt := model.Input.Value
			model.Input.Clear()
			model.Messages = append(model.Messages, Message{Role: "user", Content: prompt})
			model.Processing, model.StatusText, model.Err = true, "Working", nil
			turnCtx, cancel := context.WithCancel(model.ctx)
			model.turnCancel = cancel
			return model, startTurn(model.runner, turnCtx, prompt)
		default:
			return model, model.Input.Update(message)
		}
	case turnStartedMsg:
		model.events = message.events
		if message.events == nil {
			model.fail(fmt.Errorf("runtime returned no event stream"))
			return model, nil
		}
		return model, waitForRuntimeEvent(message.events)
	case runtimeEventMsg:
		if !message.open {
			model.finish()
			return model, nil
		}
		terminal := model.applyEvent(message.event)
		if terminal {
			return model, nil
		}
		return model, waitForRuntimeEvent(model.events)
	case PermissionRequestMsg:
		model.Permission = components.NewPermissionDialog(message.Request.Tool, permissionDescription(message.Request))
		model.permissionReply = message.Respond
		return model, nil
	case permissionRespondedMsg:
		return model, nil
	}
	return model, nil
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
		event, open := <-events
		return runtimeEventMsg{event: event, open: open}
	}
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
			model.Messages = append(model.Messages, Message{Role: "tool", Content: event.ToolCall.Name})
		}
	case core.EventToolResult:
		if event.ToolResult != nil {
			model.Messages = append(model.Messages, Message{Role: "tool", Content: toolResultText(event.ToolResult)})
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

func permissionDescription(request permissions.Request) string {
	if request.Command != "" {
		return request.Command
	}
	if len(request.Paths) > 0 {
		return strings.Join(request.Paths, "\n")
	}
	return request.Action
}
