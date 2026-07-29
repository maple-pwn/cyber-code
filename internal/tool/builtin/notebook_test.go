package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotebookEditReplacesCellByIDAtomically(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "demo.ipynb")
	notebook := `{"nbformat":4,"nbformat_minor":5,"metadata":{},"cells":[{"cell_type":"code","id":"abc","metadata":{},"source":["print(1)\n"],"outputs":[]}]}`
	if err := os.WriteFile(path, []byte(notebook), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewNotebookEdit(workspace)
	args := `{"path":"demo.ipynb","operation":"replace","cell_id":"abc","source":"print(2)\n"}`
	result, err := tool.Run(context.Background(), []byte(args))
	if err != nil || !strings.Contains(result.Content[0].Text, "updated") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	content, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(content), "print(2)") || strings.Contains(string(content), "print(1)") {
		t.Fatalf("notebook=%s err=%v", content, err)
	}
	if result.Diff == nil || result.Diff.Path != "demo.ipynb" || result.Diff.OldText != notebook || result.Diff.NewText != string(content) {
		t.Fatalf("notebook diff = %#v", result.Diff)
	}
}

func TestNotebookEditSupportsInsertAndDeleteAndRejectsInvalidNotebook(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "demo.ipynb")
	base := map[string]any{"nbformat": 4, "nbformat_minor": 5, "metadata": map[string]any{}, "cells": []any{}}
	encoded, _ := json.Marshal(base)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewNotebookEdit(workspace)
	insert := `{"path":"demo.ipynb","operation":"insert","index":0,"cell_type":"markdown","source":"hello"}`
	if _, err := tool.Run(context.Background(), []byte(insert)); err != nil {
		t.Fatal(err)
	}
	delete := `{"path":"demo.ipynb","operation":"delete","index":0}`
	if _, err := tool.Run(context.Background(), []byte(delete)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"cells":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), []byte(insert)); err == nil {
		t.Fatal("invalid notebook was accepted")
	}
}
