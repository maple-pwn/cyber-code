package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	configpkg "cyber-code/internal/config"
	"cyber-code/internal/product"
	"cyber-code/internal/session"
)

type sessionMetadata struct {
	ID      string    `json:"id"`
	Profile string    `json:"profile,omitempty"`
	Model   string    `json:"model,omitempty"`
	Updated time.Time `json:"updated"`
}

type sessionListEntry struct {
	ID           string    `json:"id"`
	Profile      string    `json:"profile,omitempty"`
	Model        string    `json:"model,omitempty"`
	Updated      time.Time `json:"updated"`
	LastSequence uint64    `json:"last_sequence"`
	MessageCount int       `json:"message_count"`
	Summary      string    `json:"summary,omitempty"`
}

func newSessionsCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{Use: "sessions", Short: "manage saved sessions"}
	var jsonOutput bool
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		entries, err := loadSessionIndex(environment.stateDir)
		if err != nil {
			return err
		}
		store, err := session.NewStore(filepath.Join(environment.stateDir, "sessions"), session.StoreOptions{})
		if err != nil {
			return err
		}
		items := make([]sessionListEntry, 0, len(entries))
		for id, metadata := range entries {
			item := sessionListEntry{ID: id, Profile: metadata.Profile, Model: metadata.Model, Updated: metadata.Updated}
			if snapshot, metadataErr := store.SnapshotMetadata(environment.ctx, id); metadataErr == nil {
				item.LastSequence = snapshot.LastSequence
				item.MessageCount = snapshot.MessageCount
				item.Summary = snapshot.Summary
				if !snapshot.UpdatedAt.IsZero() {
					item.Updated = snapshot.UpdatedAt
				}
			}
			items = append(items, item)
		}
		sort.Slice(items, func(left, right int) bool {
			if items[left].Updated.Equal(items[right].Updated) {
				return items[left].ID < items[right].ID
			}
			return items[left].Updated.After(items[right].Updated)
		})
		encoder := json.NewEncoder(environment.stdout)
		for _, item := range items {
			if jsonOutput {
				if err := encoder.Encode(item); err != nil {
					return err
				}
				continue
			}
			writeSessionListItem(environment.stdout, item, terminalColumns())
		}
		return nil
	}}
	list.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON object per session")
	command.AddCommand(list)
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
		secrets, err := configuredExportSecrets(environment.configFile)
		if err != nil {
			return err
		}
		return store.Export(environment.ctx, args[0], writer, session.ExportOptions{Secrets: secrets})
	}}
	export.Flags().StringVarP(&destination, "output", "o", "", "output file")
	command.AddCommand(export)
	command.AddCommand(&cobra.Command{Use: "delete <id>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		store, err := session.NewStore(filepath.Join(environment.stateDir, "sessions"), session.StoreOptions{})
		if err != nil {
			return err
		}
		lease, err := store.AcquireLease(args[0])
		if err != nil {
			return err
		}
		defer lease.Close()
		path := filepath.Join(environment.stateDir, "sessions.json")
		return withStateFileLock(path, func() error {
			entries, err := loadSessionIndex(environment.stateDir)
			if err != nil {
				return err
			}
			if _, ok := entries[args[0]]; !ok {
				return fmt.Errorf("session %q does not exist", args[0])
			}
			metadata := entries[args[0]]
			delete(entries, args[0])
			if err := writeStateFile(path, entries); err != nil {
				return err
			}
			if err := store.Delete(environment.ctx, args[0]); err != nil {
				entries[args[0]] = metadata
				return errors.Join(err, writeStateFile(path, entries))
			}
			return nil
		})
	}})
	return command
}

func terminalColumns() int {
	if columns, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && columns > 0 {
		return columns
	}
	return 120
}

func writeSessionListItem(writer interface{ Write([]byte) (int, error) }, item sessionListEntry, width int) {
	width = max(width, 20)
	line := fmt.Sprintf("%-24s  %-12s  %-20s  messages=%-4d  sequence=%-6d  %s",
		item.ID, item.Profile, item.Model, item.MessageCount, item.LastSequence, item.Updated.Format(time.RFC3339))
	_, _ = fmt.Fprintln(writer, ansi.Truncate(strings.TrimRight(line, " "), width, ""))
	if item.Summary != "" {
		_, _ = fmt.Fprintln(writer, ansi.Truncate("  summary: "+item.Summary, width, ""))
	}
}

func configuredExportSecrets(configFile string) ([]string, error) {
	projectFile := ""
	if cwd, err := os.Getwd(); err == nil {
		projectFile = filepath.Join(cwd, "."+product.Name+".yaml")
	}
	loaded, err := configpkg.Load(configpkg.LoadOptions{UserFile: configFile, ProjectFile: projectFile})
	if err != nil {
		return nil, fmt.Errorf("load export redaction credentials: %w", err)
	}
	seen := make(map[string]struct{})
	secrets := make([]string, 0, len(loaded.Profiles))
	for _, profile := range loaded.Profiles {
		addExportSecretFromEnvironment(profile.APIKeyEnv, seen, &secrets)
	}
	for _, name := range []string{
		"AWS_BEARER_TOKEN_BEDROCK", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"ANTHROPIC_FOUNDRY_API_KEY", "AZURE_CLIENT_SECRET",
	} {
		addExportSecretFromEnvironment(name, seen, &secrets)
	}
	return secrets, nil
}

func addExportSecretFromEnvironment(name string, seen map[string]struct{}, secrets *[]string) {
	if name == "" {
		return
	}
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return
	}
	if _, duplicate := seen[value]; duplicate {
		return
	}
	seen[value] = struct{}{}
	*secrets = append(*secrets, value)
}

func recordSession(stateDir string, metadata sessionMetadata) error {
	path := filepath.Join(stateDir, "sessions.json")
	return withStateFileLock(path, func() error {
		entries, err := loadSessionIndex(stateDir)
		if err != nil {
			return err
		}
		metadata.Updated = time.Now().UTC()
		entries[metadata.ID] = metadata
		return writeStateFile(path, entries)
	})
}

func loadSessionIndex(stateDir string) (map[string]sessionMetadata, error) {
	entries := make(map[string]sessionMetadata)
	return entries, readStateFile(filepath.Join(stateDir, "sessions.json"), &entries)
}
