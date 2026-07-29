package cli

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"cyber-code/internal/platform"
	updatepkg "cyber-code/internal/update"
)

func newVersionCheckCommand(environment *commandEnvironment) *cobra.Command {
	var enabled bool
	var metadataURL, publicKeyText string
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "version-check",
		Short: "explicitly check signed release metadata without installing",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !enabled {
				_, err := fmt.Fprintln(environment.stdout, "version checking is disabled; pass --enable with a trusted HTTPS metadata URL and Ed25519 public key")
				return err
			}
			publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyText))
			if err != nil || len(publicKey) != ed25519.PublicKeySize {
				return fmt.Errorf("--public-key must be a base64 Ed25519 public key")
			}
			result, err := updatepkg.Check(environment.ctx, updatepkg.Options{
				Enabled: true, CurrentVersion: environment.options.Version, MetadataURL: metadataURL,
				PublicKey: ed25519.PublicKey(publicKey), Timeout: timeout,
			})
			if err != nil {
				return err
			}
			if !result.UpdateAvailable {
				_, err = fmt.Fprintf(environment.stdout, "cyber-code %s is current (latest %s)\n", result.CurrentVersion, result.LatestVersion)
				return err
			}
			_, err = fmt.Fprintf(environment.stdout, "cyber-code %s is available: %s\nNo files were downloaded or installed.\n", result.LatestVersion, result.DownloadURL)
			return err
		},
	}
	command.Flags().BoolVar(&enabled, "enable", false, "allow this invocation to access signed release metadata")
	command.Flags().StringVar(&metadataURL, "metadata-url", "", "trusted HTTPS release metadata URL")
	command.Flags().StringVar(&publicKeyText, "public-key", "", "trusted base64 Ed25519 public key")
	command.Flags().DurationVar(&timeout, "timeout", 3*time.Second, "network timeout (capped at 10s)")
	return command
}

func newTerminalSetupCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "terminal-setup",
		Short: "show platform-specific terminal guidance without changing settings",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			guidance, err := platform.TerminalSetupGuidance(runtime.GOOS, os.Getenv)
			if err != nil {
				return err
			}
			for _, step := range guidance.Steps {
				if _, err := fmt.Fprintln(environment.stdout, step); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
