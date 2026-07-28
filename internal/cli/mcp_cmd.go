package cli

import (
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

type mcpEntry struct {
	Name    string   `json:"name"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
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
	var serverCommand, serverURL string
	var serverArgs []string
	add := &cobra.Command{Use: "add <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if (serverCommand == "") == (serverURL == "") {
			return fmt.Errorf("exactly one of --command or --url is required")
		}
		if serverURL != "" {
			parsed, err := url.Parse(serverURL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("MCP URL must use http or https")
			}
		}
		entries, err := loadMCPEntries(path)
		if err != nil {
			return err
		}
		entries[args[0]] = mcpEntry{Name: args[0], Command: serverCommand, Args: append([]string(nil), serverArgs...), URL: serverURL}
		if err := writeStateFile(path, entries); err != nil {
			return err
		}
		_, err = fmt.Fprintln(environment.stdout, args[0])
		return err
	}}
	add.Flags().StringVar(&serverCommand, "command", "", "stdio server executable")
	add.Flags().StringSliceVar(&serverArgs, "arg", nil, "stdio server argument")
	add.Flags().StringVar(&serverURL, "url", "", "HTTP server URL")
	command.AddCommand(add)
	command.AddCommand(&cobra.Command{Use: "remove <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		entries, err := loadMCPEntries(path)
		if err != nil {
			return err
		}
		if _, ok := entries[args[0]]; !ok {
			return fmt.Errorf("MCP server %q does not exist", args[0])
		}
		delete(entries, args[0])
		return writeStateFile(path, entries)
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
	return command
}

func loadMCPEntries(path string) (map[string]mcpEntry, error) {
	entries := make(map[string]mcpEntry)
	return entries, readStateFile(path, &entries)
}
