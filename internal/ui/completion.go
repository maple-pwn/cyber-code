package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxCompletions = 50

var defaultCommandNames = []string{
	"branch", "checkpoint", "compact", "context", "help", "hooks", "mcp",
	"memory", "model", "permissions", "rewind", "skills", "status", "tasks",
}

func completeInput(value string, cursor int, commands []string, workspace string) []string {
	runes := []rune(value)
	if cursor < 0 || cursor > len(runes) {
		return nil
	}
	before, after := string(runes[:cursor]), string(runes[cursor:])
	if strings.HasPrefix(before, "/") && !strings.ContainsAny(before, " \t\r\n") {
		prefix := strings.TrimPrefix(before, "/")
		seen := make(map[string]struct{})
		var result []string
		for _, command := range commands {
			command = strings.TrimPrefix(strings.TrimSpace(command), "/")
			candidate := "/" + command + after
			if command == "" || !strings.HasPrefix(command, prefix) {
				continue
			}
			if _, duplicate := seen[candidate]; duplicate {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
		sort.Strings(result)
		return limitCompletions(result)
	}
	if strings.TrimSpace(workspace) == "" {
		return nil
	}
	start := strings.LastIndexAny(before, " \t\r\n") + 1
	token := before[start:]
	if token == "" {
		return nil
	}
	directoryToken, namePrefix := filepath.Split(filepath.FromSlash(token))
	directory := filepath.Join(workspace, directoryToken)
	if !insideCompletionWorkspace(workspace, directory) {
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), namePrefix) {
			continue
		}
		completed := filepath.ToSlash(filepath.Join(directoryToken, entry.Name()))
		if entry.IsDir() {
			completed += "/"
		}
		result = append(result, before[:start]+completed+after)
	}
	sort.Strings(result)
	return limitCompletions(result)
}

func insideCompletionWorkspace(workspace, candidate string) bool {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(workspace, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func limitCompletions(values []string) []string {
	if len(values) > maxCompletions {
		return values[:maxCompletions]
	}
	return values
}
