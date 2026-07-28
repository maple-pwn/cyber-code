package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cyber-code/internal/permissions"
	"cyber-code/internal/product"
)

var (
	ErrUnavailable      = errors.New("media capability unavailable")
	ErrPermissionDenied = errors.New("media command permission denied")
)

type Capability struct {
	Available bool   `json:"available"`
	Backend   string `json:"backend,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type CommandFinder func(string) (string, error)
type CommandRunner func(context.Context, string, []string) error

type Authorizer interface {
	Decide(context.Context, permissions.Request) (permissions.Decision, error)
}

type NotifyOptions struct {
	GOOS       string
	LookPath   CommandFinder
	RunCommand CommandRunner
	Authorizer Authorizer
	Workspace  string
}

type Notifier struct {
	capability Capability
	program    string
	goos       string
	run        CommandRunner
	authorizer Authorizer
	workspace  string
}

func NewNotifier(options NotifyOptions) *Notifier {
	if options.GOOS == "" {
		options.GOOS = nativeNotificationTarget()
	}
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.Workspace == "" {
		options.Workspace, _ = os.Getwd()
	}
	if options.RunCommand == nil {
		options.RunCommand = managedCommandRunner(options.Workspace)
	}
	backend, program, reason := detectNotificationBackend(options.GOOS, options.LookPath)
	return &Notifier{
		capability: Capability{Available: program != "", Backend: backend, Reason: reason},
		program:    program, goos: options.GOOS, run: options.RunCommand,
		authorizer: options.Authorizer, workspace: options.Workspace,
	}
}

func (notifier *Notifier) Capability() Capability { return notifier.capability }

func (notifier *Notifier) Notify(ctx context.Context, title, message string) error {
	if !notifier.capability.Available {
		return fmt.Errorf("%w: %s", ErrUnavailable, notifier.capability.Reason)
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("notification message is required")
	}
	if len(title)+len(message) > 16<<10 {
		return fmt.Errorf("notification exceeds size limit")
	}
	if err := authorizeMedia(ctx, notifier.authorizer, notifier.workspace, "notification", notifier.program); err != nil {
		return err
	}
	return notifier.run(ctx, notifier.program, notificationArguments(notifier.goos, title, message))
}

func detectNotificationBackend(goos string, lookPath CommandFinder) (string, string, string) {
	var backend string
	var candidates []string
	switch goos {
	case "linux":
		backend, candidates = "notify-send", []string{"notify-send"}
	case "windows":
		backend, candidates = "windows-toast", []string{"powershell.exe", "powershell", "pwsh.exe", "pwsh"}
	default:
		return "", "", "desktop notifications are unsupported on " + goos
	}
	for _, candidate := range candidates {
		if path, err := lookPath(candidate); err == nil {
			return backend, path, ""
		}
	}
	return "", "", backend + " command was not found"
}

func notificationArguments(goos, title, message string) []string {
	if title == "" {
		title = product.Name
	}
	if goos == "linux" {
		return []string{title, message}
	}
	encode := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	const script = `$title=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($args[0]));$message=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($args[1]));$app=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($args[2]));[Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime]|Out-Null;$xml=[Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02);$nodes=$xml.GetElementsByTagName('text');$nodes.Item(0).AppendChild($xml.CreateTextNode($title))|Out-Null;$nodes.Item(1).AppendChild($xml.CreateTextNode($message))|Out-Null;$toast=[Windows.UI.Notifications.ToastNotification]::new($xml);[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show($toast)`
	return []string{"-NoProfile", "-NonInteractive", "-Command", script, encode(title), encode(message), encode(product.Name)}
}

func authorizeMedia(ctx context.Context, authorizer Authorizer, workspace, tool, program string) error {
	if authorizer == nil {
		return fmt.Errorf("%w: permission broker is required", ErrPermissionDenied)
	}
	decision, err := authorizer.Decide(ctx, permissions.Request{
		Tool: tool, Action: permissions.ActionExecute, Workspace: workspace, Command: filepath.Base(program),
	})
	if err != nil {
		return err
	}
	if decision.Behavior != permissions.PermissionBehaviorAllow {
		return ErrPermissionDenied
	}
	return nil
}

func managedCommandRunner(workspace string) CommandRunner {
	return func(ctx context.Context, program string, arguments []string) error {
		process, err := NewRunner(Options{}).Start(ctx, ProcessRequest{Command: program, Args: arguments, Workspace: workspace})
		if err != nil {
			return err
		}
		defer process.Close()
		return process.Wait()
	}
}
