package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"claude-code-go/internal/commands"
	"claude-code-go/internal/core"
	"claude-code-go/internal/frontend"
	"claude-code-go/internal/query"
	"claude-code-go/internal/tools"
	"claude-code-go/internal/types"
	"claude-code-go/internal/ui"
	"claude-code-go/pkg/api"
)

// App represents the CLI application.
type App struct {
	config        *Config
	registry      *commands.Registry
	toolRegistry  *tools.Registry
	queryEngine   *query.QueryEngine
	uiModel       *ui.Model
	apiClient     *api.Client
	ctx           context.Context
	cancel        context.CancelFunc
	initialPrompt string
	version       string
}

// Config holds CLI configuration.
type Config struct {
	Debug          bool
	Verbose        bool
	PrintMode      bool
	Model          string
	PermissionMode string
	Cwd            string
	MaxTurns       int
	APIKey         string
	BaseURL        string
	Runtime        frontend.Runner
	PrintJSON      bool
	Stdout         io.Writer
	Stderr         io.Writer
	PermissionUI   *ui.PermissionBridge
	Context        context.Context
}

// ExitError preserves a frontend exit status for the process entrypoint.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("application exited with status %d", e.Code)
}

// NewApp creates a new CLI application.
func NewApp(config *Config, version string) *App {
	if config == nil {
		config = &Config{}
	}
	parent := config.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	return &App{
		config:  config,
		ctx:     ctx,
		cancel:  cancel,
		version: version,
	}
}

// Initialize sets up all application components.
func (a *App) Initialize() error {
	// Get current working directory
	cwd := a.config.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// Initialize command registry
	a.registry = commands.NewRegistry()
	a.registerCommands()

	// Initialize tool registry
	a.toolRegistry = tools.NewToolRegistry()
	a.registerTools()

	// Initialize API client
	a.apiClient = api.NewClient(api.Config{
		APIKey:  a.config.APIKey,
		BaseURL: a.config.BaseURL,
	})

	// Initialize query engine
	queryConfig := query.QueryEngineConfig{
		Cwd:       cwd,
		Tools:     a.toolRegistry.List(),
		MaxTurns:  a.config.MaxTurns,
		APIClient: a.apiClient,
		GetAppState: func() *types.AppState {
			return &types.AppState{
				MainLoopModel: a.config.Model,
			}
		},
	}

	if a.config.Model != "" {
		queryConfig.UserSpecifiedModel = a.config.Model
	}

	a.queryEngine = query.NewQueryEngine(queryConfig)

	return nil
}

// Run starts the application.
func (a *App) Run(initialPrompt string) error {
	a.initialPrompt = initialPrompt

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	defer func() {
		signal.Stop(sigChan)
		close(done)
	}()
	go func() {
		select {
		case <-sigChan:
			a.cancel()
		case <-done:
		}
	}()

	if a.config.PrintMode {
		return a.runPrintMode()
	}
	return a.runInteractiveMode()
}

// runPrintMode runs in non-interactive mode.
func (a *App) runPrintMode() error {
	if a.initialPrompt == "" {
		return fmt.Errorf("no prompt provided in print mode")
	}

	code := frontend.Run(a.ctx, a.runner(), a.initialPrompt, frontend.PrintOptions{
		JSON: a.config.PrintJSON, Stdout: a.config.Stdout, Stderr: a.config.Stderr,
	})
	if code != frontend.ExitOK {
		return &ExitError{Code: code}
	}
	return nil
}

// runInteractiveMode runs the interactive UI.
func (a *App) runInteractiveMode() error {
	a.uiModel = ui.NewModel(a.runner(), ui.ModelOptions{InitialPrompt: a.initialPrompt})

	// Create and run the tea program
	p := tea.NewProgram(a.uiModel, tea.WithAltScreen())
	if a.config.PermissionUI != nil {
		a.config.PermissionUI.Attach(p.Send)
		defer a.config.PermissionUI.Detach()
	}

	// Handle UI events in a goroutine
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				p.Quit()
				return
			}
		}
	}()

	// Start the UI
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running UI: %w", err)
	}

	// Handle final state
	if m, ok := finalModel.(*ui.Model); ok {
		if m.Err != nil {
			return m.Err
		}
	}

	return nil
}

// registerCommands registers all built-in commands.
func (a *App) registerCommands() {
	a.registry.Register(commands.NewHelpCommand(a.registry))
	a.registry.Register(commands.NewExitCommand())
	a.registry.Register(commands.NewClearCommand())
	a.registry.Register(commands.NewModelCommand())
	a.registry.Register(commands.NewConfigCommand())
	a.registry.Register(commands.NewCostCommand())
	a.registry.Register(commands.NewThemeCommand())
}

// registerTools registers all built-in tools.
func (a *App) registerTools() {
	a.toolRegistry.Register(tools.NewBashTool())
	a.toolRegistry.Register(tools.NewFileReadTool())
	a.toolRegistry.Register(tools.NewFileWriteTool())
	a.toolRegistry.Register(tools.NewGlobTool())
	a.toolRegistry.Register(tools.NewGrepTool())
}

// Shutdown cleans up resources.
func (a *App) Shutdown() {
	if a.cancel != nil {
		a.cancel()
	}
	if runtime, ok := a.config.Runtime.(interface {
		Shutdown(context.Context) error
	}); ok {
		_ = runtime.Shutdown(context.Background())
	}
}

// RunWithPrompt runs the app with a specific prompt (for scripting).
func (a *App) RunWithPrompt(prompt string) (string, error) {
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	var output strings.Builder
	for event := range a.runner().Run(ctx, prompt) {
		switch event.Type {
		case core.EventTextDelta:
			output.WriteString(event.Text)
		case core.EventAssistantMessage:
			if event.Message != nil {
				for _, block := range event.Message.Content {
					if block.Type == core.ContentText {
						output.WriteString(block.Text)
					}
				}
			}
		case core.EventError:
			if event.Err == nil {
				return "", errors.New("runtime failed")
			}
			return "", event.Err
		}
	}
	return output.String(), nil
}

func (a *App) runner() frontend.Runner {
	if a.config.Runtime != nil {
		return a.config.Runtime
	}
	return legacyQueryRunner{engine: a.queryEngine}
}

type legacyQueryRunner struct {
	engine *query.QueryEngine
}

func (runner legacyQueryRunner) Run(ctx context.Context, prompt string) <-chan core.Event {
	events := make(chan core.Event, 16)
	go func() {
		defer close(events)
		if runner.engine == nil {
			emitLegacyEvent(ctx, events, core.Event{Type: core.EventError, Err: &core.Error{
				Kind: core.ErrorKindConfiguration, Op: "cli.query", Message: "runtime is not configured",
			}})
			return
		}
		messages, err := runner.engine.SubmitMessage(ctx, prompt)
		if err != nil {
			emitLegacyError(events, ctx, err)
			return
		}
		for message := range messages {
			switch message := message.(type) {
			case query.SDKMessage:
				for _, event := range legacySDKEvents(message) {
					if !emitLegacyEvent(ctx, events, event) || event.Type == core.EventError {
						return
					}
				}
			case query.ResultMessage:
				if message.IsError {
					text := strings.TrimSpace(message.Result)
					if text == "" {
						text = strings.TrimSpace(message.Subtype)
					}
					emitLegacyEvent(ctx, events, core.Event{Type: core.EventError, Err: &core.Error{
						Kind: core.ErrorKindProvider, Op: "cli.query", Message: text,
					}})
					return
				}
				if !emitLegacyEvent(ctx, events, core.Event{Type: core.EventCompleted, FinishReason: message.StopReason}) {
					return
				}
			}
		}
	}()
	return events
}

func legacySDKEvents(message query.SDKMessage) []core.Event {
	switch message.Type {
	case "assistant":
		response, ok := message.Message.(*api.MessageResponse)
		if !ok || response == nil {
			return nil
		}
		events := make([]core.Event, 0, len(response.Content))
		for _, block := range response.Content {
			switch block.Type {
			case "text":
				events = append(events, core.Event{Type: core.EventTextDelta, Text: block.Text})
			case "tool_use":
				events = append(events, core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{
					ID: block.ID, Name: block.Name, Arguments: block.Input,
				}})
			}
		}
		return events
	case "tool_result":
		result, ok := message.Message.(map[string]interface{})
		if !ok {
			return nil
		}
		return []core.Event{{Type: core.EventToolResult, ToolResult: &core.ToolResult{
			ToolCallID: stringValue(result["tool_use_id"]),
			Content:    []core.ContentBlock{{Type: core.ContentText, Text: stringValue(result["content"])}},
		}}}
	case "system":
		if text := legacySystemError(message.Message); text != "" {
			return []core.Event{{Type: core.EventError, Err: &core.Error{
				Kind: core.ErrorKindProvider, Op: "cli.query", Message: text,
			}}}
		}
	}
	return nil
}

func legacySystemError(message interface{}) string {
	switch value := message.(type) {
	case map[string]string:
		if value["subtype"] == "error" {
			return value["error"]
		}
	case map[string]interface{}:
		if stringValue(value["subtype"]) == "error" {
			return stringValue(value["error"])
		}
	}
	return ""
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func emitLegacyError(events chan<- core.Event, ctx context.Context, err error) {
	kind := core.ErrorKindProvider
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		kind = core.ErrorKindCanceled
	}
	emitLegacyEvent(ctx, events, core.Event{Type: core.EventError, Err: &core.Error{
		Kind: kind, Op: "cli.query", Message: err.Error(), Cause: err,
	}})
}

func emitLegacyEvent(ctx context.Context, events chan<- core.Event, event core.Event) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
