package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"time"

	"cyber-code/internal/agent"
	"cyber-code/internal/bugreport"
	"cyber-code/internal/collaboration"
	configpkg "cyber-code/internal/config"
	"cyber-code/internal/contextbuilder"
	"cyber-code/internal/core"
	"cyber-code/internal/gitworkflow"
	"cyber-code/internal/hooks"
	"cyber-code/internal/lsp"
	"cyber-code/internal/mcp"
	"cyber-code/internal/memory"
	"cyber-code/internal/permissions"
	"cyber-code/internal/platform"
	"cyber-code/internal/plugin"
	"cyber-code/internal/product"
	"cyber-code/internal/provider"
	"cyber-code/internal/provider/anthropic"
	"cyber-code/internal/provider/azure"
	"cyber-code/internal/provider/bedrock"
	"cyber-code/internal/provider/openai"
	"cyber-code/internal/provider/vertex"
	runtimepkg "cyber-code/internal/runtime"
	api "cyber-code/internal/services/api"
	"cyber-code/internal/session"
	"cyber-code/internal/skill"
	"cyber-code/internal/tasks"
	"cyber-code/internal/tool"
	"cyber-code/internal/tool/builtin"
	"cyber-code/internal/utils"
)

const (
	defaultContextWindow  = 128_000
	defaultReservedOutput = 8_192
)

type compositionOptions struct {
	ConfigFile, StateDir, Profile, PermissionMode, Model, Cwd, SessionID, Version string
	MaxTurns                                                                      int
	Headless                                                                      bool
	Confirmer                                                                     permissions.Confirmer
	Questioner                                                                    builtin.Questioner
	SetVimMode                                                                    func(bool) error
	ClearConversation                                                             func() error
}

func composeRuntime(ctx context.Context, options compositionOptions) (_ *runtimepkg.Runtime, returnErr error) {
	var err error
	if options.SessionID != "" {
		entries, err := loadSessionIndex(options.StateDir)
		if err != nil {
			return nil, err
		}
		metadata, exists := entries[options.SessionID]
		if !exists {
			return nil, fmt.Errorf("session %q does not exist", options.SessionID)
		}
		if options.Profile == "" {
			options.Profile = metadata.Profile
		}
		if options.Model == "" {
			options.Model = metadata.Model
		}
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
	loaded, err := configpkg.Load(configpkg.LoadOptions{
		UserFile: options.ConfigFile, ProjectFile: filepath.Join(workspace, "."+product.Name+".yaml"),
		CLI: configpkg.Overrides{Profile: options.Profile, PermissionMode: options.PermissionMode},
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
	compactor, err := configureCompactor(options.StateDir, profile.Model, modelProvider)
	if err != nil {
		return nil, err
	}
	mode := permissions.PermissionMode(loaded.PermissionMode)
	source := permissions.SourceUserSettings
	if options.PermissionMode != "" {
		source = permissions.SourceCliArg
	}
	audit, err := newPersistentAuditLog(filepath.Join(options.StateDir, "audit.json"), 1000)
	if err != nil {
		return nil, err
	}
	broker, err := permissions.NewBroker(permissions.Options{
		Mode: mode, ModeSource: source, Headless: options.Headless, Confirmer: options.Confirmer, Audit: audit,
	})
	if err != nil {
		return nil, err
	}
	registry := tool.NewRegistry()
	processRunner := platform.NewRunner(platform.Options{SandboxMode: platform.SandboxMode(loaded.SandboxMode)})
	if capability := processRunner.SandboxCapability(); capability.Mode == platform.SandboxRequired && !capability.Strong {
		return nil, fmt.Errorf("configure process sandbox: %w: %s", platform.ErrSandboxUnavailable, capability.DegradedReason)
	}
	gitService, err := gitworkflow.NewService(workspace, &authorizedExecutor{tool: "git", broker: broker, delegate: processRunner})
	if err != nil {
		return nil, fmt.Errorf("configure Git workflows: %w", err)
	}
	if err := registerWorkspaceTools(registry, workspace, processRunner, options.Questioner); err != nil {
		return nil, err
	}
	discoveredSkills, err := discoverConfiguredSkills(workspace, options.StateDir)
	if err != nil {
		return nil, err
	}
	if len(discoveredSkills) > 0 {
		if err := skill.RegisterTool(registry, discoveredSkills); err != nil {
			return nil, err
		}
	}
	agentLoader, err := collaboration.NewDefinitionLoader(options.StateDir, workspace)
	if err != nil {
		return nil, err
	}
	agentDefinitions, err := agentLoader.Load()
	if err != nil {
		return nil, fmt.Errorf("load agent definitions: %w", err)
	}
	hookRunner, err := configureHooks(options.StateDir, workspace, broker, processRunner)
	if err != nil {
		return nil, err
	}
	services := make([]io.Closer, 0, 4)
	defer func() {
		if returnErr != nil {
			closeServices(services)
		}
	}()
	lspManager, err := configureLSP(options.StateDir, registry, broker, processRunner)
	if err != nil {
		return nil, err
	}
	if lspManager != nil {
		services = append(services, lspManager)
	}
	mcpManager, err := connectConfiguredMCP(ctx, options.StateDir, workspace, registry, broker, processRunner)
	if err != nil {
		return nil, err
	}
	if mcpManager != nil {
		if err := mcp.RegisterResourceTool(registry, mcpManager); err != nil {
			_ = mcpManager.Close()
			return nil, err
		}
		services = append(services, mcpManager)
	}
	pluginManager, err := loadEnabledPlugins(ctx, options.StateDir, registry, broker)
	if err != nil {
		return nil, err
	}
	if pluginManager != nil {
		services = append(services, pluginManager)
	}
	sessionID := options.SessionID
	if sessionID == "" {
		sessionID, err = newSessionID()
		if err != nil {
			return nil, err
		}
	}
	if err := registry.Register(builtin.NewTodo(todoStatePath(options.StateDir, sessionID))); err != nil {
		return nil, err
	}
	maxTurns := options.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 100
	}
	contextBuilder, err := buildContextBuilderWithThresholds(workspace, options.StateDir, loaded.ContextWarningThreshold, loaded.ContextCompactThreshold)
	if err != nil {
		return nil, err
	}
	childRegistry := registry.Clone()
	coordinator, err := collaboration.NewCoordinator(filepath.Join(options.StateDir, "collaboration"))
	if err != nil {
		return nil, fmt.Errorf("configure agent collaboration: %w", err)
	}
	taskService, err := configureTaskService(taskCompositionOptions{
		Provider: modelProvider, Registry: childRegistry, Broker: broker, Hooks: hookRunner,
		Model: profile.Model, ContextBuilder: contextBuilder, SessionID: sessionID,
		ParentMode: mode, ParentMaxTurns: maxTurns, Definitions: agentDefinitions,
		Board: coordinator.Board, Coordinator: coordinator,
	})
	if err != nil {
		return nil, err
	}
	if err := tasks.RegisterTools(registry, taskService); err != nil {
		_ = taskService.Close()
		return nil, err
	}
	services = append(services, taskService)
	store, err := session.NewStore(filepath.Join(options.StateDir, "sessions"), session.StoreOptions{})
	if err != nil {
		return nil, err
	}
	agentOptions := agent.Options{
		Model: profile.Model, ContextBuilder: contextBuilder, MaxTurns: maxTurns, Tools: registry,
		ToolRunner: tool.NewRunner(registry, broker, tool.RunnerOptions{Hooks: hookRunner, SessionID: sessionID, Gate: collaborationExecutionGate(coordinator)}),
		Compactor:  compactor, Hooks: hookRunner, SessionID: sessionID,
	}
	var built *runtimepkg.Runtime
	if options.SessionID != "" {
		built, err = runtimepkg.Resume(modelProvider, agentOptions, store, sessionID, services...)
	} else {
		built, err = runtimepkg.NewPersistent(modelProvider, agentOptions, store, sessionID, services...)
	}
	if err != nil {
		return nil, err
	}
	services = nil // Runtime owns the service lifetime after successful construction.
	actions := ControlActions{
		InitializeInstructions: newInstructionInitializer(workspace),
		EffectiveConfig:        func() configpkg.Config { return *loaded },
		UsageSnapshot:          built.UsageSnapshot,
		HistoryCount:           func() int { return len(built.History()) },
		ClearHistory: func(ctx context.Context) error {
			if err := built.ClearHistory(ctx); err != nil {
				return err
			}
			if options.ClearConversation != nil && !options.Headless {
				return options.ClearConversation()
			}
			return nil
		},
		SetVimMode: options.SetVimMode,
		CreateBugReport: func(ctx context.Context, description string) (string, error) {
			result, err := bugreport.Create(ctx, bugreport.Options{
				Directory: filepath.Join(options.StateDir, "bug-reports"), Version: options.Version, GOOS: goruntime.GOOS, GOARCH: goruntime.GOARCH,
				Secrets: configuredBugReportSecrets(*loaded, os.LookupEnv),
			}, bugreport.Input{
				Description: description,
				Diagnostics: fmt.Sprintf("profile: %s\nmodel: %s\nsession: %s\nhistory messages: %d", loaded.ActiveProfile, profile.Model, sessionID, len(built.History())),
			})
			return result.Path, err
		},
	}
	if profile.Pricing != nil {
		pricing := session.ModelPricing{
			InputPerMillion:      profile.Pricing.InputPerMillion,
			OutputPerMillion:     profile.Pricing.OutputPerMillion,
			CacheReadPerMillion:  profile.Pricing.CacheReadPerMillion,
			CacheWritePerMillion: profile.Pricing.CacheWritePerMillion,
		}
		actions.EstimateCost = func(usage core.Usage) (float64, error) {
			return pricing.Cost(usage), nil
		}
	}
	commands, err := buildControlPlane(built, options.StateDir, loaded.ActiveProfile, profile.Model, mode, hookRunner, discoveredSkills, contextBuilder, mcpManager, gitService, actions)
	if err != nil {
		_ = built.Shutdown(context.Background())
		return nil, fmt.Errorf("configure command control plane: %w", err)
	}
	built.AttachControlPlane(commands)
	if options.SessionID == "" {
		err = recordSession(options.StateDir, sessionMetadata{ID: sessionID, Profile: loaded.ActiveProfile, Model: profile.Model})
	}
	if err != nil {
		_ = built.Shutdown(context.Background())
		return nil, err
	}
	return built, nil
}

func configuredBugReportSecrets(configuration configpkg.Config, lookup func(string) (string, bool)) []string {
	if lookup == nil {
		return nil
	}
	seen := make(map[string]struct{})
	secrets := make([]string, 0, len(configuration.Profiles))
	for _, profile := range configuration.Profiles {
		value, ok := lookup(profile.APIKeyEnv)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		secrets = append(secrets, value)
	}
	return secrets
}

func registerWorkspaceTools(registry *tool.Registry, workspace string, executor platform.Executor, questioner builtin.Questioner) error {
	for _, registered := range []tool.Tool{
		builtin.NewReadFile(workspace), builtin.NewWriteFile(workspace), builtin.NewEditFile(workspace),
		builtin.NewSearchFiles(workspace), builtin.NewGrepFiles(workspace), builtin.NewGlobFiles(workspace),
		builtin.NewShell(workspace, executor), builtin.NewAskUser(questioner), builtin.NewWebFetch(nil),
		builtin.NewWebSearch(builtin.NewDuckDuckGoSearch(nil)), builtin.NewNotebookEdit(workspace),
		builtin.NewSemanticSearch(workspace),
	} {
		if err := registry.Register(registered); err != nil {
			return err
		}
	}
	return nil
}

func todoStatePath(stateDir, sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return filepath.Join(stateDir, "todos", hex.EncodeToString(digest[:])+".json")
}

func configureHooks(stateDir, workspace string, broker *permissions.Broker, executor platform.Executor) (*hooks.Runner, error) {
	commands, err := loadHookCommands(filepath.Join(stateDir, "hooks.json"))
	if err != nil {
		return nil, fmt.Errorf("load hook configuration: %w", err)
	}
	if len(commands) == 0 {
		return nil, nil
	}
	runner, err := hooks.NewRunner(hooks.RunnerOptions{
		Registry: hooks.NewRegistry(), Executor: &authorizedExecutor{
			tool: "hook", broker: broker, delegate: executor,
		}, Workspace: workspace,
	})
	if err != nil {
		return nil, err
	}
	for _, event := range hooks.HookEvents {
		for _, command := range commands[event] {
			if err := runner.RegisterCommand(event, command); err != nil {
				return nil, err
			}
		}
	}
	return runner, nil
}

func configureLSP(stateDir string, registry *tool.Registry, broker *permissions.Broker, processRunner *platform.Runner) (*lsp.Manager, error) {
	configs, err := loadLSPConfigs(filepath.Join(stateDir, "lsp.json"))
	if err != nil {
		return nil, fmt.Errorf("load LSP configuration: %w", err)
	}
	if len(configs) == 0 {
		return nil, nil
	}
	manager, err := lsp.NewManager(lsp.ManagerOptions{Configs: configs, Authorizer: broker, Runner: processRunner})
	if err != nil {
		return nil, err
	}
	if err := lsp.RegisterTools(registry, manager); err != nil {
		_ = manager.Close()
		return nil, err
	}
	return manager, nil
}

func discoverConfiguredSkills(workspace, stateDir string) ([]skill.Skill, error) {
	candidates := []skill.Root{
		{Name: "project", Path: filepath.Join(workspace, "."+product.Name, "skills")},
		{Name: "user", Path: filepath.Join(stateDir, "skills")},
	}
	roots := make([]skill.Root, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Stat(candidate.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect %s skill root: %w", candidate.Name, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s skill root is not a directory", candidate.Name)
		}
		roots = append(roots, candidate)
	}
	if len(roots) == 0 {
		return nil, nil
	}
	loader, err := skill.NewLoader(roots)
	if err != nil {
		return nil, err
	}
	return loader.Discover()
}

func buildContextBuilder(currentDir, stateDir string) (*contextbuilder.Builder, error) {
	return buildContextBuilderWithThresholds(currentDir, stateDir, contextbuilder.DefaultWarningThreshold, contextbuilder.DefaultCompactThreshold)
}

func buildContextBuilderWithThresholds(currentDir, stateDir string, warningThreshold, compactThreshold float64) (*contextbuilder.Builder, error) {
	projectRoot := currentDir
	if root, err := utils.FindGitRoot(currentDir); err == nil {
		projectRoot = root
	}
	loader, err := contextbuilder.NewLoader(contextbuilder.LoaderOptions{
		StateDir: stateDir, Workspace: projectRoot, CurrentDir: currentDir,
	})
	if err != nil {
		return nil, fmt.Errorf("configure context instructions: %w", err)
	}
	sources, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("load context instructions: %w", err)
	}
	memoryStore, err := memory.NewStore(filepath.Join(stateDir, "memory"), memory.Options{})
	if err != nil {
		return nil, fmt.Errorf("configure memory: %w", err)
	}
	for _, scope := range []memory.Scope{memory.ScopeUser, memory.ScopeProject} {
		entries, listErr := memoryStore.List(context.Background(), scope)
		if listErr != nil {
			return nil, fmt.Errorf("load %s memory: %w", scope, listErr)
		}
		for _, entry := range entries {
			sources = append(sources, contextbuilder.Source{
				ID: "memory:" + entry.ID, Kind: contextbuilder.SourceMemory, Priority: 200,
				Path: filepath.Join(stateDir, "memory", "memory.json"), Content: entry.Content,
			})
		}
	}
	builder, err := contextbuilder.New(contextbuilder.Options{
		Sources: sources, ContextWindow: defaultContextWindow, ReservedOutput: defaultReservedOutput,
		WarningThreshold: warningThreshold, CompactThreshold: compactThreshold,
	})
	if err != nil {
		return nil, fmt.Errorf("configure context builder: %w", err)
	}
	return builder, nil
}

func connectConfiguredMCP(ctx context.Context, stateDir, workspace string, registry *tool.Registry, broker *permissions.Broker, processRunner *platform.Runner) (*mcp.Manager, error) {
	entries, err := loadMCPEntries(filepath.Join(stateDir, "mcp.json"))
	if err != nil {
		return nil, fmt.Errorf("load MCP configuration: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	manager, err := mcp.NewManager(mcp.ManagerOptions{
		Registry: registry, Authorizer: broker, TransportFactory: mcp.NewDefaultTransportFactory(mcp.DefaultTransportOptions{Runner: processRunner}),
	})
	if err != nil {
		return nil, err
	}
	for name, entry := range entries {
		if entry.Disabled {
			continue
		}
		config := mcp.ServerConfig{Name: name, Workspace: workspace, Args: append([]string(nil), entry.Args...)}
		if entry.URL != "" {
			config.Transport = mcp.TransportHTTP
			config.URL = entry.URL
			config.Headers, err = resolveMCPHeaders(ctx, stateDir, name, entry)
			if err != nil {
				_ = manager.Close()
				return nil, err
			}
		} else {
			config.Transport = mcp.TransportStdio
			config.Command = entry.Command
		}
		if err := manager.Connect(ctx, config); err != nil {
			_ = manager.Close()
			return nil, err
		}
	}
	return manager, nil
}

func resolveMCPHeaders(ctx context.Context, stateDir, name string, entry mcpEntry) (map[string]string, error) {
	headers := make(map[string]string, len(entry.HeaderEnv)+1)
	for header, environment := range entry.HeaderEnv {
		value, exists := os.LookupEnv(environment)
		if !exists || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("MCP header environment variable %s is not set", environment)
		}
		headers[header] = value
	}
	store, err := mcp.NewCredentialStore(filepath.Join(stateDir, "mcp-credentials"))
	if err != nil {
		return nil, err
	}
	if _, exists := headers["Authorization"]; exists {
		return headers, nil
	}
	credential, ok, err := store.Get(ctx, name)
	if err != nil || !ok {
		return headers, err
	}
	if credential.ExpiresAt > 0 && time.Now().Unix() >= credential.ExpiresAt {
		secret := ""
		if entry.OAuthClientSecretEnv != "" {
			var exists bool
			secret, exists = os.LookupEnv(entry.OAuthClientSecretEnv)
			if !exists {
				return nil, fmt.Errorf("MCP OAuth client secret environment variable %s is not set", entry.OAuthClientSecretEnv)
			}
		}
		credential, err = store.Resolve(ctx, name, time.Now(), mcp.OAuthRefresher{TokenURL: entry.OAuthTokenURL, ClientID: entry.OAuthClientID, ClientSecret: secret})
		if err != nil {
			return nil, err
		}
	}
	tokenType := strings.TrimSpace(credential.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	headers["Authorization"] = tokenType + " " + credential.AccessToken
	return headers, nil
}

func loadEnabledPlugins(ctx context.Context, stateDir string, registry *tool.Registry, broker *permissions.Broker) (*plugin.Manager, error) {
	states, err := loadPluginStates(filepath.Join(stateDir, "plugins.json"))
	if err != nil {
		return nil, fmt.Errorf("load plugin state: %w", err)
	}
	names := make([]string, 0, len(states))
	for name, state := range states {
		if state.Enabled {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	sort.Strings(names)
	manager, err := plugin.NewManager(plugin.ManagerOptions{Registry: registry, Authorizer: broker})
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		root, err := resolvePluginRoot(stateDir, name)
		if err != nil {
			_ = manager.Close()
			return nil, err
		}
		if err := manager.Load(ctx, root); err != nil {
			_ = manager.Close()
			return nil, fmt.Errorf("load plugin %q: %w", name, err)
		}
	}
	return manager, nil
}

func closeServices(services []io.Closer) {
	for index := len(services) - 1; index >= 0; index-- {
		if services[index] != nil {
			_ = services[index].Close()
		}
	}
}

func buildProvider(profile configpkg.Profile) (provider.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(profile.Provider)) {
	case "anthropic":
		return anthropic.New(profile)
	case "openai", "openai-compatible", "deepseek":
		return openai.New(profile)
	case "bedrock":
		if profile.APIKeyEnv != "" {
			return bedrock.New(profile)
		}
		if _, exists := os.LookupEnv("AWS_BEARER_TOKEN_BEDROCK"); exists {
			profile.APIKeyEnv = "AWS_BEARER_TOKEN_BEDROCK"
			return bedrock.New(profile)
		}
		region := strings.TrimSpace(os.Getenv("AWS_REGION"))
		if region == "" {
			region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
		}
		if region == "" {
			return nil, fmt.Errorf("Bedrock managed authentication requires AWS_REGION or AWS_DEFAULT_REGION")
		}
		return bedrock.New(profile, bedrock.WithCredentials(region, api.GetAWSAuthManager().RefreshAndGetAWSCredentials))
	case "vertex":
		if profile.APIKeyEnv != "" {
			return vertex.New(profile)
		}
		return vertex.New(profile, vertex.WithTokenSource(api.GetGoogleAuthManager().GetAccessToken))
	case "azure":
		if profile.APIKeyEnv != "" {
			return azure.New(profile)
		}
		if _, exists := os.LookupEnv("ANTHROPIC_FOUNDRY_API_KEY"); exists {
			profile.APIKeyEnv = "ANTHROPIC_FOUNDRY_API_KEY"
			return azure.New(profile)
		}
		return azure.New(profile, azure.WithTokenSource(api.GetAzureAuthManager().GetAccessToken))
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
