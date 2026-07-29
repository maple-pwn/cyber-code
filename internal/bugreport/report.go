// Package bugreport creates bounded, redacted local diagnostic reports.
package bugreport

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"cyber-code/internal/security"
)

const (
	defaultMaxReportBytes = 64 << 10
	MaxIssueBodyBytes     = 8 << 10
)

type Options struct {
	Directory string
	Version   string
	GOOS      string
	GOARCH    string
	Now       func() time.Time
	MaxBytes  int
	Secrets   []string
}

type Input struct {
	Description string
	Diagnostics string
}

type Result struct {
	Path     string
	IssueURL string
}

func Create(ctx context.Context, options Options, input Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(options.Directory) == "" {
		return Result{}, fmt.Errorf("bug report directory is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultMaxReportBytes
	}
	if info, err := os.Lstat(options.Directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Result{}, fmt.Errorf("bug report directory must be a real directory")
		}
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("inspect bug report directory: %w", err)
	}
	if err := os.MkdirAll(options.Directory, 0o700); err != nil {
		return Result{}, fmt.Errorf("create bug report directory: %w", err)
	}
	if info, err := os.Lstat(options.Directory); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Result{}, fmt.Errorf("validate bug report directory: a real directory is required")
	}
	if err := os.Chmod(options.Directory, 0o700); err != nil {
		return Result{}, fmt.Errorf("restrict bug report directory: %w", err)
	}
	redactor := security.NewRedactor(options.Secrets...)
	content := fmt.Sprintf("# cyber-code bug report\n\ncreated: %s\nversion: %s\nplatform: %s/%s\n\n## Description\n\n%s\n\n## Diagnostics\n\n%s\n",
		options.Now().UTC().Format(time.RFC3339), options.Version, options.GOOS, options.GOARCH, input.Description, input.Diagnostics)
	content = truncateUTF8(redactor.Text(content), options.MaxBytes)
	path := filepath.Join(options.Directory, "bug-report-"+options.Now().UTC().Format("20060102T150405.000000000Z")+".md")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("create bug report: %w", err)
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.WriteString(content); err != nil {
		return Result{}, fmt.Errorf("write bug report: %w", err)
	}
	if err := file.Sync(); err != nil {
		return Result{}, fmt.Errorf("sync bug report: %w", err)
	}
	if err := file.Close(); err != nil {
		return Result{}, fmt.Errorf("close bug report: %w", err)
	}
	keep = true
	return Result{Path: path}, nil
}

func BuildIssueURL(base, title, body string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("issue URL must be an absolute HTTPS URL without user information")
	}
	query := parsed.Query()
	query.Set("title", truncateUTF8(title, 256))
	query.Set("body", truncateUTF8(body, MaxIssueBodyBytes))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func truncateUTF8(value string, maximum int) string {
	if maximum <= 0 || len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
