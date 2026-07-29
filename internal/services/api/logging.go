package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// GlobalCacheStrategy represents the strategy used for global prompt caching
type GlobalCacheStrategy string

const (
	GlobalCacheStrategyToolBased    GlobalCacheStrategy = "tool_based"
	GlobalCacheStrategySystemPrompt GlobalCacheStrategy = "system_prompt"
	GlobalCacheStrategyNone         GlobalCacheStrategy = "none"
)

// KnownGateway represents a known AI gateway
type KnownGateway string

const (
	GatewayLiteLLM             KnownGateway = "litellm"
	GatewayHelicone            KnownGateway = "helicone"
	GatewayPortkey             KnownGateway = "portkey"
	GatewayCloudflareAIGateway KnownGateway = "cloudflare-ai-gateway"
	GatewayKong                KnownGateway = "kong"
	GatewayBraintrust          KnownGateway = "braintrust"
	GatewayDatabricks          KnownGateway = "databricks"
)

// Gateway fingerprints for detecting AI gateways from response headers
var gatewayFingerprints = map[KnownGateway][]string{
	GatewayLiteLLM:             {"x-litellm-"},
	GatewayHelicone:            {"helicone-"},
	GatewayPortkey:             {"x-portkey-"},
	GatewayCloudflareAIGateway: {"cf-aig-"},
	GatewayKong:                {"x-kong-"},
	GatewayBraintrust:          {"x-bt-"},
}

// Gateway host suffixes for detecting gateways from base URL
var gatewayHostSuffixes = map[KnownGateway][]string{
	GatewayDatabricks: {
		".cloud.databricks.com",
		".azuredatabricks.net",
		".gcp.databricks.com",
	},
}

// DetectGateway detects the AI gateway from response headers or base URL
func DetectGateway(headers http.Header, baseURL string) KnownGateway {
	// Check headers first
	if headers != nil {
		for gw, prefixes := range gatewayFingerprints {
			for _, prefix := range prefixes {
				for key := range headers {
					if strings.HasPrefix(strings.ToLower(key), prefix) {
						return gw
					}
				}
			}
		}
	}

	// Check base URL
	if baseURL != "" {
		// Parse hostname
		host := baseURL
		if strings.Contains(baseURL, "://") {
			parts := strings.SplitN(baseURL, "://", 2)
			if len(parts) > 1 {
				host = strings.SplitN(parts[1], "/", 2)[0]
			}
		}
		host = strings.ToLower(host)

		for gw, suffixes := range gatewayHostSuffixes {
			for _, suffix := range suffixes {
				if strings.HasSuffix(host, suffix) {
					return gw
				}
			}
		}
	}

	return ""
}

// APILogger emits local diagnostics only when a call explicitly enables debug.
type APILogger struct{ writer io.Writer }

// NewAPILogger creates a new API logger
func NewAPILogger() *APILogger {
	return NewAPILoggerWithWriter(os.Stderr)
}

// NewAPILoggerWithWriter creates a local debug logger with an injectable sink.
func NewAPILoggerWithWriter(writer io.Writer) *APILogger {
	if writer == nil {
		writer = io.Discard
	}
	return &APILogger{writer: writer}
}

// LogAPIQuery logs an API query event
func (l *APILogger) LogAPIQuery(params LogAPIQueryParams) {
	if !params.Debug || l == nil {
		return
	}
	_, _ = fmt.Fprintf(l.writer, "[API Query] model=%s messages=%d temp=%.2f\n",
		params.Model, params.MessagesLength, params.Temperature)
}

// LogAPIError logs an API error event
func (l *APILogger) LogAPIError(params LogAPIErrorParams) {
	if !params.Debug || l == nil {
		return
	}
	gateway := DetectGateway(params.Headers, params.BaseURL)
	_, _ = fmt.Fprintf(l.writer, "[API Error] model=%s status=%s gateway=%s attempt=%d\n",
		params.Model, params.Status, gateway, params.Attempt)
}

// LogAPISuccess logs a successful API call
func (l *APILogger) LogAPISuccess(params LogAPISuccessParams) {
	if !params.Debug || l == nil {
		return
	}
	gateway := DetectGateway(params.Headers, params.BaseURL)
	usage := NonNullableUsage{}
	if params.Usage != nil {
		usage = *params.Usage
	}
	_, _ = fmt.Fprintf(l.writer, "[API Success] model=%s tokens_in=%d tokens_out=%d duration=%dms gateway=%s\n",
		params.Model, usage.InputTokens, usage.OutputTokens, params.DurationMs, gateway)
}

// LogAPIQueryParams contains parameters for logging an API query
type LogAPIQueryParams struct {
	Model          string
	MessagesLength int
	Temperature    float64
	Betas          []string
	PermissionMode string
	QuerySource    string
	ThinkingType   string
	EffortValue    string
	FastMode       bool
	Debug          bool
}

// LogAPIErrorParams contains parameters for logging an API error
type LogAPIErrorParams struct {
	Error                      error
	Model                      string
	MessageCount               int
	MessageTokens              int
	DurationMs                 int64
	DurationMsIncludingRetries int64
	Attempt                    int
	RequestID                  string
	ClientRequestID            string
	DidFallBackToNonStreaming  bool
	PromptCategory             string
	Headers                    http.Header
	BaseURL                    string
	Status                     string
	FastMode                   bool
	Debug                      bool
}

// LogAPISuccessParams contains parameters for logging a successful API call
type LogAPISuccessParams struct {
	Model                      string
	PreNormalizedModel         string
	MessageCount               int
	MessageTokens              int
	Usage                      *NonNullableUsage
	DurationMs                 int64
	DurationMsIncludingRetries int64
	Attempt                    int
	TTFTMs                     int64
	RequestID                  string
	StopReason                 string
	CostUSD                    float64
	DidFallBackToNonStreaming  bool
	QuerySource                string
	Headers                    http.Header
	BaseURL                    string
	PermissionMode             string
	GlobalCacheStrategy        GlobalCacheStrategy
	TextContentLength          int
	ThinkingContentLength      int
	ToolUseContentLengths      map[string]int
	FastMode                   bool
	Debug                      bool
}

// GetBuildAgeMinutes returns the age of the build in minutes
func GetBuildAgeMinutes() int {
	// Release builds do not currently carry a build timestamp.
	return 0
}

// GetAnthropicEnvMetadata returns metadata about Anthropic environment variables
func GetAnthropicEnvMetadata() map[string]string {
	metadata := make(map[string]string)
	for _, name := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			metadata[name] = value
		}
	}
	return metadata
}

// GetLastAPITimestamp returns the last API call timestamp for tracking intervals
func GetLastAPITimestamp() *time.Time {
	// Retained for compatibility; API logging intentionally has no global state.
	return nil
}

// SetLastAPITimestamp sets the last API call timestamp
func SetLastAPITimestamp(time.Time) {}
