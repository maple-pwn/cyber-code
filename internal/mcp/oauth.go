package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OAuthRefresher struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
	Now          func() time.Time
}

func (refresher OAuthRefresher) Refresh(ctx context.Context, _ string, previous Credential) (Credential, error) {
	endpoint, err := url.Parse(refresher.TokenURL)
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && isLoopbackHost(endpoint.Hostname()))) {
		return Credential{}, errors.New("MCP OAuth token URL must use HTTPS")
	}
	if strings.TrimSpace(previous.RefreshToken) == "" {
		return Credential{}, errors.New("MCP OAuth refresh token is unavailable")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {previous.RefreshToken}}
	if refresher.ClientID != "" {
		form.Set("client_id", refresher.ClientID)
	}
	if refresher.ClientSecret != "" {
		form.Set("client_secret", refresher.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, errors.New("create MCP OAuth refresh request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := refresher.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return Credential{}, errors.New("send MCP OAuth refresh request")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Credential{}, errors.New("read MCP OAuth refresh response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Credential{}, errors.New("MCP OAuth refresh was rejected")
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.AccessToken) == "" {
		return Credential{}, errors.New("invalid MCP OAuth refresh response")
	}
	if payload.RefreshToken == "" {
		payload.RefreshToken = previous.RefreshToken
	}
	now := time.Now
	if refresher.Now != nil {
		now = refresher.Now
	}
	expiresAt := int64(0)
	if payload.ExpiresIn > 0 {
		expiresAt = now().Unix() + payload.ExpiresIn
	}
	return Credential{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, TokenType: payload.TokenType, ExpiresAt: expiresAt}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
