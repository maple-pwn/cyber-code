package cyberagent

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultReadinessLimit int64 = 16 << 10

type ReadinessRecord struct {
	Endpoint        string `json:"endpoint"`
	PID             int    `json:"pid"`
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocol_version"`
}

type SupervisorOptions struct {
	Executable           string
	PrefixArgs           []string
	Environment          []string
	HTTPClient           *http.Client
	ReadinessTimeout     time.Duration
	ReadinessLimit       int64
	StopTimeout          time.Duration
	RestartLimit         int
	RestartDelay         time.Duration
	MinimumVersion       string
	ProtocolVersion      int
	RequiredCapabilities []string
	TokenGenerator       func() (string, error)
}

type Supervisor struct {
	options SupervisorOptions

	mu          sync.RWMutex
	child       *supervisedChild
	readiness   ReadinessRecord
	client      *Client
	started     bool
	stopping    bool
	restarts    int
	terminalErr error

	done       chan struct{}
	stop       chan struct{}
	stopOnce   sync.Once
	doneOnce   sync.Once
	lifecycle  context.Context
	cancelLife context.CancelFunc
}

type supervisedChild struct {
	command *exec.Cmd
	guard   supervisorProcessGuard
	wait    <-chan error
}

type supervisorProcessGuard interface {
	graceful(*os.Process) error
	kill(*os.Process) error
	close() error
}

func NewSupervisor(options SupervisorOptions) (*Supervisor, error) {
	if strings.TrimSpace(options.Executable) == "" {
		return nil, fmt.Errorf("cyber-agent executable is required")
	}
	if options.ReadinessTimeout <= 0 {
		options.ReadinessTimeout = 15 * time.Second
	}
	if options.ReadinessLimit == 0 {
		options.ReadinessLimit = defaultReadinessLimit
	}
	if options.ReadinessLimit < 1 {
		return nil, fmt.Errorf("cyber-agent readiness limit must be positive")
	}
	if options.StopTimeout <= 0 {
		options.StopTimeout = 5 * time.Second
	}
	if options.RestartLimit < 0 {
		return nil, fmt.Errorf("cyber-agent restart limit cannot be negative")
	}
	if options.RestartDelay <= 0 {
		options.RestartDelay = 250 * time.Millisecond
	}
	if options.ProtocolVersion == 0 {
		options.ProtocolVersion = 1
	}
	if options.MinimumVersion == "" {
		options.MinimumVersion = "0.1.0"
	}
	if _, err := parseVersion(options.MinimumVersion); err != nil {
		return nil, fmt.Errorf("invalid minimum cyber-agent version: %w", err)
	}
	if options.TokenGenerator == nil {
		options.TokenGenerator = generateBearer
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	return &Supervisor{
		options: options, done: make(chan struct{}), stop: make(chan struct{}), lifecycle: lifecycle, cancelLife: cancel,
	}, nil
}

func (supervisor *Supervisor) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	supervisor.mu.Lock()
	if supervisor.started {
		supervisor.mu.Unlock()
		return fmt.Errorf("cyber-agent supervisor is already started")
	}
	if supervisor.stopping {
		supervisor.mu.Unlock()
		return fmt.Errorf("cyber-agent supervisor is stopped")
	}
	supervisor.mu.Unlock()

	child, readiness, client, err := supervisor.startChild(ctx)
	if err != nil {
		return err
	}
	supervisor.mu.Lock()
	supervisor.child, supervisor.readiness, supervisor.client, supervisor.started = child, readiness, client, true
	supervisor.mu.Unlock()
	go supervisor.monitor(child)
	return nil
}

func (supervisor *Supervisor) Readiness() (ReadinessRecord, bool) {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	return supervisor.readiness, supervisor.started && supervisor.client != nil && !supervisor.stopping
}

func (supervisor *Supervisor) Client() (*Client, error) {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	if supervisor.client == nil || supervisor.stopping {
		return nil, fmt.Errorf("cyber-agent runtime is not connected")
	}
	return supervisor.client, nil
}

func (supervisor *Supervisor) Done() <-chan struct{} { return supervisor.done }

func (supervisor *Supervisor) Err() error {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	return supervisor.terminalErr
}

func (supervisor *Supervisor) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	supervisor.mu.Lock()
	select {
	case <-supervisor.done:
		err := supervisor.terminalErr
		supervisor.mu.Unlock()
		return err
	default:
	}
	if supervisor.stopping {
		done := supervisor.done
		supervisor.mu.Unlock()
		select {
		case <-done:
			return supervisor.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	supervisor.stopping = true
	child := supervisor.child
	started := supervisor.started
	supervisor.client = nil
	supervisor.mu.Unlock()
	supervisor.stopOnce.Do(func() { close(supervisor.stop); supervisor.cancelLife() })
	if !started || child == nil {
		supervisor.finish(nil)
		return nil
	}

	_ = child.guard.graceful(child.command.Process)
	timer := time.NewTimer(supervisor.options.StopTimeout)
	defer timer.Stop()
	select {
	case <-supervisor.done:
		return supervisor.Err()
	case <-ctx.Done():
		_ = child.guard.kill(child.command.Process)
		return ctx.Err()
	case <-timer.C:
		_ = child.guard.kill(child.command.Process)
	}
	select {
	case <-supervisor.done:
		return supervisor.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (supervisor *Supervisor) startChild(parent context.Context) (*supervisedChild, ReadinessRecord, *Client, error) {
	token, err := supervisor.options.TokenGenerator()
	if err != nil || strings.TrimSpace(token) == "" || len(token) > 4096 {
		return nil, ReadinessRecord{}, nil, fmt.Errorf("generate cyber-agent bearer: invalid token")
	}
	args := append([]string(nil), supervisor.options.PrefixArgs...)
	args = append(args, "serve", "--bind", "127.0.0.1", "--port", "0", "--bearer-fd", "0")
	command := exec.Command(supervisor.options.Executable, args...)
	command.Env = append([]string(nil), supervisor.options.Environment...)
	if command.Env == nil {
		command.Env = os.Environ()
	}
	configureSupervisorCommand(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, ReadinessRecord{}, nil, fmt.Errorf("open cyber-agent bearer pipe: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, ReadinessRecord{}, nil, fmt.Errorf("open cyber-agent readiness pipe: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, ReadinessRecord{}, nil, fmt.Errorf("open cyber-agent stderr pipe: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, ReadinessRecord{}, nil, fmt.Errorf("start cyber-agent: %w", err)
	}
	guard, err := attachSupervisorProcess(command)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, ReadinessRecord{}, nil, fmt.Errorf("contain cyber-agent process: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait(); close(wait) }()
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	if _, err := io.WriteString(stdin, token); err != nil {
		_ = stdin.Close()
		cleanupChild(command, guard, wait)
		return nil, ReadinessRecord{}, nil, fmt.Errorf("write cyber-agent bearer pipe: %w", err)
	}
	if err := stdin.Close(); err != nil {
		cleanupChild(command, guard, wait)
		return nil, ReadinessRecord{}, nil, fmt.Errorf("close cyber-agent bearer pipe: %w", err)
	}

	readyContext, cancel := context.WithTimeout(parent, supervisor.options.ReadinessTimeout)
	defer cancel()
	type readinessResult struct {
		record ReadinessRecord
		err    error
	}
	readinessChannel := make(chan readinessResult, 1)
	go func() {
		record, readErr := readReadiness(stdout, supervisor.options.ReadinessLimit)
		readinessChannel <- readinessResult{record: record, err: readErr}
	}()
	var readiness ReadinessRecord
	select {
	case result := <-readinessChannel:
		if result.err != nil {
			cleanupChild(command, guard, wait)
			return nil, ReadinessRecord{}, nil, result.err
		}
		readiness = result.record
	case err := <-wait:
		_ = guard.close()
		return nil, ReadinessRecord{}, nil, fmt.Errorf("cyber-agent exited before readiness: %w", normalizedWaitError(err))
	case <-readyContext.Done():
		cleanupChild(command, guard, wait)
		return nil, ReadinessRecord{}, nil, fmt.Errorf("cyber-agent readiness: %w", readyContext.Err())
	case <-supervisor.lifecycle.Done():
		cleanupChild(command, guard, wait)
		return nil, ReadinessRecord{}, nil, fmt.Errorf("cyber-agent readiness: %w", context.Canceled)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if err := validateReadiness(readiness, command.Process.Pid, supervisor.options); err != nil {
		cleanupChild(command, guard, wait)
		return nil, ReadinessRecord{}, nil, err
	}
	client, err := NewClient(ClientOptions{
		BaseURL: readiness.Endpoint, HTTPClient: supervisor.options.HTTPClient,
		TokenProvider: func(context.Context) (string, error) { return token, nil },
	})
	if err != nil {
		cleanupChild(command, guard, wait)
		return nil, ReadinessRecord{}, nil, err
	}
	capabilities, err := client.Capabilities(readyContext)
	if err != nil {
		cleanupChild(command, guard, wait)
		return nil, ReadinessRecord{}, nil, fmt.Errorf("negotiate cyber-agent capabilities: %w", err)
	}
	if err := validateCapabilities(readiness, capabilities, supervisor.options); err != nil {
		cleanupChild(command, guard, wait)
		return nil, ReadinessRecord{}, nil, err
	}
	return &supervisedChild{command: command, guard: guard, wait: wait}, readiness, client, nil
}

func (supervisor *Supervisor) monitor(child *supervisedChild) {
	current := child
	for {
		waitErr := <-current.wait
		_ = current.guard.close()
		supervisor.mu.Lock()
		if supervisor.stopping {
			supervisor.mu.Unlock()
			supervisor.finish(nil)
			return
		}
		supervisor.client = nil
		supervisor.child = nil
		supervisor.readiness = ReadinessRecord{}
		supervisor.mu.Unlock()

		lastErr := fmt.Errorf("cyber-agent process exited: %w", normalizedWaitError(waitErr))
		for {
			supervisor.mu.Lock()
			if supervisor.stopping {
				supervisor.mu.Unlock()
				supervisor.finish(nil)
				return
			}
			if supervisor.restarts >= supervisor.options.RestartLimit {
				supervisor.mu.Unlock()
				supervisor.finish(fmt.Errorf("cyber-agent restart budget exhausted: %w", lastErr))
				return
			}
			supervisor.restarts++
			supervisor.mu.Unlock()

			timer := time.NewTimer(supervisor.options.RestartDelay)
			select {
			case <-timer.C:
			case <-supervisor.stop:
				timer.Stop()
				supervisor.finish(nil)
				return
			}
			next, readiness, client, err := supervisor.startChild(supervisor.lifecycle)
			if err != nil {
				lastErr = err
				continue
			}
			supervisor.mu.Lock()
			if supervisor.stopping {
				supervisor.mu.Unlock()
				cleanupChild(next.command, next.guard, next.wait)
				supervisor.finish(nil)
				return
			}
			supervisor.child, supervisor.readiness, supervisor.client = next, readiness, client
			supervisor.mu.Unlock()
			current = next
			break
		}
	}
}

func (supervisor *Supervisor) finish(err error) {
	supervisor.mu.Lock()
	supervisor.client = nil
	supervisor.child = nil
	supervisor.readiness = ReadinessRecord{}
	if err != nil || supervisor.terminalErr == nil {
		supervisor.terminalErr = err
	}
	supervisor.mu.Unlock()
	supervisor.doneOnce.Do(func() { close(supervisor.done) })
}

func cleanupChild(command *exec.Cmd, guard supervisorProcessGuard, wait <-chan error) {
	_ = guard.kill(command.Process)
	<-wait
	_ = guard.close()
}

func readReadiness(reader io.Reader, limit int64) (ReadinessRecord, error) {
	buffered := bufio.NewReader(io.LimitReader(reader, limit+1))
	line, err := buffered.ReadBytes('\n')
	if int64(len(line)) > limit {
		return ReadinessRecord{}, fmt.Errorf("cyber-agent readiness exceeds %d bytes", limit)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return ReadinessRecord{}, fmt.Errorf("read cyber-agent readiness: %w", err)
	}
	if len(strings.TrimSpace(string(line))) == 0 {
		return ReadinessRecord{}, fmt.Errorf("cyber-agent readiness record is empty")
	}
	var record ReadinessRecord
	decoder := json.NewDecoder(strings.NewReader(string(line)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return ReadinessRecord{}, fmt.Errorf("decode cyber-agent readiness: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return ReadinessRecord{}, fmt.Errorf("decode cyber-agent readiness: trailing JSON content")
	}
	return record, nil
}

func validateReadiness(record ReadinessRecord, childPID int, options SupervisorOptions) error {
	if record.PID != childPID || record.PID <= 0 {
		return fmt.Errorf("cyber-agent readiness PID does not match supervised process")
	}
	if record.ProtocolVersion != options.ProtocolVersion {
		return fmt.Errorf("cyber-agent protocol version %d is incompatible with required version %d", record.ProtocolVersion, options.ProtocolVersion)
	}
	version, err := parseVersion(record.Version)
	if err != nil {
		return fmt.Errorf("cyber-agent readiness version is invalid: %w", err)
	}
	minimum, _ := parseVersion(options.MinimumVersion)
	if compareVersion(version, minimum) < 0 {
		return fmt.Errorf("cyber-agent version %s is older than required %s", record.Version, options.MinimumVersion)
	}
	endpoint, err := url.Parse(record.Endpoint)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("cyber-agent readiness endpoint is invalid")
	}
	host, port, err := net.SplitHostPort(endpoint.Host)
	if err != nil || port == "" || port == "0" || (!strings.EqualFold(host, "localhost") && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback())) {
		return fmt.Errorf("cyber-agent readiness endpoint must use a loopback address and non-zero port")
	}
	return nil
}

func validateCapabilities(readiness ReadinessRecord, capabilities RuntimeCapabilities, options SupervisorOptions) error {
	if capabilities.Product != "cyber-agent" {
		return fmt.Errorf("connected runtime identifies as %q, not cyber-agent", capabilities.Product)
	}
	if capabilities.ProtocolVersion != readiness.ProtocolVersion || capabilities.ProtocolVersion != options.ProtocolVersion {
		return fmt.Errorf("cyber-agent capability protocol version is incompatible")
	}
	if capabilities.RuntimeVersion != readiness.Version {
		return fmt.Errorf("cyber-agent readiness and capability versions do not match")
	}
	for _, required := range options.RequiredCapabilities {
		if !slices.Contains(capabilities.Capabilities, required) {
			return fmt.Errorf("cyber-agent capability %q is required", required)
		}
	}
	return nil
}

func generateBearer() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func normalizedWaitError(err error) error {
	if err == nil {
		return errors.New("process stopped")
	}
	return err
}

func parseVersion(value string) ([3]int, error) {
	var result [3]int
	normalized := strings.TrimPrefix(strings.TrimSpace(value), "v")
	core, _, _ := strings.Cut(normalized, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("expected major.minor.patch")
	}
	for index, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return result, fmt.Errorf("invalid numeric version component")
		}
		result[index] = parsed
	}
	return result, nil
}

func compareVersion(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
