// Package oauth provides OAuth authentication functionality for cyber-code.
package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/constants"
	"cyber-code/internal/credential"
)

// SubscriptionType represents the type of subscription
type SubscriptionType string

const (
	SubscriptionTypeMax        SubscriptionType = "max"
	SubscriptionTypePro        SubscriptionType = "pro"
	SubscriptionTypeEnterprise SubscriptionType = "enterprise"
	SubscriptionTypeTeam       SubscriptionType = "team"
)

// RateLimitTier represents the rate limit tier
type RateLimitTier string

// BillingType represents the billing type
type BillingType string

// OAuthTokens represents OAuth authentication tokens
type OAuthTokens struct {
	AccessToken      string           `json:"accessToken"`
	RefreshToken     string           `json:"refreshToken"`
	ExpiresAt        int64            `json:"expiresAt"` // Unix timestamp in milliseconds
	Scopes           []string         `json:"scopes"`
	SubscriptionType SubscriptionType `json:"subscriptionType,omitempty"`
	RateLimitTier    RateLimitTier    `json:"rateLimitTier,omitempty"`
}

// TokenExchangeResponse represents the response from token exchange
type TokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	Account      struct {
		UUID         string `json:"uuid"`
		EmailAddress string `json:"email_address"`
	} `json:"account"`
	Organization *struct {
		UUID string `json:"uuid"`
	} `json:"organization"`
}

// ProfileResponse represents the OAuth profile response
type ProfileResponse struct {
	Account struct {
		UUID        string `json:"uuid"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
	} `json:"account"`
	Organization struct {
		UUID                  string        `json:"uuid"`
		OrganizationType      string        `json:"organization_type"`
		RateLimitTier         RateLimitTier `json:"rate_limit_tier"`
		HasExtraUsageEnabled  bool          `json:"has_extra_usage_enabled"`
		BillingType           BillingType   `json:"billing_type"`
		SubscriptionCreatedAt string        `json:"subscription_created_at"`
	} `json:"organization"`
}

// OAuthClient handles OAuth authentication
type OAuthClient struct {
	httpClient *http.Client
	config     constants.OAuthConfig

	// Token management
	mu           sync.RWMutex
	cachedTokens *OAuthTokens
	store        credential.Store

	// Concurrent refresh deduplication
	pendingRefreshCheck *refreshCall
	pending401Handlers  map[string]chan bool
	pending401Mu        sync.Mutex

	// Lock for cross-process coordination
	lockPath string
}

type refreshCall struct {
	done    chan struct{}
	changed bool
	err     error
}

// OAuthClientOptions supplies the process-local dependencies used by OAuth.
// It exists so token refresh can be tested without global environment state.
type OAuthClientOptions struct {
	HTTPClient *http.Client
	Config     constants.OAuthConfig
	Store      credential.Store
	LockPath   string
}

// NewOAuthClient creates a new OAuth client
func NewOAuthClient() *OAuthClient {
	config := constants.GetOAuthConfig()

	// Get config directory
	configDir, _ := getConfigDir()
	return NewOAuthClientWithOptions(OAuthClientOptions{
		Config:   config,
		Store:    credentialStoreAt(filepath.Join(configDir, ".credentials.json")),
		LockPath: filepath.Join(configDir, ".oauth.lock"),
	})
}

// NewOAuthClientWithOptions creates an OAuth client with explicit dependencies.
func NewOAuthClientWithOptions(options OAuthClientOptions) *OAuthClient {
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if options.Config.TokenURL == "" {
		options.Config = constants.GetOAuthConfig()
	}
	if options.Store == nil {
		configDir, _ := getConfigDir()
		options.Store = credentialStoreAt(filepath.Join(configDir, ".credentials.json"))
	}
	if options.LockPath == "" {
		configDir, _ := getConfigDir()
		options.LockPath = filepath.Join(configDir, ".oauth.lock")
	}
	return &OAuthClient{httpClient: options.HTTPClient, config: options.Config, store: options.Store, lockPath: options.LockPath, pending401Handlers: make(map[string]chan bool)}
}

func credentialStoreAt(path string) credential.Store {
	store, err := credential.NewFileStore(path)
	if err != nil {
		return nil
	}
	return store
}

// GetOAuthTokens returns the current OAuth tokens
func (c *OAuthClient) GetOAuthTokens() *OAuthTokens {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cachedTokens
}

// SetOAuthTokens sets the OAuth tokens
func (c *OAuthClient) SetOAuthTokens(tokens *OAuthTokens) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedTokens = tokens
}

// LoadOAuthTokens loads OAuth tokens from storage
func (c *OAuthClient) LoadOAuthTokens() (*OAuthTokens, error) {
	// Check for environment variable override first
	if token := os.Getenv("CYBER_CODE_OAUTH_TOKEN"); token != "" {
		return &OAuthTokens{
			AccessToken: token,
			Scopes:      []string{constants.ClaudeAIInferenceScope},
		}, nil
	}

	// Check for OAuth token from file descriptor
	if token := os.Getenv("CYBER_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR"); token != "" {
		// Read from file descriptor (simplified for now)
		// In production, this would read from the actual FD
	}

	// Load from credentials file
	c.mu.RLock()
	if c.cachedTokens != nil {
		defer c.mu.RUnlock()
		return c.cachedTokens, nil
	}
	c.mu.RUnlock()

	if c.store == nil {
		return nil, fmt.Errorf("OAuth credential store is unavailable")
	}
	data, err := c.store.Load(context.Background())
	if err != nil {
		if errors.Is(err, credential.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var tokens OAuthTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}

	c.SetOAuthTokens(&tokens)
	return &tokens, nil
}

// SaveOAuthTokens saves OAuth tokens to storage
func (c *OAuthClient) SaveOAuthTokens(tokens *OAuthTokens) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}

	if c.store == nil {
		return fmt.Errorf("OAuth credential store is unavailable")
	}
	if err := c.store.Save(context.Background(), data); err != nil {
		return fmt.Errorf("save OAuth tokens: %w", err)
	}
	c.SetOAuthTokens(tokens)
	return nil
}

// IsOAuthTokenExpired checks if the OAuth token is expired
func IsOAuthTokenExpired(expiresAt int64) bool {
	if expiresAt == 0 {
		return false
	}

	// 5 minute buffer
	bufferTime := int64(5 * 60 * 1000)
	now := time.Now().UnixMilli()
	return now+bufferTime >= expiresAt
}

// ShouldUseClaudeAIAuth checks if the user has Claude.ai authentication scope
func ShouldUseClaudeAIAuth(scopes []string) bool {
	for _, scope := range scopes {
		if scope == constants.ClaudeAIInferenceScope {
			return true
		}
	}
	return false
}

// RefreshOAuthToken refreshes the OAuth token
func (c *OAuthClient) RefreshOAuthToken(ctx context.Context, refreshToken string, scopes []string) (*OAuthTokens, error) {
	// Use Claude AI scopes by default
	if len(scopes) == 0 {
		scopes = constants.ClaudeAIOAuthScopes
	}

	requestBody := map[string]interface{}{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.config.ClientID,
		"scope":         strings.Join(scopes, " "),
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	// Calculate expiration time
	expiresAt := time.Now().UnixMilli() + int64(tokenResp.ExpiresIn*1000)
	parsedScopes := parseScopes(tokenResp.Scope)

	// Create new tokens
	newTokens := &OAuthTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		Scopes:       parsedScopes,
	}

	// If refresh token is not returned, keep the old one
	if newTokens.RefreshToken == "" {
		newTokens.RefreshToken = refreshToken
	}

	// Fetch profile info for subscription type
	profile, err := c.fetchProfileInfo(ctx, tokenResp.AccessToken)
	if err == nil && profile != nil {
		newTokens.SubscriptionType = getSubscriptionTypeFromOrgType(profile.Organization.OrganizationType)
		newTokens.RateLimitTier = profile.Organization.RateLimitTier
	}

	return newTokens, nil
}

// fetchProfileInfo fetches the user's profile information
func (c *OAuthClient) fetchProfileInfo(ctx context.Context, accessToken string) (*ProfileResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseAPIURL+"/api/oauth/profile", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("profile fetch failed with status %d", resp.StatusCode)
	}

	var profile ProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return &profile, nil
}

// CheckAndRefreshOAuthTokenIfNeeded checks if the OAuth token needs refresh
func (c *OAuthClient) CheckAndRefreshOAuthTokenIfNeeded(ctx context.Context, force bool) (changed bool, err error) {
	// Deduplicate all concurrent refreshes. A force caller retries only when the
	// in-flight non-force check completed without refreshing a token.
	for {
		c.mu.Lock()
		if c.pendingRefreshCheck == nil {
			break
		}
		pending := c.pendingRefreshCheck
		c.mu.Unlock()
		select {
		case <-pending.done:
			if !force || pending.changed || pending.err != nil {
				return pending.changed, pending.err
			}
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

	// Mark refresh as in progress
	pending := &refreshCall{done: make(chan struct{})}
	c.pendingRefreshCheck = pending
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		pending.changed = changed
		pending.err = err
		if c.pendingRefreshCheck == pending {
			c.pendingRefreshCheck = nil
		}
		c.mu.Unlock()
		close(pending.done)
	}()

	// Load tokens
	tokens, err := c.LoadOAuthTokens()
	if err != nil {
		return false, err
	}

	// Skip if not force and not expired
	if !force {
		if tokens == nil || tokens.RefreshToken == "" || !IsOAuthTokenExpired(tokens.ExpiresAt) {
			return false, nil
		}
	}

	if tokens == nil || tokens.RefreshToken == "" {
		return false, nil
	}

	if !ShouldUseClaudeAIAuth(tokens.Scopes) {
		return false, nil
	}

	// Clear cache and reload
	c.SetOAuthTokens(nil)
	freshTokens, err := c.LoadOAuthTokens()
	if err != nil {
		return false, err
	}

	if freshTokens == nil || freshTokens.RefreshToken == "" || !IsOAuthTokenExpired(freshTokens.ExpiresAt) {
		return false, nil
	}

	// Acquire lock for cross-process coordination
	release, err := c.acquireLock()
	if err != nil {
		// Lock acquisition failed, another process is refreshing
		return false, nil
	}
	defer release()

	// Check one more time after acquiring lock
	c.SetOAuthTokens(nil)
	lockedTokens, err := c.LoadOAuthTokens()
	if err != nil {
		return false, err
	}

	if lockedTokens == nil || lockedTokens.RefreshToken == "" || !IsOAuthTokenExpired(lockedTokens.ExpiresAt) {
		return false, nil
	}

	// Refresh the token
	refreshedTokens, err := c.RefreshOAuthToken(ctx, lockedTokens.RefreshToken, nil)
	if err != nil {
		return false, err
	}

	// Save the new tokens
	if err := c.SaveOAuthTokens(refreshedTokens); err != nil {
		return false, err
	}

	return true, nil
}

// HandleOAuth401Error handles a 401 "OAuth token has expired" error from the API
func (c *OAuthClient) HandleOAuth401Error(ctx context.Context, failedAccessToken string) (bool, error) {
	// Deduplicate concurrent calls with the same failed token
	c.pending401Mu.Lock()
	if ch, exists := c.pending401Handlers[failedAccessToken]; exists {
		c.pending401Mu.Unlock()
		result := <-ch
		return result, nil
	}

	ch := make(chan bool, 1)
	c.pending401Handlers[failedAccessToken] = ch
	c.pending401Mu.Unlock()

	defer func() {
		c.pending401Mu.Lock()
		delete(c.pending401Handlers, failedAccessToken)
		c.pending401Mu.Unlock()
	}()

	result, err := c.handleOAuth401ErrorImpl(ctx, failedAccessToken)
	if err != nil {
		ch <- false
		return false, err
	}

	ch <- result
	return result, nil
}

func (c *OAuthClient) handleOAuth401ErrorImpl(ctx context.Context, failedAccessToken string) (bool, error) {
	// Clear cache and reload
	c.SetOAuthTokens(nil)
	currentTokens, err := c.LoadOAuthTokens()
	if err != nil {
		return false, err
	}

	if currentTokens == nil || currentTokens.RefreshToken == "" {
		return false, nil
	}

	// If keychain has a different token, another tab already refreshed - use it
	if currentTokens.AccessToken != failedAccessToken {
		return true, nil
	}

	// Same token that failed - force refresh
	return c.CheckAndRefreshOAuthTokenIfNeeded(ctx, true)
}

// acquireLock acquires a file lock for cross-process coordination
func (c *OAuthClient) acquireLock() (func(), error) {
	// Create lock directory if needed
	dir := filepath.Dir(c.lockPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	// Create lock file
	lockFile, err := os.OpenFile(c.lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("lock already held by another process")
		}
		return nil, err
	}

	return func() {
		lockFile.Close()
		os.Remove(c.lockPath)
	}, nil
}

// parseScopes parses a scope string into a slice
func parseScopes(scopeString string) []string {
	if scopeString == "" {
		return nil
	}
	return strings.Split(scopeString, " ")
}

// getSubscriptionTypeFromOrgType converts organization type to subscription type
func getSubscriptionTypeFromOrgType(orgType string) SubscriptionType {
	switch orgType {
	case "claude_max":
		return SubscriptionTypeMax
	case "claude_pro":
		return SubscriptionTypePro
	case "claude_enterprise":
		return SubscriptionTypeEnterprise
	case "claude_team":
		return SubscriptionTypeTeam
	default:
		return ""
	}
}

// getConfigDir returns the config directory
func getConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	configDir := filepath.Join(home, constants.ConfigDirName)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", err
	}

	return configDir, nil
}

// HasProfileScope checks if the token has the user:profile scope
func HasProfileScope(tokens *OAuthTokens) bool {
	if tokens == nil {
		return false
	}
	for _, scope := range tokens.Scopes {
		if scope == constants.ClaudeAIProfileScope {
			return true
		}
	}
	return false
}

// ClearOAuthTokenCache clears the OAuth token cache
func (c *OAuthClient) ClearOAuthTokenCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedTokens = nil
}
