package doctor

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunReportsMissingOptionalDependenciesWithoutFailure(t *testing.T) {
	report := Run(context.Background(), Options{
		ConfigFile: filepath.Join(t.TempDir(), "missing.yaml"),
		StateDir:   t.TempDir(),
		LookPath:   func(string) (string, error) { return "", exec.ErrNotFound },
	})
	if report.SchemaVersion != 1 || len(report.Checks) == 0 || !report.Healthy {
		t.Fatalf("report = %#v", report)
	}
	for _, check := range report.Checks {
		if check.Status == StatusFail && !check.Required {
			t.Fatalf("optional check failed report: %#v", check)
		}
	}
}
