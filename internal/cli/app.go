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

	"cyber-code/internal/core"
	"cyber-code/internal/frontend"
	"cyber-code/internal/ui"
)

// App represents the CLI application.
type App struct {
	config        *Config
	uiModel       *ui.Model
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
	QuestionUI     *ui.QuestionBridge
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
	if a.config.QuestionUI != nil {
		a.config.QuestionUI.Attach(p.Send)
		defer a.config.QuestionUI.Detach()
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
	return unconfiguredRunner{}
}

type unconfiguredRunner struct{}

func (unconfiguredRunner) Run(context.Context, string) <-chan core.Event {
	events := make(chan core.Event, 1)
	events <- core.Event{Type: core.EventError, Err: &core.Error{
		Kind: core.ErrorKindConfiguration, Op: "cli.runtime", Message: "runtime is not configured",
	}}
	close(events)
	return events
}
