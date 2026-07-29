package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

type OAuthAuthorizationOptions struct {
	AuthorizationURL string
	TokenURL         string
	ClientID         string
	ClientSecret     string
	Scopes           []string
	HTTPClient       *http.Client
	Now              func() time.Time
	OpenBrowser      bool
	BrowserOpener    func(string) error
}

type OAuthLogin struct {
	AuthorizationURL string
	BrowserOpened    bool
}
type oauthAuthorizationResult struct {
	credential Credential
	err        error
}

type OAuthAuthorizationFlow struct {
	options      OAuthAuthorizationOptions
	mu           sync.Mutex
	started      bool
	result       chan oauthAuthorizationResult
	server       *http.Server
	codeVerifier string
}

func NewOAuthAuthorizationFlow(options OAuthAuthorizationOptions) (*OAuthAuthorizationFlow, error) {
	authorize, err := url.Parse(options.AuthorizationURL)
	if err != nil || authorize.Scheme != "https" || authorize.Hostname() == "" {
		return nil, errors.New("MCP OAuth authorization URL must use HTTPS")
	}
	token, tokenErr := url.Parse(options.TokenURL)
	if tokenErr != nil || token.Hostname() == "" || (token.Scheme != "https" && !(token.Scheme == "http" && isLoopbackHost(token.Hostname()))) {
		return nil, errors.New("MCP OAuth token URL must use HTTPS")
	}
	if strings.TrimSpace(options.ClientID) == "" {
		return nil, errors.New("MCP OAuth client ID is required")
	}
	return &OAuthAuthorizationFlow{options: options, result: make(chan oauthAuthorizationResult, 1)}, nil
}

func (flow *OAuthAuthorizationFlow) Start(ctx context.Context) (OAuthLogin, error) {
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.started {
		return OAuthLogin{}, errors.New("MCP OAuth authorization already started")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return OAuthLogin{}, fmt.Errorf("bind OAuth callback listener: %w", err)
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		_ = listener.Close()
		return OAuthLogin{}, fmt.Errorf("generate OAuth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	// Generate PKCE code_verifier and code_challenge (S256).
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		_ = listener.Close()
		return OAuthLogin{}, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	codeVerifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	flow.codeVerifier = codeVerifier
	challengeHash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(challengeHash[:])

	redirectURI := "http://" + listener.Addr().String() + "/callback"
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	flow.server, flow.started = server, true
	mux.HandleFunc("/callback", func(writer http.ResponseWriter, request *http.Request) {
		if subtle.ConstantTimeCompare([]byte(request.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(writer, "invalid OAuth state — possible CSRF", http.StatusBadRequest)
			return
		}
		if errParam := request.URL.Query().Get("error"); errParam != "" {
			desc := request.URL.Query().Get("error_description")
			http.Error(writer, fmt.Sprintf("authorization denied: %s — %s", errParam, desc), http.StatusBadRequest)
			select {
			case flow.result <- oauthAuthorizationResult{err: fmt.Errorf("OAuth authorization denied: %s: %s", errParam, desc)}:
			default:
			}
			go server.Shutdown(context.Background())
			return
		}
		code := strings.TrimSpace(request.URL.Query().Get("code"))
		if code == "" {
			http.Error(writer, "authorization code is required", http.StatusBadRequest)
			return
		}
		credential, exchangeErr := flow.exchange(request.Context(), code, redirectURI)
		select {
		case flow.result <- oauthAuthorizationResult{credential: credential, err: exchangeErr}:
		default:
		}
		if exchangeErr != nil {
			http.Error(writer, "OAuth token exchange failed — see terminal for details", http.StatusBadGateway)
		} else {
			_, _ = io.WriteString(writer, "Authorization complete. You may close this window.")
		}
		go server.Shutdown(context.Background())
	})
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			select {
			case flow.result <- oauthAuthorizationResult{err: fmt.Errorf("MCP OAuth callback server failed: %w", serveErr)}:
			default:
			}
		}
	}()
	authorize, _ := url.Parse(flow.options.AuthorizationURL)
	query := authorize.Query()
	query.Set("response_type", "code")
	query.Set("client_id", flow.options.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", "S256")
	if len(flow.options.Scopes) > 0 {
		query.Set("scope", strings.Join(flow.options.Scopes, " "))
	}
	authorize.RawQuery = query.Encode()
	login := OAuthLogin{AuthorizationURL: authorize.String()}
	if flow.options.OpenBrowser {
		opener := flow.options.BrowserOpener
		if opener == nil {
			opener = openBrowser
		}
		login.BrowserOpened = opener(authorize.String()) == nil
	}
	return login, nil
}

func (flow *OAuthAuthorizationFlow) Wait(ctx context.Context) (Credential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case result := <-flow.result:
		return result.credential, result.err
	case <-ctx.Done():
		flow.mu.Lock()
		server := flow.server
		flow.mu.Unlock()
		if server != nil {
			_ = server.Shutdown(context.Background())
		}
		return Credential{}, ctx.Err()
	}
}

func (flow *OAuthAuthorizationFlow) exchange(ctx context.Context, code, redirectURI string) (Credential, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"client_id":    {flow.options.ClientID},
	}
	if flow.codeVerifier != "" {
		form.Set("code_verifier", flow.codeVerifier)
	}
	if flow.options.ClientSecret != "" {
		form.Set("client_secret", flow.options.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, flow.options.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, fmt.Errorf("create MCP OAuth token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := flow.options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return Credential{}, fmt.Errorf("send MCP OAuth token request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Credential{}, fmt.Errorf("read MCP OAuth token response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("MCP OAuth token exchange rejected (HTTP %d)", response.StatusCode)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.AccessToken) == "" {
		return Credential{}, errors.New("invalid MCP OAuth token response")
	}
	now := time.Now
	if flow.options.Now != nil {
		now = flow.options.Now
	}
	expiresAt := int64(0)
	if payload.ExpiresIn > 0 {
		expiresAt = now().Unix() + payload.ExpiresIn
	}
	return Credential{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, TokenType: payload.TokenType, ExpiresAt: expiresAt}, nil
}

// openBrowser attempts to open the given URL in the user's default browser.
func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "linux":
		return exec.Command("xdg-open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}
