package cli

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
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
			plugins, err := loadPluginStates(path)
			if err != nil {
				return err
			}
			plugins[args[0]] = pluginState{Name: args[0], Enabled: enabled}
			return writeStateFile(path, plugins)
		}})
	}
	return command
}

func loadPluginStates(path string) (map[string]pluginState, error) {
	plugins := make(map[string]pluginState)
	return plugins, readStateFile(path, &plugins)
}
