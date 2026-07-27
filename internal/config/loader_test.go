package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.yaml")
	projectFile := filepath.Join(dir, "project.yaml")
	writeConfigFile(t, userFile, `
active_profile: user-profile
permission_mode: default
profiles:
  user-profile:
    provider: anthropic
    model: user-model
  env-profile:
    provider: anthropic
    model: env-model
  cli-profile:
    provider: openai
    base_url: http://localhost:8000/v1
    model: cli-model
`)
	writeConfigFile(t, projectFile, `
active_profile: project-profile
permission_mode: plan
profiles:
  project-profile:
    provider: anthropic
    model: project-model
`)
	t.Setenv("CLAUDE_GO_PROFILE", "env-profile")
	t.Setenv("CLAUDE_GO_PERMISSION_MODE", "plan")

	got, err := Load(LoadOptions{
		UserFile:    userFile,
		ProjectFile: projectFile,
		CLI: Overrides{
			Profile:        "cli-profile",
			PermissionMode: "accept-edits",
		},
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got.ActiveProfile != "cli-profile" {
		t.Fatalf("active profile = %q, want CLI override", got.ActiveProfile)
	}
	if got.PermissionMode != "accept-edits" {
		t.Fatalf("permission mode = %q, want CLI override", got.PermissionMode)
	}
	if got.Profiles["project-profile"].Model != "project-model" {
		t.Fatalf("project profile was not merged: %#v", got.Profiles)
	}
}

func TestLoadUsesProjectThenUserThenDefaults(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.yaml")
	projectFile := filepath.Join(dir, "project.yaml")
	writeConfigFile(t, userFile, `
active_profile: user-profile
profiles:
  user-profile:
    provider: anthropic
    model: user-model
`)
	writeConfigFile(t, projectFile, `
active_profile: project-profile
profiles:
  project-profile:
    provider: anthropic
    model: project-model
`)
	t.Setenv("CLAUDE_GO_PROFILE", "")
	t.Setenv("CLAUDE_GO_PERMISSION_MODE", "")

	project, err := Load(LoadOptions{UserFile: userFile, ProjectFile: projectFile})
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}
	if project.ActiveProfile != "project-profile" {
		t.Fatalf("active profile = %q, want project-profile", project.ActiveProfile)
	}

	user, err := Load(LoadOptions{UserFile: userFile})
	if err != nil {
		t.Fatalf("load user config: %v", err)
	}
	if user.ActiveProfile != "user-profile" {
		t.Fatalf("active profile = %q, want user-profile", user.ActiveProfile)
	}

	defaults, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if defaults.ActiveProfile != "anthropic" || defaults.PermissionMode != "default" {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}
}

func TestLoadRejectsUnknownYAMLFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFile(t, path, `
active_profile: anthropic
unknown_setting: true
profiles:
  anthropic:
    provider: anthropic
    model: model
`)

	_, err := Load(LoadOptions{UserFile: path})
	if err == nil {
		t.Fatal("expected unknown YAML field to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown_setting") {
		t.Fatalf("error does not identify unknown field: %v", err)
	}
}

func TestLoadValidatesProfileReferenceBaseURLAndPermissionMode(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "missing active profile",
			yaml: "active_profile: missing\nprofiles: {}\n",
			want: "missing",
		},
		{
			name: "unsupported base URL scheme",
			yaml: "active_profile: local\nprofiles:\n  local:\n    provider: openai\n    base_url: ftp://localhost/v1\n    model: model\n",
			want: "base_url",
		},
		{
			name: "invalid permission mode",
			yaml: "active_profile: anthropic\npermission_mode: unrestricted\nprofiles:\n  anthropic:\n    provider: anthropic\n    model: model\n",
			want: "permission_mode",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeConfigFile(t, path, test.yaml)
			_, err := Load(LoadOptions{UserFile: path})
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("error %q does not contain %q", err, test.want)
			}
		})
	}
}

func TestLoadAllowsBypassModeOnlyFromExplicitCLIOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFile(t, path, `
active_profile: anthropic
permission_mode: bypass
profiles:
  anthropic:
    provider: anthropic
    model: model
`)

	if _, err := Load(LoadOptions{ProjectFile: path}); err == nil {
		t.Fatal("expected project config bypass mode to be rejected")
	}

	got, err := Load(LoadOptions{
		ProjectFile: path,
		CLI:         Overrides{PermissionMode: "bypass"},
	})
	if err != nil {
		t.Fatalf("load explicit CLI bypass mode: %v", err)
	}
	if got.PermissionMode != "bypass" {
		t.Fatalf("permission mode = %q, want bypass", got.PermissionMode)
	}
}

func TestResolveCredentialReadsEnvironmentOnDemandWithoutPersistingSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFile(t, path, `
active_profile: local
profiles:
  local:
    provider: openai
    base_url: http://localhost:8000/v1
    model: model
    api_key_env: TEST_PROVIDER_API_KEY
`)
	t.Setenv("TEST_PROVIDER_API_KEY", "first-secret")

	got, err := Load(LoadOptions{UserFile: path})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	serialized, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if strings.Contains(string(serialized), "first-secret") {
		t.Fatalf("serialized config contains resolved credential: %s", serialized)
	}

	t.Setenv("TEST_PROVIDER_API_KEY", "second-secret")
	credential, err := ResolveCredential(got.Profiles[got.ActiveProfile])
	if err != nil {
		t.Fatalf("resolve credential: %v", err)
	}
	if credential != "second-secret" {
		t.Fatalf("credential = %q, want current environment value", credential)
	}
}

func TestResolveCredentialReportsMissingEnvironmentVariableWithoutSecret(t *testing.T) {
	t.Setenv("MISSING_PROVIDER_API_KEY", "")
	_, err := ResolveCredential(Profile{APIKeyEnv: "MISSING_PROVIDER_API_KEY"})
	if err == nil {
		t.Fatal("expected missing credential error")
	}
	if !strings.Contains(err.Error(), "MISSING_PROVIDER_API_KEY") {
		t.Fatalf("error does not identify missing environment variable: %v", err)
	}
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
