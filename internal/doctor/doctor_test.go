package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestRunProvidesRemediationForOperationalWarnings(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configFile, []byte(`
active_profile: local
profiles:
  local:
    provider: openai
    model: test-model
    api_key_env: TEST_MISSING_API_KEY
`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), Options{
		ConfigFile: configFile, StateDir: t.TempDir(), GOOS: "linux",
		LookPath:  func(string) (string, error) { return "", exec.ErrNotFound },
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	want := map[string]bool{"credential": false, "sandbox": false, "notifications": false, "voice": false, "mcp": false, "lsp": false}
	for _, check := range report.Checks {
		if _, tracked := want[check.Name]; !tracked || check.Status == StatusPass {
			continue
		}
		if strings.TrimSpace(check.Remediation) == "" {
			t.Fatalf("check %q has no remediation: %#v", check.Name, check)
		}
		want[check.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Fatalf("warning remediation not observed for %q: %#v", name, report.Checks)
		}
	}
}

func TestRunProvidesConfigurationFailureRemediation(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(configFile, []byte("unknown_field: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), Options{ConfigFile: configFile, StateDir: t.TempDir()})
	for _, check := range report.Checks {
		if check.Name == "configuration" {
			if check.Status != StatusFail || !strings.Contains(check.Remediation, "config validate") {
				t.Fatalf("configuration check = %#v", check)
			}
			return
		}
	}
	t.Fatal("configuration check was not reported")
}
