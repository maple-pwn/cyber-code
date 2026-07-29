package api

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAPILoggerIsLocalDebugOnlyAndRedactsRawErrors(t *testing.T) {
	var output bytes.Buffer
	logger := NewAPILoggerWithWriter(&output)
	logger.LogAPIQuery(LogAPIQueryParams{Model: "model", MessagesLength: 2})
	logger.LogAPIError(LogAPIErrorParams{Model: "model", Error: errors.New("secret-token"), Status: "500"})
	logger.LogAPISuccess(LogAPISuccessParams{Model: "model"})
	if output.Len() != 0 {
		t.Fatalf("non-debug logger output = %q", output.String())
	}
	logger.LogAPIQuery(LogAPIQueryParams{Model: "model", MessagesLength: 2, Debug: true})
	logger.LogAPIError(LogAPIErrorParams{Model: "model", Error: errors.New("secret-token"), Status: "500", Debug: true})
	logger.LogAPISuccess(LogAPISuccessParams{Model: "model", Debug: true})
	if text := output.String(); !strings.Contains(text, "[API Query]") || !strings.Contains(text, "[API Error]") || !strings.Contains(text, "[API Success]") || strings.Contains(text, "secret-token") {
		t.Fatalf("debug logger output = %q", text)
	}
}

func TestAnthropicEnvironmentMetadataIsExplicitAndNonSensitive(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.example.test")
	t.Setenv("ANTHROPIC_MODEL", "model-a")
	t.Setenv("ANTHROPIC_SMALL_FAST_MODEL", "model-b")
	t.Setenv("ANTHROPIC_API_KEY", "must-not-appear")
	metadata := GetAnthropicEnvMetadata()
	if metadata["ANTHROPIC_BASE_URL"] != "https://api.example.test" || metadata["ANTHROPIC_MODEL"] != "model-a" || metadata["ANTHROPIC_SMALL_FAST_MODEL"] != "model-b" {
		t.Fatalf("metadata = %#v", metadata)
	}
	if _, exists := metadata["ANTHROPIC_API_KEY"]; exists {
		t.Fatalf("credential was included in metadata: %#v", metadata)
	}
}

func TestCompatibilityTimingAPIsRemainStateless(t *testing.T) {
	if GetBuildAgeMinutes() != 0 {
		t.Fatal("unknown build age must remain zero")
	}
	SetLastAPITimestamp(time.Unix(100, 0))
	if GetLastAPITimestamp() != nil {
		t.Fatal("compatibility timestamp API unexpectedly introduced global state")
	}
}
