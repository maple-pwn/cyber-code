package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCompleteInputReturnsBoundedSortedDataOnlySuggestions(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"beta.txt", "alpha.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := completeInput("/s", 2, []string{"status", "skills", "status"}, workspace); !reflect.DeepEqual(got, []string{"/skills", "/status"}) {
		t.Fatalf("command suggestions = %#v", got)
	}
	if got := completeInput("open a", 6, nil, workspace); !reflect.DeepEqual(got, []string{"open alpha.txt"}) {
		t.Fatalf("path suggestions = %#v", got)
	}
}

func TestDefaultCommandCompletionIncludesGitWorkflows(t *testing.T) {
	for _, command := range []string{"commit", "diff", "review", "init", "cost", "stats", "clear", "vim", "config"} {
		found := false
		for _, candidate := range defaultCommandNames {
			found = found || candidate == command
		}
		if !found {
			t.Fatalf("default completion is missing %q", command)
		}
	}
}

func TestDefaultCommandCompletionIncludesExitAliases(t *testing.T) {
	for _, command := range []string{"exit", "quit"} {
		found := false
		for _, candidate := range defaultCommandNames {
			found = found || candidate == command
		}
		if !found {
			t.Fatalf("default completions missing %q", command)
		}
	}
}
