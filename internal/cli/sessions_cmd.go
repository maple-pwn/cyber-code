package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"claude-code-go/internal/session"
)

type sessionMetadata struct {
	ID      string    `json:"id"`
	Profile string    `json:"profile,omitempty"`
	Model   string    `json:"model,omitempty"`
	Updated time.Time `json:"updated"`
}

func newSessionsCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{Use: "sessions", Short: "manage saved sessions"}
	command.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		entries, err := loadSessionIndex(environment.stateDir)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(entries))
		for id := range entries {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			_, _ = fmt.Fprintln(environment.stdout, id)
		}
		return nil
	}})
	command.AddCommand(&cobra.Command{Use: "resume <id>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		store, err := session.NewStore(filepath.Join(environment.stateDir, "sessions"), session.StoreOptions{})
		if err != nil {
			return err
		}
		snapshot, err := store.Resume(environment.ctx, args[0])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(environment.stdout, "%s\t%d\n", snapshot.SessionID, snapshot.LastSequence)
		return err
	}})
	var destination string
	export := &cobra.Command{Use: "export <id>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		store, err := session.NewStore(filepath.Join(environment.stateDir, "sessions"), session.StoreOptions{})
		if err != nil {
			return err
		}
		writer := environment.stdout
		var file *os.File
		if destination != "" {
			file, err = os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			defer file.Close()
			writer = file
		}
		return store.Export(environment.ctx, args[0], writer, session.ExportOptions{})
	}}
	export.Flags().StringVarP(&destination, "output", "o", "", "output file")
	command.AddCommand(export)
	command.AddCommand(&cobra.Command{Use: "delete <id>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		entries, err := loadSessionIndex(environment.stateDir)
		if err != nil {
			return err
		}
		if _, ok := entries[args[0]]; !ok {
			return fmt.Errorf("session %q does not exist", args[0])
		}
		store, err := session.NewStore(filepath.Join(environment.stateDir, "sessions"), session.StoreOptions{})
		if err != nil {
			return err
		}
		if err := store.Delete(environment.ctx, args[0]); err != nil {
			return err
		}
		delete(entries, args[0])
		return writeStateFile(filepath.Join(environment.stateDir, "sessions.json"), entries)
	}})
	return command
}

func recordSession(stateDir string, metadata sessionMetadata) error {
	entries, err := loadSessionIndex(stateDir)
	if err != nil {
		return err
	}
	metadata.Updated = time.Now().UTC()
	entries[metadata.ID] = metadata
	return writeStateFile(filepath.Join(stateDir, "sessions.json"), entries)
}

func loadSessionIndex(stateDir string) (map[string]sessionMetadata, error) {
	entries := make(map[string]sessionMetadata)
	return entries, readStateFile(filepath.Join(stateDir, "sessions.json"), &entries)
}
