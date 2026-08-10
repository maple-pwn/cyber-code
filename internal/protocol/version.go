package protocol

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type VersionInfo struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

var CurrentVersion = VersionInfo{Major: 1, Minor: 1}

type Negotiated struct {
	Version      VersionInfo
	Capabilities []string
}

var capabilityMinor = map[string]int{"base": 0, "permissions": 0, "ide-context": 1, "diff": 1}

func (version VersionInfo) String() string { return fmt.Sprintf("%d.%d", version.Major, version.Minor) }

func ParseVersion(value string) (VersionInfo, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 2 {
		return VersionInfo{}, fmt.Errorf("protocol version must use major.minor")
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil || major <= 0 || minor < 0 {
		return VersionInfo{}, fmt.Errorf("invalid protocol version %q", value)
	}
	return VersionInfo{Major: major, Minor: minor}, nil
}

func Negotiate(local, remote VersionInfo, localCapabilities, remoteCapabilities []string) (Negotiated, error) {
	if local.Major <= 0 || remote.Major <= 0 || local.Minor < 0 || remote.Minor < 0 || local.Major != remote.Major {
		return Negotiated{}, fmt.Errorf("incompatible protocol major versions %s and %s", local, remote)
	}
	minor := local.Minor
	if remote.Minor < minor {
		minor = remote.Minor
	}
	remoteSet := make(map[string]struct{}, len(remoteCapabilities))
	for _, capability := range remoteCapabilities {
		remoteSet[capability] = struct{}{}
	}
	seen := make(map[string]struct{})
	var capabilities []string
	for _, capability := range localCapabilities {
		requiredMinor, known := capabilityMinor[capability]
		if !known || requiredMinor > minor {
			continue
		}
		if _, supported := remoteSet[capability]; !supported {
			continue
		}
		if _, duplicate := seen[capability]; duplicate {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	if _, ok := seen["base"]; !ok {
		return Negotiated{}, fmt.Errorf("base protocol capability is required")
	}
	return Negotiated{Version: VersionInfo{Major: local.Major, Minor: minor}, Capabilities: capabilities}, nil
}
