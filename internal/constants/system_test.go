package constants

import (
	"runtime"
	"strings"
	"testing"

	"cyber-code/internal/product"
)

func TestGetUnameSRUsesStableRuntimeIdentity(t *testing.T) {
	want := runtime.GOOS + "/" + runtime.GOARCH
	if got := GetUnameSR(); got != want || got == "" {
		t.Fatalf("GetUnameSR() = %q, want %q", got, want)
	}
}

func TestSystemPrefixesAndAttributionUseProductIdentity(t *testing.T) {
	if got := GetCLISyspromptPrefix(true, true); got != AgentSDKCyberCodePresetPrefix {
		t.Fatalf("non-interactive appended prefix = %q", got)
	}
	previousVersion := product.BuildVersion
	product.BuildVersion = "9.8.7-test"
	t.Cleanup(func() { product.BuildVersion = previousVersion })
	t.Setenv("CYBER_CODE_ATTRIBUTION_HEADER", "false")
	header := GetAttributionHeader("fingerprint")
	if !strings.Contains(header, "cc_version=9.8.7-test.fingerprint") {
		t.Fatalf("attribution header = %q", header)
	}
}
