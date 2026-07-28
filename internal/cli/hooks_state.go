package cli

import (
	"fmt"
	"strings"

	"cyber-code/internal/hooks"
)

func loadHookCommands(path string) (map[hooks.HookEvent][]string, error) {
	encoded := make(map[string][]string)
	if err := readStateFile(path, &encoded); err != nil {
		return nil, err
	}
	commands := make(map[hooks.HookEvent][]string, len(encoded))
	for name, configured := range encoded {
		if !hooks.IsHookEvent(name) {
			return nil, fmt.Errorf("unsupported hook event %q", name)
		}
		event := hooks.HookEvent(name)
		for _, command := range configured {
			command = strings.TrimSpace(command)
			if command == "" {
				return nil, fmt.Errorf("hook command for %s is required", event)
			}
			commands[event] = append(commands[event], command)
		}
	}
	return commands, nil
}
