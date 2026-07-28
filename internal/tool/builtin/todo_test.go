package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodoWritesValidatedSessionStateAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.json")
	todo := NewTodo(path)
	arguments, _ := json.Marshal(map[string]any{"todos": []TodoItem{{ID: "a", Content: "first task", Status: "in_progress"}}})
	result, err := todo.Run(context.Background(), arguments)
	if err != nil || !strings.Contains(result.Content[0].Text, "first task") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(encoded), "in_progress") {
		t.Fatalf("stored todo = %s, error = %v", encoded, err)
	}
}

func TestTodoRejectsDuplicateAndInvalidItems(t *testing.T) {
	todo := NewTodo(filepath.Join(t.TempDir(), "todo.json"))
	for name, items := range map[string][]TodoItem{
		"duplicate": {{ID: "same", Content: "one", Status: "pending"}, {ID: "same", Content: "two", Status: "pending"}},
		"status":    {{ID: "bad", Content: "bad status", Status: "unknown"}},
	} {
		t.Run(name, func(t *testing.T) {
			arguments, _ := json.Marshal(map[string]any{"todos": items})
			if _, err := todo.Run(context.Background(), arguments); err == nil {
				t.Fatal("invalid todo was accepted")
			}
		})
	}
}
