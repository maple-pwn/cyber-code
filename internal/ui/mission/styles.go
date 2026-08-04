package mission

import "github.com/charmbracelet/lipgloss"

var (
	brandStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	liveStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	warningStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	dangerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	sectionStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	frameStyle    = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238"))
	approvalStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("214")).Padding(0, 1)
)
