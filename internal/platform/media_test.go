package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/permissions"
)

func TestNotificationDetectsLinuxAndWindowsBackends(t *testing.T) {
	tests := []struct {
		goos    string
		found   string
		backend string
	}{
		{goos: "linux", found: "notify-send", backend: "notify-send"},
		{goos: "windows", found: "powershell.exe", backend: "windows-toast"},
	}
	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			var program string
			var arguments []string
			notifier := NewNotifier(NotifyOptions{
				GOOS: test.goos,
				LookPath: func(name string) (string, error) {
					if name == test.found {
						return filepath.Join("bin", name), nil
					}
					return "", exec.ErrNotFound
				},
				RunCommand: func(_ context.Context, name string, args []string) error {
					program, arguments = name, append([]string(nil), args...)
					return nil
				},
				Authorizer: allowMediaAuthorizer{},
				Workspace:  t.TempDir(),
			})
			if capability := notifier.Capability(); !capability.Available || capability.Backend != test.backend {
				t.Fatalf("capability = %#v", capability)
			}
			if err := notifier.Notify(context.Background(), "Title", "Message"); err != nil {
				t.Fatal(err)
			}
			if program == "" || len(arguments) == 0 {
				t.Fatalf("program = %q, arguments = %v", program, arguments)
			}
		})
	}
}

func TestNotificationMissingDependencyIsUnavailable(t *testing.T) {
	notifier := NewNotifier(NotifyOptions{GOOS: "linux", LookPath: missingCommand})
	capability := notifier.Capability()
	if capability.Available || capability.Reason == "" {
		t.Fatalf("capability = %#v", capability)
	}
	if err := notifier.Notify(context.Background(), "title", "message"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestVoiceDetectsBackendsAndBoundsOutput(t *testing.T) {
	for _, test := range []struct {
		goos, command, backend string
	}{{"linux", "arecord", "alsa"}, {"windows", "ffmpeg.exe", "ffmpeg-dshow"}} {
		t.Run(test.goos, func(t *testing.T) {
			var gotArgs []string
			recorder := NewVoiceRecorder(VoiceOptions{
				GOOS: test.goos,
				LookPath: func(name string) (string, error) {
					if name == test.command {
						return filepath.Join("bin", name), nil
					}
					return "", exec.ErrNotFound
				},
				RunCommand: func(_ context.Context, _ string, args []string) error {
					gotArgs = append([]string(nil), args...)
					return os.WriteFile(args[len(args)-1], []byte("audio"), 0o600)
				},
				Authorizer: allowMediaAuthorizer{},
				Workspace:  t.TempDir(),
				TempDir:    t.TempDir(),
				MaxBytes:   16,
			})
			if capability := recorder.Capability(); !capability.Available || capability.Backend != test.backend {
				t.Fatalf("capability = %#v", capability)
			}
			audio, err := recorder.Capture(context.Background())
			if err != nil || string(audio) != "audio" || len(gotArgs) == 0 {
				t.Fatalf("audio = %q, args = %v, error = %v", audio, gotArgs, err)
			}
		})
	}
}

func TestVoiceCancellationRemovesTemporaryFile(t *testing.T) {
	var outputPath string
	started := make(chan struct{})
	recorder := NewVoiceRecorder(VoiceOptions{
		GOOS: "linux",
		LookPath: func(name string) (string, error) {
			if name == "rec" {
				return "/bin/rec", nil
			}
			return "", exec.ErrNotFound
		},
		RunCommand: func(ctx context.Context, _ string, args []string) error {
			for _, argument := range args {
				if strings.HasSuffix(argument, ".raw") {
					outputPath = argument
					break
				}
			}
			if err := os.WriteFile(outputPath, []byte("partial"), 0o600); err != nil {
				return err
			}
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
		Authorizer: allowMediaAuthorizer{}, Workspace: t.TempDir(), TempDir: t.TempDir(), MaxDuration: time.Minute,
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := recorder.Capture(ctx)
		result <- err
	}()
	<-started
	cancel()
	err := <-result
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if outputPath != "" {
		if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("temporary file remains: %v", statErr)
		}
	}
}

func TestMediaCommandsRequirePermission(t *testing.T) {
	called := false
	notifier := NewNotifier(NotifyOptions{
		GOOS: "linux", LookPath: func(string) (string, error) { return "/bin/notify-send", nil },
		RunCommand: func(context.Context, string, []string) error { called = true; return nil },
		Authorizer: denyMediaAuthorizer{}, Workspace: t.TempDir(),
	})
	if err := notifier.Notify(context.Background(), "title", "message"); !errors.Is(err, ErrPermissionDenied) || called {
		t.Fatalf("called = %v, error = %v", called, err)
	}
}

type allowMediaAuthorizer struct{}

func (allowMediaAuthorizer) Decide(context.Context, permissions.Request) (permissions.Decision, error) {
	return permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}, nil
}

type denyMediaAuthorizer struct{}

func (denyMediaAuthorizer) Decide(context.Context, permissions.Request) (permissions.Decision, error) {
	return permissions.Decision{Behavior: permissions.PermissionBehaviorDeny}, nil
}

func missingCommand(string) (string, error) { return "", exec.ErrNotFound }
