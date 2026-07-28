package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"cyber-code/internal/lsp"
)

type lspEntry struct {
	Command               string            `json:"command"`
	Args                  []string          `json:"args,omitempty"`
	Environment           map[string]string `json:"environment,omitempty"`
	InitializationOptions json.RawMessage   `json:"initialization_options,omitempty"`
}

func loadLSPConfigs(path string) ([]lsp.ServerConfig, error) {
	entries := make(map[string]lspEntry)
	if err := readStateFile(path, &entries); err != nil {
		return nil, err
	}
	languages := make([]string, 0, len(entries))
	for language := range entries {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	configs := make([]lsp.ServerConfig, 0, len(languages))
	for _, language := range languages {
		entry := entries[language]
		language = strings.ToLower(strings.TrimSpace(language))
		if language == "" || strings.TrimSpace(entry.Command) == "" {
			return nil, fmt.Errorf("LSP language and command are required")
		}
		var initialization any
		if len(entry.InitializationOptions) > 0 {
			if !json.Valid(entry.InitializationOptions) {
				return nil, fmt.Errorf("LSP language %q initialization options are invalid", language)
			}
			initialization = json.RawMessage(append([]byte(nil), entry.InitializationOptions...))
		}
		configs = append(configs, lsp.ServerConfig{
			Language: language, Command: entry.Command, Args: append([]string(nil), entry.Args...),
			Environment: entry.Environment, InitializationOptions: initialization,
		})
	}
	return configs, nil
}
