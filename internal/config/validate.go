package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var allowedPermissionModes = map[string]struct{}{
	"default":      {},
	"plan":         {},
	"accept-edits": {},
	"bypass":       {},
}

var allowedSandboxModes = map[string]struct{}{"off": {}, "best-effort": {}, "required": {}}

// Validate checks profile references and values without resolving secrets.
func Validate(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}

	activeProfile := strings.TrimSpace(config.ActiveProfile)
	if activeProfile == "" {
		return fmt.Errorf("active_profile is required")
	}
	if _, ok := config.Profiles[activeProfile]; !ok {
		return fmt.Errorf("active_profile %q does not reference a configured profile", activeProfile)
	}
	config.ActiveProfile = activeProfile

	for name, profile := range config.Profiles {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("profile name must not be empty")
		}
		if strings.TrimSpace(profile.Provider) == "" {
			return fmt.Errorf("profile %q provider is required", name)
		}
		if strings.TrimSpace(profile.Model) == "" {
			return fmt.Errorf("profile %q model is required", name)
		}
		if profile.BaseURL != "" {
			parsed, err := url.Parse(profile.BaseURL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("profile %q base_url must use http or https and include a host", name)
			}
		}
		if profile.APIKeyEnv != "" && !environmentNamePattern.MatchString(profile.APIKeyEnv) {
			return fmt.Errorf("profile %q api_key_env is not a valid environment variable name", name)
		}
	}

	if _, ok := allowedPermissionModes[config.PermissionMode]; !ok {
		return fmt.Errorf("permission_mode %q is not supported", config.PermissionMode)
	}
	if _, ok := allowedSandboxModes[config.SandboxMode]; !ok {
		return fmt.Errorf("sandbox_mode %q is not supported", config.SandboxMode)
	}
	return nil
}

// ResolveCredential reads the selected profile's credential on demand. The
// returned value is never written back into Config.
func ResolveCredential(profile Profile) (string, error) {
	if profile.APIKeyEnv == "" {
		return "", nil
	}
	if !environmentNamePattern.MatchString(profile.APIKeyEnv) {
		return "", fmt.Errorf("api_key_env %q is not a valid environment variable name", profile.APIKeyEnv)
	}
	credential, ok := os.LookupEnv(profile.APIKeyEnv)
	if !ok || strings.TrimSpace(credential) == "" {
		return "", fmt.Errorf("credential environment variable %s is not set", profile.APIKeyEnv)
	}
	return credential, nil
}
