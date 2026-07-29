package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"cyber-code/internal/marketplace"
)

type pluginState struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func newPluginsCommand(environment *commandEnvironment) *cobra.Command {
	path := filepath.Join(environment.stateDir, "plugins.json")
	command := &cobra.Command{Use: "plugins", Short: "manage plugins"}
	command.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		plugins, err := loadPluginStates(path)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(plugins))
		for name := range plugins {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			_, _ = fmt.Fprintf(environment.stdout, "%s\t%t\n", name, plugins[name].Enabled)
		}
		return nil
	}})
	for _, enabled := range []bool{true, false} {
		enabled := enabled
		verb := "disable"
		if enabled {
			verb = "enable"
		}
		command.AddCommand(&cobra.Command{Use: verb + " <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			if err := validatePluginName(args[0]); err != nil {
				return err
			}
			return withStateFileLock(path, func() error {
				plugins, err := loadPluginStates(path)
				if err != nil {
					return err
				}
				plugins[args[0]] = pluginState{Name: args[0], Enabled: enabled}
				return writeStateFile(path, plugins)
			})
		}})
	}
	command.AddCommand(newMarketplaceCommand(environment))
	return command
}

func newMarketplaceCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{Use: "marketplace", Short: "manage Claude-compatible plugin marketplaces"}
	manager, err := marketplace.NewManager(environment.stateDir)
	if err != nil {
		command.RunE = func(*cobra.Command, []string) error { return err }
		return command
	}
	command.AddCommand(&cobra.Command{Use: "add <alias> <path-or-https-git-url>", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		added, err := manager.Add(environment.ctx, args[0], args[1])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(environment.stdout, "%s\t%s\t%s\n", added.Alias, added.Name, added.Digest)
		return err
	}})
	command.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		sources, err := manager.List()
		if err != nil {
			return err
		}
		for _, source := range sources {
			if _, err := fmt.Fprintf(environment.stdout, "%s\t%s\t%s\t%s\n", source.Alias, source.Name, source.Revision, source.Digest); err != nil {
				return err
			}
		}
		return nil
	}})
	command.AddCommand(&cobra.Command{Use: "search [query]", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		results, err := manager.Search(query)
		if err != nil {
			return err
		}
		for _, result := range results {
			if _, err := fmt.Fprintf(environment.stdout, "%s@%s\t%s\t%s\n", result.Plugin.Name, result.Marketplace.Alias, result.Plugin.Version, result.Plugin.Description); err != nil {
				return err
			}
		}
		return nil
	}})
	command.AddCommand(&cobra.Command{Use: "install <plugin@marketplace>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		name, alias, found := strings.Cut(args[0], "@")
		if !found || name == "" || alias == "" || strings.Contains(alias, "@") {
			return fmt.Errorf("install requires plugin@marketplace")
		}
		installed, err := manager.Install(environment.ctx, alias, name)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(environment.stdout, "%s\t%s\t%s\n", installed.Name, installed.Version, installed.Digest)
		return err
	}})
	command.AddCommand(&cobra.Command{Use: "remove <plugin>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if err := manager.Remove(args[0]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(environment.stdout, "%s removed\n", args[0])
		return err
	}})
	return command
}

func loadPluginStates(path string) (map[string]pluginState, error) {
	plugins := make(map[string]pluginState)
	return plugins, readStateFile(path, &plugins)
}
