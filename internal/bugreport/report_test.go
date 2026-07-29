package bugreport

import (
	"context"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCreateWritesBoundedRedactedLocalOnlyReport(t *testing.T) {
	directory := t.TempDir()
	secret := "sk-bug-report-secret"
	result, err := Create(context.Background(), Options{
		Directory: directory, Version: "2.1.88", GOOS: "linux", GOARCH: "amd64",
		Now: func() time.Time { return time.Unix(123, 0).UTC() }, MaxBytes: 512,
	}, Input{Description: "A failure with api_key=" + secret, Diagnostics: strings.Repeat("diagnostic details ", 200)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path == "" || result.IssueURL != "" {
		t.Fatalf("result = %#v", result)
	}
	content, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > 512 || strings.Contains(string(content), secret) || !strings.Contains(string(content), "[REDACTED]") {
		t.Fatalf("report length=%d content=%q", len(content), content)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(result.Path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("mode=%v error=%v", info.Mode().Perm(), err)
		}
	}
}

func TestIssueURLIsOptionalAndContainsBoundedFields(t *testing.T) {
	issueURL, err := BuildIssueURL("https://github.com/maple-pwn/cyber-code/issues/new", "bug title", strings.Repeat("details ", 1000))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(issueURL)
	if err != nil || parsed.Scheme != "https" || parsed.Query().Get("title") != "bug title" || len(parsed.Query().Get("body")) > MaxIssueBodyBytes {
		t.Fatalf("issue URL = %q, error = %v", issueURL, err)
	}
	if _, err := BuildIssueURL("http://example.test/issues/new", "title", "body"); err == nil {
		t.Fatal("insecure issue URL was accepted")
	}
}

func TestCreateRefusesSymlinkReportDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	directory := root + string(os.PathSeparator) + "bug-reports"
	if err := os.Symlink(outside, directory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Create(context.Background(), Options{Directory: directory}, Input{Description: "test"}); err == nil {
		t.Fatal("symlink report directory was followed")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside entries=%v error=%v", entries, err)
	}
}
