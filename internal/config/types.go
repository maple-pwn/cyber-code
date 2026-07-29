// Package config loads and validates provider profiles without retaining
// resolved credentials.
package config

// Profile configures one model provider. APIKeyEnv names an environment
// variable; its value is resolved only when a provider is constructed.
type Profile struct {
	Provider  string        `json:"provider" yaml:"provider"`
	BaseURL   string        `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Model     string        `json:"model" yaml:"model"`
	APIKeyEnv string        `json:"api_key_env,omitempty" yaml:"api_key_env,omitempty"`
	Pricing   *ModelPricing `json:"pricing,omitempty" yaml:"pricing,omitempty"`
}

// ModelPricing contains user-supplied USD rates per million tokens.
type ModelPricing struct {
	InputPerMillion      float64 `json:"input_per_million" yaml:"input_per_million"`
	OutputPerMillion     float64 `json:"output_per_million" yaml:"output_per_million"`
	CacheReadPerMillion  float64 `json:"cache_read_per_million,omitempty" yaml:"cache_read_per_million,omitempty"`
	CacheWritePerMillion float64 `json:"cache_write_per_million,omitempty" yaml:"cache_write_per_million,omitempty"`
}

// Config is the merged, validated application configuration.
type Config struct {
	ActiveProfile           string             `json:"active_profile" yaml:"active_profile"`
	Profiles                map[string]Profile `json:"profiles" yaml:"profiles"`
	PermissionMode          string             `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
	SandboxMode             string             `json:"sandbox_mode,omitempty" yaml:"sandbox_mode,omitempty"`
	ContextWarningThreshold float64            `json:"context_warning_threshold,omitempty" yaml:"context_warning_threshold,omitempty"`
	ContextCompactThreshold float64            `json:"context_compact_threshold,omitempty" yaml:"context_compact_threshold,omitempty"`
}

// Overrides contains command-line values. Empty fields do not override lower
// precedence layers.
type Overrides struct {
	Profile        string
	PermissionMode string
}

// LoadOptions identifies optional config layers and command-line overrides.
type LoadOptions struct {
	UserFile    string
	ProjectFile string
	CLI         Overrides
}

// Default returns a fresh configuration with no resolved secrets.
func Default() Config {
	return Config{
		ActiveProfile: "anthropic",
		Profiles: map[string]Profile{
			"anthropic": {
				Provider:  "anthropic",
				Model:     "claude-sonnet-4-20250514",
				APIKeyEnv: "ANTHROPIC_API_KEY",
			},
		},
		PermissionMode:          "default",
		SandboxMode:             "best-effort",
		ContextWarningThreshold: 0.80,
		ContextCompactThreshold: 0.90,
	}
}
