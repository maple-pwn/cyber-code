package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"cyber-code/internal/product"

	"gopkg.in/yaml.v3"
)

// Load merges defaults, user config, project config, environment variables,
// and CLI overrides in increasing order of precedence.
func Load(options LoadOptions) (*Config, error) {
	merged := Default()

	for _, layer := range []struct {
		name string
		path string
	}{
		{name: "user", path: options.UserFile},
		{name: "project", path: options.ProjectFile},
	} {
		loaded, err := loadFile(layer.path)
		if err != nil {
			return nil, fmt.Errorf("load %s config: %w", layer.name, err)
		}
		merge(&merged, loaded)
	}

	if value := strings.TrimSpace(os.Getenv(product.EnvProfile)); value != "" {
		merged.ActiveProfile = value
	}
	if value := strings.TrimSpace(os.Getenv(product.EnvPermissionMode)); value != "" {
		merged.PermissionMode = value
	}
	if value := strings.TrimSpace(options.CLI.Profile); value != "" {
		merged.ActiveProfile = value
	}
	cliPermissionMode := strings.TrimSpace(options.CLI.PermissionMode)
	if cliPermissionMode != "" {
		merged.PermissionMode = cliPermissionMode
	}
	if merged.PermissionMode == "bypass" && cliPermissionMode != "bypass" {
		return nil, fmt.Errorf("permission_mode %q requires an explicit CLI override", merged.PermissionMode)
	}

	if err := Validate(&merged); err != nil {
		return nil, err
	}
	return &merged, nil
}

func loadFile(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var loaded Config
	if err := decoder.Decode(&loaded); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, nil
		}
		return Config{}, err
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return Config{}, err
		}
		return Config{}, fmt.Errorf("multiple YAML documents are not supported")
	}
	return loaded, nil
}

func merge(target *Config, overlay Config) {
	if overlay.ActiveProfile != "" {
		target.ActiveProfile = overlay.ActiveProfile
	}
	if overlay.PermissionMode != "" {
		target.PermissionMode = overlay.PermissionMode
	}
	if overlay.SandboxMode != "" {
		target.SandboxMode = overlay.SandboxMode
	}
	if overlay.ContextWarningThreshold != 0 {
		target.ContextWarningThreshold = overlay.ContextWarningThreshold
	}
	if overlay.ContextCompactThreshold != 0 {
		target.ContextCompactThreshold = overlay.ContextCompactThreshold
	}
	if target.Profiles == nil {
		target.Profiles = make(map[string]Profile)
	}
	for name, profile := range overlay.Profiles {
		current := target.Profiles[name]
		if profile.Provider != "" {
			current.Provider = profile.Provider
		}
		if profile.BaseURL != "" {
			current.BaseURL = profile.BaseURL
		}
		if profile.Model != "" {
			current.Model = profile.Model
		}
		if profile.APIKeyEnv != "" {
			current.APIKeyEnv = profile.APIKeyEnv
		}
		if profile.Pricing != nil {
			pricing := *profile.Pricing
			current.Pricing = &pricing
		}
		target.Profiles[name] = current
	}
}
