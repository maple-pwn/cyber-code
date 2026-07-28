package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"claude-code-go/internal/core"
	"claude-code-go/internal/frontend"
	"claude-code-go/internal/permissions"
	"claude-code-go/internal/ui"
)

type ExecuteOptions struct {
	Runner     frontend.Runner
	Version    string
	ConfigFile string
	StateDir   string
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
	var printMode, jsonMode bool
	var profile, permissionMode, model, cwd string
	var maxTurns int
	command := &cobra.Command{
		Use:           "claude-go [prompt]",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		Version:       environment.options.Version,
		RunE: func(_ *cobra.Command, args []string) error {
			runner := environment.options.Runner
			var shutdown func(context.Context) error
			var permissionUI *ui.PermissionBridge
			if !printMode {
				permissionUI = ui.NewPermissionBridge()
			}
			if runner == nil {
				built, err := composeRuntime(environment.ctx, compositionOptions{
					ConfigFile: environment.configFile, StateDir: environment.stateDir, Profile: profile,
					PermissionMode: permissionMode, Model: model, Cwd: cwd, MaxTurns: maxTurns, Headless: printMode,
					Confirmer: func(ctx context.Context, request permissions.Request) (permissions.Decision, error) {
						if permissionUI == nil {
							return permissions.Decision{Behavior: permissions.PermissionBehaviorDeny, Reason: "permission UI unavailable"}, nil
						}
						return permissionUI.Confirm(ctx, request)
					},
				})
				if err != nil {
					return err
				}
				runner, shutdown = built, built.Shutdown
			}
			if shutdown != nil {
				defer shutdown(context.Background())
			}
			prompt := strings.Join(args, " ")
			if printMode {
				code := frontend.Run(environment.ctx, runner, prompt, frontend.PrintOptions{
					JSON: jsonMode, Stdout: environment.stdout, Stderr: environment.stderr,
				})
				if code != frontend.ExitOK {
					return exitStatus{code: code}
				}
				return nil
			}
			app := NewApp(&Config{Runtime: runner, Cwd: cwd, PermissionUI: permissionUI, Context: environment.ctx}, environment.options.Version)
			defer app.Shutdown()
			return app.Run(prompt)
		},
	}
	command.Flags().BoolVarP(&printMode, "print", "p", false, "print events without the interactive UI")
	command.Flags().BoolVar(&jsonMode, "json", false, "emit JSON Lines in print mode")
	command.Flags().StringVar(&profile, "profile", "", "provider profile")
	command.Flags().StringVar(&permissionMode, "permission-mode", "", "permission mode")
	command.Flags().StringVarP(&model, "model", "m", "", "model override")
	command.Flags().StringVar(&cwd, "cwd", "", "workspace directory")
	command.Flags().IntVar(&maxTurns, "max-turns", 100, "maximum agent turns")
	command.AddCommand(newConfigCommand(environment))
	command.AddCommand(newDoctorCommand(environment))
	command.AddCommand(newMCPCommand(environment))
	command.AddCommand(newPluginsCommand(environment))
	command.AddCommand(newSessionsCommand(environment))
	return command
}

func resolveConfigFile(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if value := strings.TrimSpace(os.Getenv("CLAUDE_GO_CONFIG")); value != "" {
		return value
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", ".claude-go.yaml")
	}
	return filepath.Join(directory, "claude-code-go", "config.yaml")
}

func resolveStateDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if value := strings.TrimSpace(os.Getenv("CLAUDE_GO_STATE_DIR")); value != "" {
		return value
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", ".claude-go")
	}
	return filepath.Join(directory, "claude-code-go")
}
