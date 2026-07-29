package platform

import (
	"fmt"
	"strings"
)

type TerminalGuidance struct {
	Supported bool
	Terminal  string
	Shell     string
	Steps     []string
}

func TerminalSetupGuidance(goos string, getenv func(string) string) (TerminalGuidance, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	guidance := TerminalGuidance{Supported: true, Terminal: strings.TrimSpace(getenv("TERM_PROGRAM")), Shell: strings.TrimSpace(getenv("SHELL"))}
	switch goos {
	case "linux":
		guidance.Steps = []string{
			"cyber-code does not modify shell profiles or terminal settings automatically.",
			"Use a UTF-8 terminal and ensure TERM is set; install a Nerd Font only if you want richer glyphs.",
			"Configure your terminal emulator's multiline key binding explicitly if Shift+Enter is not forwarded.",
		}
	case "windows":
		guidance.Steps = []string{
			"cyber-code does not modify PowerShell profiles, Windows Terminal settings, or the registry.",
			"Use Windows Terminal with UTF-8 output and a recent PowerShell or Command Prompt.",
			"Configure multiline key bindings in Windows Terminal when the host consumes Shift+Enter.",
		}
	case "darwin":
		guidance.Steps = []string{
			"cyber-code does not modify shell profiles or terminal settings automatically.",
			"Use a UTF-8 terminal and configure multiline key bindings in the terminal application if needed.",
		}
	default:
		return TerminalGuidance{}, fmt.Errorf("terminal setup is unsupported on %q", goos)
	}
	return guidance, nil
}
