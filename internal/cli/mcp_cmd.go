package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"cyber-code/internal/mcp"
)

type mcpEntry struct {
	Name                 string            `json:"name"`
	Command              string            `json:"command,omitempty"`
	Args                 []string          `json:"args,omitempty"`
	URL                  string            `json:"url,omitempty"`
	HeaderEnv            map[string]string `json:"header_env,omitempty"`
	Disabled             bool              `json:"disabled,omitempty"`
	OAuthTokenURL        string            `json:"oauth_token_url,omitempty"`
	OAuthClientID        string            `json:"oauth_client_id,omitempty"`
	OAuthClientSecretEnv string            `json:"oauth_client_secret_env,omitempty"`
}

func newMCPCommand(environment *commandEnvironment) *cobra.Command {
	path := filepath.Join(environment.stateDir, "mcp.json")
	command := &cobra.Command{Use: "mcp", Short: "manage MCP servers"}
	command.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		entries, err := loadMCPEntries(path)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			_, _ = fmt.Fprintln(environment.stdout, name)
		}
		return nil
	}})
	var serverCommand, serverURL, oauthTokenURL, oauthClientID, oauthClientSecretEnv string
	var serverArgs []string
	var serverHeaderEnv []string
	add := &cobra.Command{Use: "add <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if (serverCommand == "") == (serverURL == "") {
			return fmt.Errorf("exactly one of --command or --url is required")
		}
		if serverURL != "" {
			parsed, err := url.Parse(serverURL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("MCP URL must use http or https")
			}
		} else if len(serverHeaderEnv) > 0 {
			return fmt.Errorf("MCP headers require an HTTP server URL")
		}
		headerEnv, err := parseHeaderEnvironment(serverHeaderEnv)
		if err != nil {
			return err
		}
		if err := withStateFileLock(path, func() error {
			entries, err := loadMCPEntries(path)
			if err != nil {
				return err
			}
			entries[args[0]] = mcpEntry{Name: args[0], Command: serverCommand, Args: append([]string(nil), serverArgs...), URL: serverURL, HeaderEnv: headerEnv, OAuthTokenURL: oauthTokenURL, OAuthClientID: oauthClientID, OAuthClientSecretEnv: oauthClientSecretEnv}
			return writeStateFile(path, entries)
		}); err != nil {
			return err
		}
		_, err = fmt.Fprintln(environment.stdout, args[0])
		return err
	}}
	add.Flags().StringVar(&serverCommand, "command", "", "stdio server executable")
	add.Flags().StringSliceVar(&serverArgs, "arg", nil, "stdio server argument")
	add.Flags().StringVar(&serverURL, "url", "", "HTTP server URL")
	add.Flags().StringSliceVar(&serverHeaderEnv, "header-env", nil, "HTTP header mapped to an environment variable (Header=ENV_VAR)")
	add.Flags().StringVar(&oauthTokenURL, "oauth-token-url", "", "OAuth token refresh endpoint")
	add.Flags().StringVar(&oauthClientID, "oauth-client-id", "", "OAuth client ID")
	add.Flags().StringVar(&oauthClientSecretEnv, "oauth-client-secret-env", "", "environment variable containing OAuth client secret")
	command.AddCommand(add)
	command.AddCommand(&cobra.Command{Use: "remove <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return withStateFileLock(path, func() error {
			entries, err := loadMCPEntries(path)
			if err != nil {
				return err
			}
			if _, ok := entries[args[0]]; !ok {
				return fmt.Errorf("MCP server %q does not exist", args[0])
			}
			delete(entries, args[0])
			return writeStateFile(path, entries)
		})
	}})
	command.AddCommand(&cobra.Command{Use: "test <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		entries, err := loadMCPEntries(path)
		if err != nil {
			return err
		}
		entry, ok := entries[args[0]]
		if !ok {
			return fmt.Errorf("MCP server %q does not exist", args[0])
		}
		if entry.Command != "" {
			if _, err := exec.LookPath(entry.Command); err != nil {
				return fmt.Errorf("MCP executable is unavailable: %w", err)
			}
		}
		_, err = fmt.Fprintln(environment.stdout, "configuration is valid")
		return err
	}})
	setDisabled := func(disabled bool) *cobra.Command {
		verb := "enable"
		if disabled {
			verb = "disable"
		}
		return &cobra.Command{Use: verb + " <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			if err := withStateFileLock(path, func() error {
				entries, err := loadMCPEntries(path)
				if err != nil {
					return err
				}
				entry, ok := entries[args[0]]
				if !ok {
					return fmt.Errorf("MCP server %q does not exist", args[0])
				}
				entry.Disabled = disabled
				entries[args[0]] = entry
				return writeStateFile(path, entries)
			}); err != nil {
				return err
			}
			_, err := fmt.Fprintln(environment.stdout, verb+"d")
			return err
		}}
	}
	command.AddCommand(setDisabled(true), setDisabled(false))
	command.AddCommand(&cobra.Command{Use: "status <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		entries, err := loadMCPEntries(path)
		if err != nil {
			return err
		}
		entry, ok := entries[args[0]]
		if !ok {
			return fmt.Errorf("MCP server %q does not exist", args[0])
		}
		state := "enabled"
		if entry.Disabled {
			state = "disabled"
		}
		_, err = fmt.Fprintf(environment.stdout, "%s: %s\n", args[0], state)
		return err
	}})
	credentialStore, err := mcp.NewCredentialStore(filepath.Join(environment.stateDir, "mcp-credentials"))
	if err == nil {
		auth := &cobra.Command{Use: "auth", Short: "manage MCP OAuth credentials"}
		var accessEnv, refreshEnv, tokenType string
		var expiresAt int64
		set := &cobra.Command{Use: "set <name>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			entries, loadErr := loadMCPEntries(path)
			if loadErr != nil {
				return loadErr
			}
			if _, ok := entries[args[0]]; !ok {
				return fmt.Errorf("MCP server %q does not exist", args[0])
			}
			if !validEnvironmentName(accessEnv) {
				return fmt.Errorf("access token environment variable is invalid")
			}
			access, ok := os.LookupEnv(accessEnv)
			if !ok || strings.TrimSpace(access) == "" {
				return fmt.Errorf("access token environment variable %s is not set", accessEnv)
			}
			refresh := ""
			if refreshEnv != "" {
				if !validEnvironmentName(refreshEnv) {
					return fmt.Errorf("refresh token environment variable is invalid")
				}
				refresh, ok = os.LookupEnv(refreshEnv)
				if !ok {
					return fmt.Errorf("refresh token environment variable %s is not set", refreshEnv)
				}
			}
			if tokenType == "" {
				tokenType = "Bearer"
			}
			if putErr := credentialStore.Put(cmd.Context(), args[0], mcp.Credential{AccessToken: access, RefreshToken: refresh, TokenType: tokenType, ExpiresAt: expiresAt}); putErr != nil {
				return putErr
			}
			_, writeErr := fmt.Fprintln(environment.stdout, "credential stored")
			return writeErr
		}}
		set.Flags().StringVar(&accessEnv, "access-token-env", "", "environment variable containing the access token")
		set.Flags().StringVar(&refreshEnv, "refresh-token-env", "", "environment variable containing the refresh token")
		set.Flags().StringVar(&tokenType, "token-type", "Bearer", "authorization token type")
		set.Flags().Int64Var(&expiresAt, "expires-at", 0, "Unix access token expiry")
		auth.AddCommand(set)
		auth.AddCommand(&cobra.Command{Use: "remove <name>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error { return credentialStore.Delete(cmd.Context(), args[0]) }})
		command.AddCommand(auth)
	}
	return command
}

func parseHeaderEnvironment(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("MCP header environment mapping must use Header=ENV_VAR")
		}
		header := http.CanonicalHeaderKey(strings.TrimSpace(parts[0]))
		environment := strings.TrimSpace(parts[1])
		if header == "" || header == "Host" || header == "Content-Length" {
			return nil, fmt.Errorf("MCP header name is invalid or restricted")
		}
		if !validEnvironmentName(environment) {
			return nil, fmt.Errorf("MCP header environment variable name %q is invalid", environment)
		}
		result[header] = environment
	}
	return result, nil
}

func validEnvironmentName(name string) bool {
	if name == "" || !((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z') || name[0] == '_') {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '_' {
			continue
		}
		return false
	}
	return true
}

func loadMCPEntries(path string) (map[string]mcpEntry, error) {
	entries := make(map[string]mcpEntry)
	return entries, readStateFile(path, &entries)
}
