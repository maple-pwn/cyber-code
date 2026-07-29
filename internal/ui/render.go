package ui

import "strings"

func renderMessages(messages []Message) []string {
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
		lines := displayLines(message.Content)
		if len(lines) == 0 {
			continue
		}
		result = append(result, label+": "+lines[0])
		result = append(result, lines[1:]...)
	}
	return result
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
