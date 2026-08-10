package cyberagent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	DiscoverySourceExplicit    = "explicit"
	DiscoverySourceEnvironment = "environment"
	DiscoverySourcePATH        = "path"
	DiscoverySourcePlatform    = "platform"
)

var ErrRuntimeNotFound = errors.New("cyber-agent executable was not found")

type DiscoveryOptions struct {
	ExplicitPath     string
	Environment      []string
	LookPath         func(string) (string, error)
	GOOS             string
	HomeDir          string
	InstallLocations []string
}

type DiscoveryResult struct {
	Path   string
	Source string
}

func Discover(options DiscoveryOptions) (DiscoveryResult, error) {
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	goos := options.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	home := options.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}

	if path := strings.TrimSpace(options.ExplicitPath); path != "" {
		return discoveredFile(path, DiscoverySourceExplicit, goos)
	}
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	}
	if path := environmentValue(environment, "CYBER_AGENT_PATH", goos); strings.TrimSpace(path) != "" {
		return discoveredFile(path, DiscoverySourceEnvironment, goos)
	}
	if path, err := lookPath(executableName(goos)); err == nil && strings.TrimSpace(path) != "" {
		absolute, absErr := filepath.Abs(path)
		if absErr != nil {
			return DiscoveryResult{}, fmt.Errorf("resolve cyber-agent from PATH: %w", absErr)
		}
		return DiscoveryResult{Path: absolute, Source: DiscoverySourcePATH}, nil
	}

	locations := options.InstallLocations
	if locations == nil {
		locations = defaultInstallLocations(goos, home)
	}
	for _, candidate := range locations {
		if result, err := discoveredFile(candidate, DiscoverySourcePlatform, goos); err == nil {
			return result, nil
		}
	}
	return DiscoveryResult{}, ErrRuntimeNotFound
}

func discoveredFile(path, source, goos string) (DiscoveryResult, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("resolve cyber-agent executable: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("cyber-agent executable %q: %w", absolute, err)
	}
	if !info.Mode().IsRegular() {
		return DiscoveryResult{}, fmt.Errorf("cyber-agent executable %q is not a regular file", absolute)
	}
	if goos != "windows" && info.Mode().Perm()&0o111 == 0 {
		return DiscoveryResult{}, fmt.Errorf("cyber-agent executable %q is not executable", absolute)
	}
	return DiscoveryResult{Path: absolute, Source: source}, nil
}

func environmentValue(environment []string, key, goos string) string {
	for index := len(environment) - 1; index >= 0; index-- {
		name, value, found := strings.Cut(environment[index], "=")
		if found && (name == key || (goos == "windows" && strings.EqualFold(name, key))) {
			return value
		}
	}
	return ""
}

func executableName(goos string) string {
	if goos == "windows" {
		return "cyber-agent.exe"
	}
	return "cyber-agent"
}

func defaultInstallLocations(goos, home string) []string {
	name := executableName(goos)
	switch goos {
	case "windows":
		locations := []string{}
		for _, root := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles")} {
			if root != "" {
				locations = append(locations, filepath.Join(root, "cyber-agent", name))
			}
		}
		return locations
	case "darwin":
		return []string{filepath.Join(home, ".local", "bin", name), "/opt/homebrew/bin/" + name, "/usr/local/bin/" + name}
	default:
		return []string{filepath.Join(home, ".local", "bin", name), "/usr/local/bin/" + name, "/usr/bin/" + name}
	}
}
