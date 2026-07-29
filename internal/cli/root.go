package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"cyber-code/internal/attachment"
	"cyber-code/internal/core"
	"cyber-code/internal/frontend"
	"cyber-code/internal/permissions"
	"cyber-code/internal/product"
	"cyber-code/internal/protocol"
	"cyber-code/internal/tool/builtin"
	"cyber-code/internal/ui"
)

type ExecuteOptions struct {
	Runner             frontend.Runner
	Version            string
	ConfigFile         string
	StateDir           string
	UpdateMetadataURL  string
	UpdatePublicKeyB64 string
	UpdateHTTPClient   *http.Client
}

type commandEnvironment struct {
	ctx        context.Context
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	options    ExecuteOptions
	configFile string
	stateDir   string
}

type exitStatus struct{ code int }

func (status exitStatus) Error() string { return fmt.Sprintf("exit status %d", status.code) }

func Execute(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	return ExecuteWithOptions(ctx, stdin, stdout, stderr, args, ExecuteOptions{})
}

func ExecuteWithOptions(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string, options ExecuteOptions) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	environment := &commandEnvironment{
		ctx: ctx, stdin: stdin, stdout: stdout, stderr: stderr, options: options,
		configFile: resolveConfigFile(options.ConfigFile), stateDir: resolveStateDir(options.StateDir),
	}
	root := newRootCommand(environment)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		var status exitStatus
		if errors.As(err, &status) {
			return status.code
		}
		_, _ = fmt.Fprintln(stderr, err)
		return errorExitCode(err)
	}
	return frontend.ExitOK
}

func errorExitCode(err error) int {
	var classified *core.Error
	if !errors.As(err, &classified) {
		return frontend.ExitRuntime
	}
	switch classified.Kind {
	case core.ErrorKindConfiguration:
		return frontend.ExitConfiguration
	case core.ErrorKindAuthentication:
		return frontend.ExitAuthentication
	case core.ErrorKindPermission:
		return frontend.ExitPermission
	case core.ErrorKindCanceled:
		return frontend.ExitCanceled
	default:
		return frontend.ExitRuntime
	}
}

func newRootCommand(environment *commandEnvironment) *cobra.Command {
	var printMode, jsonMode, verbose bool
	var profile, permissionMode, model, cwd, resumeSession string
	var imagePaths []string
	var maxTurns int
	command := &cobra.Command{
		Use:           product.Command + " [prompt]",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		Version:       environment.options.Version,
		RunE: func(_ *cobra.Command, args []string) error {
			runner := environment.options.Runner
			var shutdown func(context.Context) error
			var permissionUI *ui.PermissionBridge
			var questionUI *ui.QuestionBridge
			var controlUI *ui.ControlBridge
			bootstrapConfirmer := newBootstrapConfirmer(environment.stdin, environment.stderr)
			if !printMode {
				permissionUI = ui.NewPermissionBridge()
				questionUI = ui.NewQuestionBridge()
				controlUI = ui.NewControlBridge()
			}
			if runner == nil {
				built, err := composeRuntime(environment.ctx, compositionOptions{
					ConfigFile: environment.configFile, StateDir: environment.stateDir, Profile: profile,
					PermissionMode: permissionMode, Model: model, Cwd: cwd, MaxTurns: maxTurns, Headless: printMode,
					SessionID: resumeSession, Version: environment.options.Version,
					Confirmer:         newInteractiveConfirmer(permissionUI, bootstrapConfirmer),
					Questioner:        newInteractiveQuestioner(questionUI),
					SetVimMode:        controlUI.SetVimMode,
					ClearConversation: controlUI.ClearConversation,
				})
				if err != nil {
					return err
				}
				runner, shutdown = built, built.Shutdown
			}
			if shutdown != nil {
				defer shutdown(context.Background())
			}
			if len(imagePaths) > 0 {
				images, err := loadInitialImages(cwd, imagePaths)
				if err != nil {
					return &core.Error{Kind: core.ErrorKindConfiguration, Op: "cli.image", Message: "image attachment could not be loaded", Cause: err}
				}
				runner = &initialImageRunner{delegate: runner, images: images}
			}
			prompt := strings.Join(args, " ")
			if printMode {
				code := frontend.Run(environment.ctx, runner, prompt, frontend.PrintOptions{
					JSON: jsonMode, Verbose: verbose, Stdout: environment.stdout, Stderr: environment.stderr,
				})
				if code != frontend.ExitOK {
					return exitStatus{code: code}
				}
				return nil
			}
			app := NewApp(&Config{Runtime: runner, Cwd: cwd, PermissionUI: permissionUI, QuestionUI: questionUI, ControlUI: controlUI, Context: environment.ctx}, environment.options.Version)
			defer app.Shutdown()
			return app.Run(prompt)
		},
	}
	command.Flags().BoolVarP(&printMode, "print", "p", false, "print events without the interactive UI")
	command.Flags().BoolVar(&jsonMode, "json", false, "emit JSON Lines in print mode")
	command.Flags().BoolVar(&verbose, "verbose", false, "emit safe tool progress and token usage to stderr in print mode")
	command.Flags().StringVar(&profile, "profile", "", "provider profile")
	command.Flags().StringVar(&permissionMode, "permission-mode", "", "permission mode")
	command.Flags().StringVarP(&model, "model", "m", "", "model override")
	command.Flags().StringVar(&cwd, "cwd", "", "workspace directory")
	command.Flags().StringArrayVar(&imagePaths, "image", nil, "attach an image from the workspace to the first turn")
	command.Flags().StringVar(&resumeSession, "resume", "", "resume a persisted session")
	command.Flags().IntVar(&maxTurns, "max-turns", 100, "maximum agent turns")
	command.AddCommand(newConfigCommand(environment))
	command.AddCommand(newDoctorCommand(environment))
	command.AddCommand(newMCPCommand(environment))
	command.AddCommand(newPluginsCommand(environment))
	command.AddCommand(newSessionsCommand(environment))
	command.AddCommand(newServeCommand(environment))
	command.AddCommand(newVersionCheckCommand(environment))
	command.AddCommand(newTerminalSetupCommand(environment))
	return command
}

type contentRunner interface {
	RunContent(context.Context, []core.ContentBlock) <-chan core.Event
}

type initialImageRunner struct {
	mu       sync.Mutex
	delegate frontend.Runner
	images   []core.ContentBlock
}

func (runner *initialImageRunner) Run(ctx context.Context, prompt string) <-chan core.Event {
	runner.mu.Lock()
	images := runner.images
	runner.images = nil
	runner.mu.Unlock()
	if len(images) == 0 {
		return runner.delegate.Run(ctx, prompt)
	}
	multimodal, ok := runner.delegate.(contentRunner)
	if !ok {
		return singleErrorEvent(&core.Error{Kind: core.ErrorKindConfiguration, Op: "cli.image", Message: "runtime does not support image input"})
	}
	content := make([]core.ContentBlock, 1, len(images)+1)
	content[0] = core.ContentBlock{Type: core.ContentText, Text: prompt}
	content = append(content, images...)
	return multimodal.RunContent(ctx, content)
}

func loadInitialImages(workspace string, paths []string) ([]core.ContentBlock, error) {
	if strings.TrimSpace(workspace) == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	images := make([]core.ContentBlock, len(paths))
	for index, path := range paths {
		image, err := attachment.LoadImage(workspace, path)
		if err != nil {
			return nil, fmt.Errorf("load image %d: %w", index+1, err)
		}
		images[index] = image
	}
	return images, nil
}

func singleErrorEvent(err *core.Error) <-chan core.Event {
	events := make(chan core.Event, 1)
	events <- core.Event{Type: core.EventError, Err: err}
	close(events)
	return events
}

func newServeCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{Use: "serve", Short: "serve the local integration protocol", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		permissionBroker := protocol.NewPermissionBroker(16)
		defer permissionBroker.Close()
		built, err := composeRuntime(environment.ctx, compositionOptions{ConfigFile: environment.configFile, StateDir: environment.stateDir, Headless: true, Confirmer: permissionBroker.Confirm})
		if err != nil {
			return err
		}
		defer built.Shutdown(context.Background())
		server, err := protocol.NewServerWithOptions(built, protocol.ServerOptions{Permissions: permissionBroker})
		if err != nil {
			return err
		}
		return server.Serve(environment.ctx, environment.stdin, environment.stdout)
	}}
	return command
}

func newInteractiveQuestioner(bridge *ui.QuestionBridge) builtin.Questioner {
	if bridge == nil {
		return nil
	}
	return bridge.Ask
}

func newInteractiveConfirmer(bridge *ui.PermissionBridge, bootstrap permissions.Confirmer) permissions.Confirmer {
	return func(ctx context.Context, request permissions.Request) (permissions.Decision, error) {
		if bridge == nil {
			return permissions.Decision{Behavior: permissions.PermissionBehaviorDeny, Reason: "permission UI unavailable"}, nil
		}
		if !bridge.Attached() {
			return bootstrap(ctx, request)
		}
		return bridge.Confirm(ctx, request)
	}
}

func newBootstrapConfirmer(input io.Reader, output io.Writer) permissions.Confirmer {
	if output == nil {
		output = io.Discard
	}
	var reader *bufio.Reader
	if input != nil {
		reader = bufio.NewReader(input)
	}
	var mutex sync.Mutex
	return func(ctx context.Context, request permissions.Request) (permissions.Decision, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return permissions.Decision{}, err
		}
		if reader == nil {
			return permissions.Decision{Behavior: permissions.PermissionBehaviorDeny, Reason: "startup confirmation input is unavailable"}, nil
		}
		mutex.Lock()
		defer mutex.Unlock()
		if err := ctx.Err(); err != nil {
			return permissions.Decision{}, err
		}
		kind := "external tool"
		switch {
		case request.Tool == "mcp" || strings.HasPrefix(request.Tool, "mcp.") || strings.HasPrefix(request.Tool, "mcp__"):
			kind = "mcp"
		case request.Tool == "plugin" || strings.HasPrefix(request.Tool, "plugin.") || strings.HasPrefix(request.Tool, "plugin__"):
			kind = "plugin"
		}
		target := permissions.SafeTargetSummary(request, false)
		if target != "" {
			target = " to " + target
		}
		_, _ = fmt.Fprintf(output, "Allow %s %s access%s during startup? [y/N]: ", kind, request.Action, target)
		answer, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return permissions.Decision{}, fmt.Errorf("read startup permission response: %w", err)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer == "y" || answer == "yes" {
			return permissions.Decision{Behavior: permissions.PermissionBehaviorAllow, Reason: "startup access confirmed by user"}, nil
		}
		return permissions.Decision{Behavior: permissions.PermissionBehaviorDeny, Reason: "startup access was not confirmed"}, nil
	}
}

func resolveConfigFile(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if value := strings.TrimSpace(os.Getenv(product.EnvConfig)); value != "" {
		return value
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "."+product.Name+".yaml")
	}
	return filepath.Join(directory, product.ConfigDirectory, "config.yaml")
}

func resolveStateDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if value := strings.TrimSpace(os.Getenv(product.EnvStateDir)); value != "" {
		return value
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "."+product.Name)
	}
	return filepath.Join(directory, product.ConfigDirectory)
}
