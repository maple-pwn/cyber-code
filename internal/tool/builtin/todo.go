package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/product"
	"cyber-code/internal/tool"
)

const maxTodoItems = 256

type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

type todoTool struct {
	path string
	spec tool.Spec
	mu   sync.Mutex
}

func NewTodo(path string) tool.Tool {
	return &todoTool{path: path, spec: tool.Spec{
		Name: "todo_write", Description: "Create or update the session task list",
		Schema: json.RawMessage(`{"type":"object","required":["todos"],"properties":{"todos":{"type":"array"}},"additionalProperties":false}`),
	}}
}

func (todo *todoTool) Spec() tool.Spec { return todo.spec }

func (todo *todoTool) Authorize(_ context.Context, _ json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Tool: todo.spec.Name, Action: permissions.ActionWrite, Workspace: filepath.Dir(todo.path), Paths: []string{todo.path}}, nil
}

func (todo *todoTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	var input struct {
		Todos []TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return core.ToolResult{}, err
	}
	if len(input.Todos) > maxTodoItems {
		return core.ToolResult{}, fmt.Errorf("todo list exceeds %d items", maxTodoItems)
	}
	seen := make(map[string]struct{}, len(input.Todos))
	for index := range input.Todos {
		item := &input.Todos[index]
		item.ID, item.Content, item.Status = strings.TrimSpace(item.ID), strings.TrimSpace(item.Content), strings.TrimSpace(item.Status)
		if item.ID == "" || item.Content == "" {
			return core.ToolResult{}, fmt.Errorf("todo %d requires id and content", index)
		}
		if item.Status != "pending" && item.Status != "in_progress" && item.Status != "completed" && item.Status != "cancelled" {
			return core.ToolResult{}, fmt.Errorf("todo %q has invalid status %q", item.ID, item.Status)
		}
		if _, exists := seen[item.ID]; exists {
			return core.ToolResult{}, fmt.Errorf("todo ID %q is duplicated", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	todo.mu.Lock()
	defer todo.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}
	if err := writeTodoFile(todo.path, input.Todos); err != nil {
		return core.ToolResult{}, err
	}
	return textResult(formatTodos(input.Todos)), nil
}

func formatTodos(items []TodoItem) string {
	if len(items) == 0 {
		return "Todo list is empty"
	}
	items = append([]TodoItem(nil), items...)
	sort.SliceStable(items, func(left, right int) bool { return items[left].ID < items[right].ID })
	var output strings.Builder
	output.WriteString("Todo list updated:\n")
	for _, item := range items {
		fmt.Fprintf(&output, "- [%s] %s: %s\n", item.Status, item.ID, item.Content)
	}
	return strings.TrimRight(output.String(), "\n")
}

func writeTodoFile(path string, items []TodoItem) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("todo state path is required")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, "."+product.Name+"-todo-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = temporary.Close(); _ = os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return syncDirectory(directory)
}
