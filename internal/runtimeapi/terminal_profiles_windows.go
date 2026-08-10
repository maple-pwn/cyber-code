//go:build windows

package runtimeapi

func DefaultTerminalProfiles() map[string]TerminalProfile {
	return map[string]TerminalProfile{
		"default-shell": {
			ID: "default-shell", RequiredAction: "terminal.open", Risk: "low", Program: "cmd.exe", Arguments: []string{"/Q"},
		},
	}
}
