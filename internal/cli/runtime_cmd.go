package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"cyber-code/internal/core"
	"cyber-code/internal/filelock"
	"cyber-code/internal/product"
	"cyber-code/internal/runtimeapi"
)

func newRuntimeCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{Use: "runtime", Short: "manage CYBER runtime sources", Args: cobra.NoArgs}
	command.AddCommand(newRuntimeServeCommand(environment))
	return command
}

func newRuntimeServeCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use: "serve", Short: "serve the authenticated local runtime over inherited stdio", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			bearer := os.Getenv(product.EnvRuntimeBearer)
			if strings.TrimSpace(bearer) == "" {
				return &core.Error{
					Kind: core.ErrorKindConfiguration, Op: "runtime.serve",
					Message: fmt.Sprintf("local runtime credential is unavailable: environment variable %s is not set", product.EnvRuntimeBearer),
				}
			}
			runtimeRoot := filepath.Join(environment.stateDir, "runtime-events")
			if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
				return fmt.Errorf("create local runtime directory: %w", err)
			}
			release, err := acquireRuntimeOwner(filepath.Join(runtimeRoot, "owner.lock"))
			if err != nil {
				return &core.Error{Kind: core.ErrorKindConfiguration, Op: "runtime.serve", Message: "another local runtime owner is active", Cause: err}
			}
			defer release()

			store, err := runtimeapi.NewStore(runtimeRoot)
			if err != nil {
				return err
			}
			runtimeID := stableLocalRuntimeID(environment.stateDir)
			service := runtimeapi.NewService(store, runtimeID, "local-user", nil)
			workspace, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve local runtime workspace: %w", err)
			}
			server, err := runtimeapi.NewLocalServer(runtimeapi.LocalServerOptions{
				Service: service, Bearer: bearer, Role: "owner", Workspace: workspace,
				TerminalProfiles: runtimeapi.DefaultTerminalProfiles(), TerminalBackend: runtimeapi.NewPortableTerminalBackend(),
				Source: runtimeapi.SourceMetadata{
					Mode: runtimeapi.SourceModeLocal, RuntimeID: runtimeID, Principal: "local-user",
					Capabilities: []string{"events", "snapshot", "commands", "terminal.observe", "terminal.input", "terminal.profile.default-shell"},
				},
			})
			if err != nil {
				return err
			}
			return server.Serve(environment.ctx, environment.stdin, environment.stdout)
		},
	}
}

func acquireRuntimeOwner(path string) (filelock.Release, error) {
	return filelock.TryAcquire(path)
}

func stableLocalRuntimeID(stateDir string) string {
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		absolute = stateDir
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absolute)))
	return "runtime-" + hex.EncodeToString(digest[:8])
}
