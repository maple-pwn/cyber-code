package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (model *Model) View() string {
	if !model.Ready {
		return "Initializing..."
	}
	width := max(20, model.Width)
	var output strings.Builder
	output.WriteString("Claude Code\n")
	output.WriteString(strings.Repeat("-", width) + "\n")
	for _, message := range model.Messages {
		label := message.Role
		switch message.Role {
		case "user":
			label = "You"
		case "assistant":
			label = "Assistant"
		case "tool":
			label = "Tool"
		case "error":
			label = "Error"
		}
		output.WriteString(fmt.Sprintf("%s: %s\n", label, message.Content))
	}
	if model.Permission != nil {
		output.WriteString(model.Permission.View())
		output.WriteByte('\n')
	}
	if model.Processing {
		output.WriteString(model.StatusText + "\n")
	}
	output.WriteString(strings.Repeat("-", width) + "\n")
	output.WriteString(model.Input.View())
	return output.String()
}

func RunUI(runner Runner) error {
	program := tea.NewProgram(NewModel(runner, ModelOptions{}), tea.WithAltScreen())
	_, err := program.Run()
	return err
}
