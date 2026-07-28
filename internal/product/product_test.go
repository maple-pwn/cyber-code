package product

import (
	"strings"
	"testing"
)

func TestCanonicalBrandManifest(t *testing.T) {
	if Name != "cyber-code" || Command != Name || ConfigDirectory != Name || EnvPrefix != "CYBER_CODE" {
		t.Fatalf("brand manifest = name:%q command:%q config:%q env:%q", Name, Command, ConfigDirectory, EnvPrefix)
	}
	if EnvConfig != "CYBER_CODE_CONFIG" || EnvStateDir != "CYBER_CODE_STATE_DIR" ||
		EnvProfile != "CYBER_CODE_PROFILE" || EnvPermissionMode != "CYBER_CODE_PERMISSION_MODE" {
		t.Fatalf("environment manifest = %q %q %q %q", EnvConfig, EnvStateDir, EnvProfile, EnvPermissionMode)
	}
	identity := strings.ToLower(DefaultSystemPrompt)
	for _, required := range []string{Name, "independent", "coding agent", "do not claim"} {
		if !strings.Contains(identity, required) {
			t.Fatalf("system identity missing %q: %q", required, DefaultSystemPrompt)
		}
	}
}
