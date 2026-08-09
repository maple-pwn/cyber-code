package cyberagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const defaultResponseLimit int64 = 4 << 20

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type TokenProvider func(context.Context) (string, error)

type ClientOptions struct {
	BaseURL       string
	HTTPClient    *http.Client
	TokenProvider TokenProvider
	ResponseLimit int64
}

type Client struct {
	baseURL       *url.URL
	httpClient    *http.Client
	tokenProvider TokenProvider
	responseLimit int64
}

type APIError struct {
	StatusCode int
	Detail     string
}

func (err *APIError) Error() string {
	if err.Detail == "" {
		return fmt.Sprintf("cyber-agent API returned HTTP %d", err.StatusCode)
	}
	return fmt.Sprintf("cyber-agent API returned HTTP %d: %s", err.StatusCode, err.Detail)
}

func NewClient(options ClientOptions) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(options.BaseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("cyber-agent base URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("cyber-agent base URL must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("cyber-agent base URL must not contain credentials, query, or fragment")
	}
	limit := options.ResponseLimit
	if limit == 0 {
		limit = defaultResponseLimit
	}
	if limit < 1 {
		return nil, fmt.Errorf("cyber-agent response limit must be positive")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		baseURL: parsed, httpClient: httpClient, tokenProvider: options.TokenProvider, responseLimit: limit,
	}, nil
}

func (client *Client) Capabilities(ctx context.Context) (RuntimeCapabilities, error) {
	var result RuntimeCapabilities
	err := client.doJSON(ctx, http.MethodGet, "/v1/capabilities", nil, "", &result)
	return result, err
}

func (client *Client) CreateSession(ctx context.Context, submission TaskSubmission, options CreateSessionOptions, key string) (SessionSnapshot, error) {
	query := url.Values{}
	setStringQuery(query, "agent_mode", options.AgentMode)
	setStringQuery(query, "autonomy", options.Autonomy)
	setStringQuery(query, "harness_ref", options.HarnessRef)
	for _, scope := range options.Scope {
		query.Add("scope", scope)
	}
	setIntQuery(query, "max_rounds", options.MaxRounds)
	setIntQuery(query, "max_tool_calls", options.MaxToolCalls)
	setIntQuery(query, "max_llm_calls", options.MaxLLMCalls)
	path := "/v1/sessions"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var result SessionSnapshot
	err := client.doJSON(ctx, http.MethodPost, path, submission, key, &result)
	return result, err
}

func (client *Client) Snapshot(ctx context.Context, sessionID string) (SessionSnapshot, error) {
	var result SessionSnapshot
	err := client.doJSON(ctx, http.MethodGet, sessionPath(sessionID), nil, "", &result)
	return result, err
}

func (client *Client) SubmitTurn(ctx context.Context, sessionID, content, key string) (SessionSnapshot, error) {
	var result SessionSnapshot
	err := client.doJSON(ctx, http.MethodPost, sessionPath(sessionID)+"/turns", map[string]string{"content": content}, key, &result)
	return result, err
}

func (client *Client) RespondInteraction(ctx context.Context, sessionID string, response InteractionResponse, key string) (SessionSnapshot, error) {
	var result SessionSnapshot
	err := client.doJSON(ctx, http.MethodPost, sessionPath(sessionID)+"/interactions", response, key, &result)
	return result, err
}

func (client *Client) ResumeSession(ctx context.Context, sessionID, key string) (SessionSnapshot, error) {
	return client.emptyMutation(ctx, sessionID, "resume", key)
}

func (client *Client) CancelSession(ctx context.Context, sessionID, key string) (SessionSnapshot, error) {
	return client.emptyMutation(ctx, sessionID, "cancel", key)
}

func (client *Client) CompactSession(ctx context.Context, sessionID, key string) (SessionSnapshot, error) {
	return client.emptyMutation(ctx, sessionID, "compact", key)
}

func (client *Client) ListSkills(ctx context.Context, query string) ([]SkillRecord, error) {
	path := "/v1/skills"
	if strings.TrimSpace(query) != "" {
		path += "?" + url.Values{"query": []string{strings.TrimSpace(query)}}.Encode()
	}
	var result SkillList
	err := client.doJSON(ctx, http.MethodGet, path, nil, "", &result)
	return result.Skills, err
}

func (client *Client) InstallSkill(ctx context.Context, skillRef, version, key string) (SkillRecord, error) {
	body := map[string]string{"skill_ref": skillRef}
	if strings.TrimSpace(version) != "" {
		body["version"] = strings.TrimSpace(version)
	}
	var result SkillRecord
	err := client.doJSON(ctx, http.MethodPost, "/v1/skills", body, key, &result)
	return result, err
}

func (client *Client) TrustSkill(ctx context.Context, skillRef, trust, key string) (SkillRecord, error) {
	var result SkillRecord
	path := "/v1/skills/" + url.PathEscape(skillRef) + "/trust"
	err := client.doJSON(ctx, http.MethodPost, path, map[string]string{"trust": trust}, key, &result)
	return result, err
}

func (client *Client) RemoveSkill(ctx context.Context, skillRef string) (SkillRemoveReceipt, error) {
	var result SkillRemoveReceipt
	err := client.doJSON(ctx, http.MethodDelete, "/v1/skills/"+url.PathEscape(skillRef), nil, "remove-skill", &result)
	return result, err
}

func (client *Client) emptyMutation(ctx context.Context, sessionID, operation, key string) (SessionSnapshot, error) {
	var result SessionSnapshot
	err := client.doJSON(ctx, http.MethodPost, sessionPath(sessionID)+"/"+operation, nil, key, &result)
	return result, err
}

func (client *Client) doJSON(ctx context.Context, method, path string, requestBody any, idempotencyKey string, result any) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode cyber-agent request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := client.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		if !idempotencyKeyPattern.MatchString(idempotencyKey) {
			return fmt.Errorf("valid cyber-agent idempotency key is required")
		}
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("cyber-agent request: %w", err)
	}
	defer response.Body.Close()
	payload, err := readBounded(response.Body, client.responseLimit)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeAPIError(response.StatusCode, payload)
	}
	if err := decodeStrict(payload, result); err != nil {
		return fmt.Errorf("decode cyber-agent response: %w", err)
	}
	return nil
}

func (client *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	endpoint := strings.TrimRight(client.baseURL.String(), "/") + path
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build cyber-agent request: %w", err)
	}
	if client.tokenProvider != nil {
		token, err := client.tokenProvider(ctx)
		if err != nil {
			return nil, fmt.Errorf("get cyber-agent token: %w", err)
		}
		if strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("cyber-agent token provider returned a blank token")
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read cyber-agent response: %w", err)
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("cyber-agent response exceeds %d bytes", limit)
	}
	return payload, nil
}

func decodeStrict(payload []byte, result any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON content")
		}
		return fmt.Errorf("trailing JSON content: %w", err)
	}
	return nil
}

func decodeAPIError(statusCode int, payload []byte) error {
	var body struct {
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(payload, &body)
	return &APIError{StatusCode: statusCode, Detail: body.Detail}
}

func sessionPath(sessionID string) string {
	return "/v1/sessions/" + url.PathEscape(sessionID)
}

func setStringQuery(query url.Values, key, value string) {
	if value != "" {
		query.Set(key, value)
	}
}

func setIntQuery(query url.Values, key string, value int) {
	if value > 0 {
		query.Set(key, strconv.Itoa(value))
	}
}
