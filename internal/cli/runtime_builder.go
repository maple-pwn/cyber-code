package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-code-go/internal/agent"
	configpkg "claude-code-go/internal/config"
	"claude-code-go/internal/permissions"
	"claude-code-go/internal/platform"
	"claude-code-go/internal/provider"
	"claude-code-go/internal/provider/anthropic"
	"claude-code-go/internal/provider/openai"
	runtimepkg "claude-code-go/internal/runtime"
	"claude-code-go/internal/session"
	"claude-code-go/internal/tool"
	"claude-code-go/internal/tool/builtin"
)

type compositionOptions struct {
	ConfigFile, StateDir, Profile, PermissionMode, Model, Cwd string
	MaxTurns                                                  int
	Headless                                                  bool
	Confirmer                                                 permissions.Confirmer
}

func composeRuntime(_ context.Context, options compositionOptions) (*runtimepkg.Runtime, error) {
	loaded, err := configpkg.Load(configpkg.LoadOptions{
		UserFile: options.ConfigFile,
		CLI:      configpkg.Overrides{Profile: options.Profile, PermissionMode: options.PermissionMode},
	})
	if err != nil {
		return nil, err
	}
	profile := loaded.Profiles[loaded.ActiveProfile]
	if options.Model != "" {
		profile.Model = options.Model
	}
	modelProvider, err := buildProvider(profile)
	if err != nil {
		return nil, err
	}
	workspace := options.Cwd
	if workspace == "" {
		workspace, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	mode := permissions.PermissionMode(loaded.PermissionMode)
	source := permissions.SourceUserSettings
	if options.PermissionMode != "" {
		source = permissions.SourceCliArg
	}
	broker, err := permissions.NewBroker(permissions.Options{
		Mode: mode, ModeSource: source, Headless: options.Headless, Confirmer: options.Confirmer,
	})
	if err != nil {
		return nil, err
	}
	registry := tool.NewRegistry()
	for _, registered := range []tool.Tool{
		builtin.NewReadFile(workspace), builtin.NewWriteFile(workspace), builtin.NewEditFile(workspace),
		builtin.NewSearchFiles(workspace), builtin.NewShell(workspace, platform.NewRunner(platform.Options{})),
	} {
		if err := registry.Register(registered); err != nil {
			return nil, err
		}
	}
	sessionID, err := newSessionID()
	if err != nil {
		return nil, err
	}
	store, err := session.NewStore(filepath.Join(options.StateDir, "sessions"), session.StoreOptions{})
	if err != nil {
		return nil, err
	}
	maxTurns := options.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 100
	}
	built, err := runtimepkg.NewPersistent(modelProvider, agent.Options{
		Model: profile.Model, MaxTurns: maxTurns, Tools: registry,
		ToolRunner: tool.NewRunner(registry, broker, tool.RunnerOptions{}), SessionID: sessionID,
	}, store, sessionID)
	if err != nil {
		return nil, err
	}
	if err := recordSession(options.StateDir, sessionMetadata{ID: sessionID, Profile: loaded.ActiveProfile, Model: profile.Model}); err != nil {
		_ = built.Shutdown(context.Background())
		return nil, err
	}
	return built, nil
}

func buildProvider(profile configpkg.Profile) (provider.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(profile.Provider)) {
	case "anthropic":
		return anthropic.New(profile)
	case "openai", "openai-compatible", "deepseek":
		return openai.New(profile)
	default:
		return nil, fmt.Errorf("provider %q is unsupported", profile.Provider)
	}
}

func newSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
