package ui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	diffHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	diffHunkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	diffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	diffDeleteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func renderMessages(messages []Message, width int) []string {
	var result []string
	for _, message := range messages {
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
		content := ansi.Strip(message.Content)
		if message.Role == "assistant" {
			if looksLikeDiff(content) {
				content = renderUnifiedDiff(content, width)
			} else if rendered, err := renderMarkdown(content, width); err == nil {
				content = rendered
			}
		}
		lines := displayLines(content)
		if len(lines) == 0 {
			continue
		}
		result = append(result, label+": "+lines[0])
		result = append(result, lines[1:]...)
	}
	return result
}

func renderMarkdown(source string, width int) (string, error) {
	width = max(10, width)
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	rendered, err := renderer.Render(ansi.Strip(source))
	if err != nil {
		return "", err
	}
	return strings.Trim(rendered, "\n"), nil
}

func renderUnifiedDiff(source string, width int) string {
	width = max(10, width)
	lines := strings.Split(ansi.Strip(source), "\n")
	for index, line := range lines {
		if ansi.StringWidth(line) > width {
			line = ansi.Truncate(line, width, "")
		}
		switch {
		case strings.HasPrefix(line, "diff --git "), strings.HasPrefix(line, "index "):
			lines[index] = diffHeaderStyle.Render(line)
		case strings.HasPrefix(line, "@@"):
			lines[index] = diffHunkStyle.Render(line)
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			lines[index] = diffAddStyle.Render(line)
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			lines[index] = diffDeleteStyle.Render(line)
		default:
			lines[index] = line
		}
	}
	return strings.Join(lines, "\n")
}

func looksLikeDiff(content string) bool {
	return strings.HasPrefix(content, "diff --git ") ||
		(strings.Contains(content, "\n--- ") && strings.Contains(content, "\n+++ "))
}

func displayLines(value string) []string {
	value = strings.TrimSuffix(value, "\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func tailLines(lines []string, maximum int) []string {
	if maximum <= 0 {
		return nil
	}
	if len(lines) <= maximum {
		return lines
	}
	return lines[len(lines)-maximum:]
}
