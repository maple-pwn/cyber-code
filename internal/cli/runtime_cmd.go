package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"cyber-code/internal/authorization"
	"cyber-code/internal/core"
	"cyber-code/internal/filelock"
	"cyber-code/internal/product"
	"cyber-code/internal/runtimeapi"
)

func newRuntimeCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{Use: "runtime", Short: "manage CYBER runtime sources", Args: cobra.NoArgs}
	command.AddCommand(newRuntimeServeCommand(environment))
	command.AddCommand(newRuntimeRemoteServeCommand(environment))
	return command
}

func newRuntimeRemoteServeCommand(environment *commandEnvironment) *cobra.Command {
	var listen, certFile, keyFile, bearerEnv, tenant, principal, sessionID, role string
	var origins []string
	command := &cobra.Command{
		Use: "remote-serve", Short: "serve authenticated runtime and admin APIs over HTTPS", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" {
				return fmt.Errorf("--cert and --key are required")
			}
			bearer := strings.TrimSpace(os.Getenv(bearerEnv))
			if bearer == "" {
				return fmt.Errorf("remote bearer environment variable %s is not set", bearerEnv)
			}
			if len(origins) == 0 {
				return fmt.Errorf("at least one --origin is required")
			}
			if listen == "" {
				listen = "127.0.0.1:8443"
			}
			if tenant == "" {
				tenant = "local"
			}
			if principal == "" {
				principal = "remote-admin"
			}
			if sessionID == "" {
				sessionID = "remote-session"
			}
			if role == "" {
				role = string(authorization.RoleOwner)
			}
			runtimeRoot := filepath.Join(environment.stateDir, "runtime-events")
			if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
				return fmt.Errorf("create remote runtime directory: %w", err)
			}
			store, err := runtimeapi.NewStore(runtimeRoot)
			if err != nil {
				return err
			}
			policyPath := filepath.Join(environment.stateDir, "authorization.json")
			fileStore, err := authorization.NewFileStore(policyPath)
			if err != nil {
				return err
			}
			policy, err := fileStore.Load()
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("load authorization policy: %w", err)
				}
				policy = authorization.NewPolicy([]authorization.Member{{TenantID: tenant, Principal: principal, Role: authorization.Role(role), Active: true}})
				if err := policy.RegisterSession(authorization.Session{ID: sessionID, TenantID: tenant, Principal: principal, ExpiresAt: time.Now().Add(24 * time.Hour)}); err != nil {
					return err
				}
				if err := policy.ElevateSession(sessionID, time.Now().Add(12*time.Hour)); err != nil {
					return err
				}
				if err := fileStore.Save(policy); err != nil {
					return err
				}
			}
			auth, err := runtimeapi.NewStaticRemoteAuthenticator(bearer, runtimeapi.RemoteClaims{
				Principal: principal, TenantID: tenant, SessionID: sessionID, Role: role,
				TaskContextID: "remote-default", ControllerID: "remote-controller",
				Capabilities: []string{"events", "snapshot", "commands"},
			})
			if err != nil {
				return err
			}
			service := runtimeapi.NewService(store, stableLocalRuntimeID(runtimeRoot), principal, nil)
			remote, err := runtimeapi.NewRemoteServer(runtimeapi.RemoteServerOptions{Service: service, Authenticator: auth, Workspace: "", AllowedOrigins: origins, Authorization: policy})
			if err != nil {
				return err
			}
			handler, err := runtimeapi.NewTeamHandlerForRemote(remote, authorization.NewAdminService(policy), auth)
			if err != nil {
				return err
			}
			httpServer := &http.Server{
				Addr: listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
				MaxHeaderBytes: 32 * 1024,
			}
			go func() {
				<-environment.ctx.Done()
				_ = httpServer.Shutdown(context.Background())
			}()
			err = httpServer.ListenAndServeTLS(certFile, keyFile)
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		},
	}
	command.Flags().StringVar(&listen, "listen", "127.0.0.1:8443", "HTTPS listen address")
	command.Flags().StringVar(&certFile, "cert", "", "TLS certificate PEM path")
	command.Flags().StringVar(&keyFile, "key", "", "TLS private key PEM path")
	command.Flags().StringVar(&bearerEnv, "bearer-env", product.EnvRuntimeBearer, "environment variable containing the static bearer token")
	command.Flags().StringVar(&tenant, "tenant", "local", "initial tenant identifier")
	command.Flags().StringVar(&principal, "principal", "remote-admin", "initial principal identifier")
	command.Flags().StringVar(&sessionID, "session", "remote-session", "initial session identifier")
	command.Flags().StringVar(&role, "role", string(authorization.RoleOwner), "initial role")
	command.Flags().StringArrayVar(&origins, "origin", nil, "allowed browser origin (repeatable)")
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
