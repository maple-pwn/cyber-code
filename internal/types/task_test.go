package types

import (
	"os"
	"path/filepath"
	"testing"

	"cyber-code/internal/product"
)

func TestGetTaskOutputPathUsesProductTemporaryDirectory(t *testing.T) {
	got := GetTaskOutputPath("task-123")
	want := filepath.Join(os.TempDir(), product.Name, "tasks", "task-123.log")
	if got != want {
		t.Fatalf("task output path = %q, want %q", got, want)
	}
}
