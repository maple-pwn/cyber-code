package cyberagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDiscoverPrefersExplicitPathThenPATH(t *testing.T) {
	explicit := writeExecutable(t, "explicit-cyber-agent")
	pathCandidate := writeExecutable(t, "path-cyber-agent")
	result, err := Discover(DiscoveryOptions{
		ExplicitPath: explicit,
		LookPath: func(name string) (string, error) {
			if name != "cyber-agent" {
				t.Fatalf("LookPath(%q)", name)
			}
			return pathCandidate, nil
		},
	})
	if err != nil || result.Path != explicit || result.Source != DiscoverySourceExplicit {
		t.Fatalf("Discover explicit = %#v, %v", result, err)
	}

	result, err = Discover(DiscoveryOptions{LookPath: func(string) (string, error) { return pathCandidate, nil }})
	if err != nil || result.Path != pathCandidate || result.Source != DiscoverySourcePATH {
		t.Fatalf("Discover PATH = %#v, %v", result, err)
	}
}

func TestDiscoverUsesEnvironmentWithoutLeakingValue(t *testing.T) {
	executable := writeExecutable(t, "environment-cyber-agent")
	result, err := Discover(DiscoveryOptions{
		Environment: []string{"OTHER=value", "CYBER_AGENT_PATH=" + executable},
		LookPath:    func(string) (string, error) { return "", os.ErrNotExist },
	})
	if err != nil || result.Path != executable || result.Source != DiscoverySourceEnvironment {
		t.Fatalf("Discover environment = %#v, %v", result, err)
	}
}

func TestSupervisorStartsNegotiatesAndStopsWithoutBearerInArgv(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	supervisor, err := NewSupervisor(SupervisorOptions{
		Executable:           os.Args[0],
		PrefixArgs:           []string{"-test.run=TestCyberAgentSupervisorHelper", "--"},
		Environment:          append(os.Environ(), "GO_WANT_CYBER_AGENT_HELPER=serve", "CYBER_AGENT_HELPER_STATE="+state),
		ReadinessTimeout:     5 * time.Second,
		StopTimeout:          2 * time.Second,
		MinimumVersion:       "0.1.0",
		ProtocolVersion:      1,
		RequiredCapabilities: []string{"session.events.v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Stop(context.Background()) })

	readiness, ok := supervisor.Readiness()
	if !ok || readiness.ProtocolVersion != 1 || readiness.Version != "0.1.0" || readiness.PID <= 0 {
		t.Fatalf("readiness = %#v, ok=%t", readiness, ok)
	}
	client, err := supervisor.Client()
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil || !slices.Contains(capabilities.Capabilities, "session.events.v1") {
		t.Fatalf("capabilities = %#v, %v", capabilities, err)
	}

	stateBytes := waitForFile(t, state, 2*time.Second)
	var childState struct {
		Args   []string `json:"args"`
		Bearer string   `json:"bearer"`
	}
	if err := json.Unmarshal(stateBytes, &childState); err != nil {
		t.Fatal(err)
	}
	if childState.Bearer == "" {
		t.Fatal("child did not receive bearer over inherited stdin")
	}
	if strings.Contains(strings.Join(childState.Args, " "), childState.Bearer) {
		t.Fatalf("bearer leaked into argv: %q", childState.Args)
	}

	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-supervisor.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not finish after stop")
	}
}

func TestSupervisorRejectsIncompatibleReadiness(t *testing.T) {
	supervisor, err := NewSupervisor(SupervisorOptions{
		Executable:       os.Args[0],
		PrefixArgs:       []string{"-test.run=TestCyberAgentSupervisorHelper", "--"},
		Environment:      append(os.Environ(), "GO_WANT_CYBER_AGENT_HELPER=incompatible"),
		ReadinessTimeout: 3 * time.Second,
		ProtocolVersion:  1,
		MinimumVersion:   "0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = supervisor.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("Start error = %v", err)
	}
}

func TestSupervisorReadinessTimeout(t *testing.T) {
	supervisor, err := NewSupervisor(SupervisorOptions{
		Executable:       os.Args[0],
		PrefixArgs:       []string{"-test.run=TestCyberAgentSupervisorHelper", "--"},
		Environment:      append(os.Environ(), "GO_WANT_CYBER_AGENT_HELPER=timeout"),
		ReadinessTimeout: 100 * time.Millisecond,
		StopTimeout:      100 * time.Millisecond,
		ProtocolVersion:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = supervisor.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "readiness") || time.Since(started) > 2*time.Second {
		t.Fatalf("Start error = %v after %s", err, time.Since(started))
	}
}

func TestSupervisorUsesBoundedRestartBudget(t *testing.T) {
	state := filepath.Join(t.TempDir(), "restart-count")
	supervisor, err := NewSupervisor(SupervisorOptions{
		Executable:       os.Args[0],
		PrefixArgs:       []string{"-test.run=TestCyberAgentSupervisorHelper", "--"},
		Environment:      append(os.Environ(), "GO_WANT_CYBER_AGENT_HELPER=crash", "CYBER_AGENT_HELPER_STATE="+state),
		ReadinessTimeout: 3 * time.Second,
		StopTimeout:      time.Second,
		RestartLimit:     1,
		RestartDelay:     10 * time.Millisecond,
		ProtocolVersion:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-supervisor.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("restart budget was not exhausted")
	}
	countBytes := waitForFile(t, state, time.Second)
	if strings.TrimSpace(string(countBytes)) != "2" {
		t.Fatalf("child starts = %q, want 2", countBytes)
	}
	if supervisor.Err() == nil || !strings.Contains(supervisor.Err().Error(), "restart budget") {
		t.Fatalf("supervisor error = %v", supervisor.Err())
	}
}

func TestCyberAgentSupervisorHelper(t *testing.T) {
	mode := os.Getenv("GO_WANT_CYBER_AGENT_HELPER")
	if mode == "" {
		return
	}
	bearer, err := io.ReadAll(io.LimitReader(os.Stdin, 4097))
	if err != nil || len(bearer) == 0 || len(bearer) > 4096 {
		os.Exit(11)
	}
	statePath := os.Getenv("CYBER_AGENT_HELPER_STATE")
	if mode == "crash" && statePath != "" {
		count := 0
		if data, readErr := os.ReadFile(statePath); readErr == nil {
			_, _ = fmt.Sscanf(string(data), "%d", &count)
		}
		_ = os.WriteFile(statePath, []byte(fmt.Sprint(count+1)), 0o600)
	}
	if statePath != "" && mode == "serve" {
		encoded, _ := json.Marshal(map[string]any{"args": os.Args, "bearer": string(bearer)})
		_ = os.WriteFile(statePath, encoded, 0o600)
	}
	if mode == "timeout" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(12)
	}
	protocol := 1
	if mode == "incompatible" {
		protocol = 2
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capabilities", func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+string(bearer) {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(response, `{"product":"cyber-agent","runtime_version":"0.1.0","protocol_version":%d,"capabilities":["session.events.v1"]}`, protocol)
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	record := ReadinessRecord{Endpoint: "http://" + listener.Addr().String(), PID: os.Getpid(), Version: "0.1.0", ProtocolVersion: protocol}
	_ = json.NewEncoder(os.Stdout).Encode(record)
	if mode == "crash" {
		time.Sleep(50 * time.Millisecond)
		os.Exit(17)
	}
	_, _ = io.Copy(io.Discard, bytes.NewReader(nil))
	select {}
}

func writeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForFile(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return nil
}
