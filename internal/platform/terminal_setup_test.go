package platform

import (
	"reflect"
	"strings"
	"testing"
)

func TestTerminalSetupGuidanceIsPlatformSpecificAndNonMutating(t *testing.T) {
	environment := map[string]string{"SHELL": "/bin/zsh", "TERM_PROGRAM": "iTerm.app"}
	before := map[string]string{"SHELL": environment["SHELL"], "TERM_PROGRAM": environment["TERM_PROGRAM"]}
	guidance, err := TerminalSetupGuidance("darwin", func(name string) string { return environment[name] })
	if err != nil || !guidance.Supported || len(guidance.Steps) == 0 || !strings.Contains(strings.Join(guidance.Steps, " "), "does not modify") {
		t.Fatalf("guidance=%#v error=%v", guidance, err)
	}
	if !reflect.DeepEqual(environment, before) {
		t.Fatalf("environment was mutated: %#v", environment)
	}
	for _, goos := range []string{"linux", "windows"} {
		guidance, err := TerminalSetupGuidance(goos, func(string) string { return "" })
		if err != nil || !guidance.Supported || len(guidance.Steps) == 0 {
			t.Fatalf("%s guidance=%#v error=%v", goos, guidance, err)
		}
	}
}

func TestTerminalSetupGuidanceRejectsUnsupportedPlatform(t *testing.T) {
	if _, err := TerminalSetupGuidance("plan9", func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v", err)
	}
}
