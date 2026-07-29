// Package update performs explicit, read-only cyber-code version checks.
package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	SignatureHeader  = "X-Cyber-Code-Signature"
	maxMetadataBytes = 64 << 10
	defaultTimeout   = 3 * time.Second
	maximumTimeout   = 10 * time.Second
)

type Options struct {
	Enabled        bool
	CurrentVersion string
	MetadataURL    string
	PublicKey      ed25519.PublicKey
	Client         *http.Client
	Timeout        time.Duration
}

type Result struct {
	Checked         bool   `json:"checked"`
	UpdateAvailable bool   `json:"update_available"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	DownloadURL     string `json:"download_url,omitempty"`
}

type metadata struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
}

func Check(ctx context.Context, options Options) (Result, error) {
	result := Result{CurrentVersion: options.CurrentVersion}
	if !options.Enabled {
		return result, nil
	}
	metadataURL, err := secureURL(options.MetadataURL)
	if err != nil {
		return result, fmt.Errorf("validate update metadata URL: %w", err)
	}
	if len(options.PublicKey) != ed25519.PublicKeySize {
		return result, fmt.Errorf("validate update signing key: expected %d bytes", ed25519.PublicKeySize)
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if timeout > maximumTimeout {
		timeout = maximumTimeout
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(checkCtx, http.MethodGet, metadataURL.String(), nil)
	if err != nil {
		return result, fmt.Errorf("create update request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	checkedClient := *client
	originalRedirect := checkedClient.CheckRedirect
	checkedClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if _, err := secureURL(request.URL.String()); err != nil {
			return err
		}
		if originalRedirect != nil {
			return originalRedirect(request, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	response, err := checkedClient.Do(request)
	if err != nil {
		return result, fmt.Errorf("fetch update metadata: %w", err)
	}
	defer response.Body.Close()
	if response.Request != nil && response.Request.URL != nil {
		if _, err := secureURL(response.Request.URL.String()); err != nil {
			return result, fmt.Errorf("validate final update metadata URL: %w", err)
		}
	}
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("fetch update metadata: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return result, fmt.Errorf("read update metadata: %w", err)
	}
	if len(body) > maxMetadataBytes {
		return result, fmt.Errorf("read update metadata: response exceeds %d bytes", maxMetadataBytes)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(response.Header.Get(SignatureHeader)))
	if err != nil || !ed25519.Verify(options.PublicKey, body, signature) {
		return result, fmt.Errorf("verify update metadata signature: invalid signature")
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	var document metadata
	if err := decoder.Decode(&document); err != nil {
		return result, fmt.Errorf("decode update metadata: %w", err)
	}
	downloadURL, err := secureURL(document.DownloadURL)
	if err != nil {
		return result, fmt.Errorf("validate update download URL: %w", err)
	}
	newer, err := NewerVersion(options.CurrentVersion, document.Version)
	if err != nil {
		return result, err
	}
	result.Checked = true
	result.UpdateAvailable = newer
	result.LatestVersion = document.Version
	if newer {
		result.DownloadURL = downloadURL.String()
	}
	return result, nil
}

func secureURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("an absolute HTTPS URL without user information is required")
	}
	return parsed, nil
}

type semanticVersion struct {
	major, minor, patch int
	prerelease          string
}

func NewerVersion(current, latest string) (bool, error) {
	left, err := parseVersion(current)
	if err != nil {
		return false, fmt.Errorf("parse current version: %w", err)
	}
	right, err := parseVersion(latest)
	if err != nil {
		return false, fmt.Errorf("parse latest version: %w", err)
	}
	for _, pair := range [][2]int{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[1] != pair[0] {
			return pair[1] > pair[0], nil
		}
	}
	if left.prerelease == right.prerelease {
		return false, nil
	}
	if left.prerelease != "" && right.prerelease == "" {
		return true, nil
	}
	if left.prerelease == "" {
		return false, nil
	}
	return comparePrerelease(left.prerelease, right.prerelease) < 0, nil
}

func parseVersion(value string) (semanticVersion, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.SplitN(value, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(numbers) != 3 {
		return semanticVersion{}, fmt.Errorf("expected major.minor.patch")
	}
	parsed := semanticVersion{}
	values := []*int{&parsed.major, &parsed.minor, &parsed.patch}
	for index := range numbers {
		number, err := strconv.Atoi(numbers[index])
		if err != nil || number < 0 || (len(numbers[index]) > 1 && numbers[index][0] == '0') {
			return semanticVersion{}, fmt.Errorf("invalid numeric component %q", numbers[index])
		}
		*values[index] = number
	}
	if len(parts) == 2 {
		if strings.TrimSpace(parts[1]) == "" {
			return semanticVersion{}, fmt.Errorf("empty prerelease")
		}
		parsed.prerelease = parts[1]
	}
	return parsed, nil
}

func comparePrerelease(left, right string) int {
	leftParts, rightParts := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < len(leftParts) && index < len(rightParts); index++ {
		if leftParts[index] == rightParts[index] {
			continue
		}
		leftNumber, leftErr := strconv.Atoi(leftParts[index])
		rightNumber, rightErr := strconv.Atoi(rightParts[index])
		if leftErr == nil && rightErr == nil {
			if leftNumber < rightNumber {
				return -1
			}
			return 1
		}
		if leftErr == nil {
			return -1
		}
		if rightErr == nil || leftParts[index] > rightParts[index] {
			return 1
		}
		return -1
	}
	if len(leftParts) < len(rightParts) {
		return -1
	}
	if len(leftParts) > len(rightParts) {
		return 1
	}
	return 0
}
