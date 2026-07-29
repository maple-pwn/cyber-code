package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	configpkg "cyber-code/internal/config"
	"cyber-code/internal/session"
)

func newConfigCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "manage provider configuration"}
	command.AddCommand(newConfigProfileCommand(environment))
	command.AddCommand(&cobra.Command{
		Use: "list", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			loaded, err := loadCommandConfig(environment.configFile)
			if err != nil {
				return err
			}
			encoded, err := yaml.Marshal(loaded)
			if err == nil {
				_, err = environment.stdout.Write(encoded)
			}
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use: "get <key>", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			loaded, err := loadCommandConfig(environment.configFile)
			if err != nil {
				return err
			}
			value, err := configValue(loaded, args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(environment.stdout, value)
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use: "set <key> <value>", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := withStateFileLock(environment.configFile, func() error {
				loaded, err := loadCommandConfig(environment.configFile)
				if err != nil {
					return err
				}
				if err := setConfigValue(loaded, args[0], args[1]); err != nil {
					return err
				}
				if err := configpkg.Validate(loaded); err != nil {
					return err
				}
				return saveCommandConfig(environment.configFile, loaded)
			}); err != nil {
				return err
			}
			_, err := fmt.Fprintln(environment.stdout, args[1])
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use: "validate", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := loadCommandConfig(environment.configFile); err != nil {
				return err
			}
			_, err := fmt.Fprintln(environment.stdout, "configuration is valid")
			return err
		},
	})
	return command
}

func newConfigProfileCommand(environment *commandEnvironment) *cobra.Command {
	profiles := &cobra.Command{Use: "profile", Short: "manage complete provider profiles"}
	var providerName, baseURL, model, apiKeyEnv string
	var inputCost, outputCost, cacheReadCost, cacheWriteCost float64
	var activate bool
	set := &cobra.Command{
		Use: "set <name>", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := withStateFileLock(environment.configFile, func() error {
				loaded, err := loadCommandConfig(environment.configFile)
				if err != nil {
					return err
				}
				profile := loaded.Profiles[args[0]]
				if command.Flags().Changed("provider") {
					profile.Provider = providerName
				}
				if command.Flags().Changed("base-url") {
					profile.BaseURL = baseURL
				}
				if command.Flags().Changed("model") {
					profile.Model = model
				}
				if command.Flags().Changed("api-key-env") {
					profile.APIKeyEnv = apiKeyEnv
				}
				if command.Flags().Changed("input-cost-per-million") || command.Flags().Changed("output-cost-per-million") ||
					command.Flags().Changed("cache-read-cost-per-million") || command.Flags().Changed("cache-write-cost-per-million") {
					pricing := configpkg.ModelPricing{}
					if profile.Pricing != nil {
						pricing = *profile.Pricing
					}
					if command.Flags().Changed("input-cost-per-million") {
						pricing.InputPerMillion = inputCost
					}
					if command.Flags().Changed("output-cost-per-million") {
						pricing.OutputPerMillion = outputCost
					}
					if command.Flags().Changed("cache-read-cost-per-million") {
						pricing.CacheReadPerMillion = cacheReadCost
					}
					if command.Flags().Changed("cache-write-cost-per-million") {
						pricing.CacheWritePerMillion = cacheWriteCost
					}
					profile.Pricing = &pricing
				}
				loaded.Profiles[args[0]] = profile
				if activate {
					loaded.ActiveProfile = args[0]
				}
				if err := configpkg.Validate(loaded); err != nil {
					return err
				}
				return saveCommandConfig(environment.configFile, loaded)
			}); err != nil {
				return err
			}
			_, err := fmt.Fprintln(environment.stdout, args[0])
			return err
		},
	}
	set.Flags().StringVar(&providerName, "provider", "", "provider type")
	set.Flags().StringVar(&baseURL, "base-url", "", "provider base URL")
	set.Flags().StringVar(&model, "model", "", "model name")
	set.Flags().StringVar(&apiKeyEnv, "api-key-env", "", "credential environment variable name")
	set.Flags().Float64Var(&inputCost, "input-cost-per-million", 0, "input token price in USD per million")
	set.Flags().Float64Var(&outputCost, "output-cost-per-million", 0, "output token price in USD per million")
	set.Flags().Float64Var(&cacheReadCost, "cache-read-cost-per-million", 0, "cache-read token price in USD per million")
	set.Flags().Float64Var(&cacheWriteCost, "cache-write-cost-per-million", 0, "cache-write token price in USD per million")
	set.Flags().BoolVar(&activate, "activate", false, "make this the active profile")
	profiles.AddCommand(set)
	return profiles
}

func loadCommandConfig(path string) (*configpkg.Config, error) {
	return configpkg.Load(configpkg.LoadOptions{UserFile: path})
}

func configValue(config *configpkg.Config, key string) (string, error) {
	switch key {
	case "active_profile":
		return config.ActiveProfile, nil
	case "permission_mode":
		return config.PermissionMode, nil
	case "context_warning_threshold":
		return strconv.FormatFloat(config.ContextWarningThreshold, 'f', -1, 64), nil
	case "context_compact_threshold":
		return strconv.FormatFloat(config.ContextCompactThreshold, 'f', -1, 64), nil
	}
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] != "profiles" {
		return "", fmt.Errorf("unknown configuration key %q", key)
	}
	profile, ok := config.Profiles[parts[1]]
	if !ok {
		return "", fmt.Errorf("profile %q does not exist", parts[1])
	}
	switch parts[2] {
	case "provider":
		return profile.Provider, nil
	case "base_url":
		return profile.BaseURL, nil
	case "model":
		return profile.Model, nil
	case "api_key_env":
		return profile.APIKeyEnv, nil
	default:
		return "", fmt.Errorf("unknown profile key %q", parts[2])
	}
}

func setConfigValue(config *configpkg.Config, key, value string) error {
	switch key {
	case "active_profile":
		config.ActiveProfile = value
		return nil
	case "permission_mode":
		config.PermissionMode = value
		return nil
	case "context_warning_threshold", "context_compact_threshold":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s must be a number: %w", key, err)
		}
		if key == "context_warning_threshold" {
			config.ContextWarningThreshold = parsed
		} else {
			config.ContextCompactThreshold = parsed
		}
		return nil
	}
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] != "profiles" {
		return fmt.Errorf("unknown configuration key %q", key)
	}
	profile := config.Profiles[parts[1]]
	switch parts[2] {
	case "provider":
		profile.Provider = value
	case "base_url":
		profile.BaseURL = value
	case "model":
		profile.Model = value
	case "api_key_env":
		profile.APIKeyEnv = value
	default:
		return fmt.Errorf("unknown profile key %q", parts[2])
	}
	config.Profiles[parts[1]] = profile
	return nil
}

func saveCommandConfig(path string, config *configpkg.Config) error {
	encoded, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := session.RestrictPrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	name := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(name)
		}
	}()
	if err := session.RestrictPrivateFile(name); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceCLIFile(name, path); err != nil {
		return err
	}
	keep = true
	return session.RestrictPrivateFile(path)
}
