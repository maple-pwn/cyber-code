package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"cyber-code/internal/ui/adapter"
)

func runTactical(environment *commandEnvironment, source adapter.Source, initialObjective string) error {
	model := adapter.NewModel(source, adapter.ModelOptions{
		Context: environment.ctx, ClientID: "tui-client", RuntimeID: "scenario-local",
		InitialObjective: initialObjective, Demo: true,
	})
	program := tea.NewProgram(
		model, tea.WithAltScreen(), tea.WithMouseCellMotion(),
		tea.WithInput(environment.stdin), tea.WithOutput(environment.stdout),
	)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-environment.ctx.Done():
			program.Quit()
		case <-done:
		}
	}()
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run tactical UI: %w", err)
	}
	return nil
}
