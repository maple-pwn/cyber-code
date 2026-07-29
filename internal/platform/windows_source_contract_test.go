package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsSandboxHasNoImplicitResourceLimitsOrElevationClaims(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	directory := filepath.Dir(currentFile)
	var source strings.Builder
	for _, name := range []string{"exec_windows.go", "sandbox_windows.go"} {
		content, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		source.Write(content)
	}
	for _, forbidden := range []string{"JOB_OBJECT_LIMIT_ACTIVE_PROCESS", "JOB_OBJECT_LIMIT_JOB_MEMORY", "isRunningElevated"} {
		if strings.Contains(source.String(), forbidden) {
			t.Fatalf("Windows sandbox contains unsupported implicit behavior %q", forbidden)
		}
	}
}
