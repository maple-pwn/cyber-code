package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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

	"cyber-code/internal/credential"
)

const (
	defaultBrowserStateTTL  = 10 * time.Minute
	maxBrowserCallbackSize  = 8 << 10
	maxBrowserTokenResponse = 1 << 20
)

var (
	errBrowserReplay  = errors.New("OAuth callback has already been used")
	errBrowserExpired = errors.New("OAuth callback has expired")
)

// BrowserLoginOptions explicitly enables the optional browser-based OIDC flow.
// Existing environment and file credential discovery is unaffected.
type BrowserLoginOptions struct {
	AuthorizationURL string
	TokenURL         string
	ClientID         string
	Scopes           []string
	CallbackAddress  string
	HTTPClient       *http.Client
	StateTTL         time.Duration
	Now              func() time.Time
	OpenBrowser      bool
	BrowserOpener    func(string) error
}

// BrowserLogin is a pending loopback OIDC login.
type BrowserLogin struct {
	AuthorizationURL string
	RedirectURI      string
	BrowserOpened    bool
	codeVerifier     string
	transaction      *browserTransaction
	server           *http.Server
	listener         net.Listener
	result           chan browserLoginResult
	ctx              context.Context
	cancel           context.CancelFunc
	client           *OAuthClient
	options          BrowserLoginOptions
	mu               sync.Mutex
}

type browserLoginResult struct {
	tokens *OAuthTokens
	err    error
}

type browserTransaction struct {
	state     string
	nonce     string
	expiresAt time.Time
	claimed   bool
	stateUsed bool
	nonceUsed bool
	invalid   bool
	mu        sync.Mutex
}

func newBrowserTransaction(now time.Time, ttl time.Duration) (*browserTransaction, error) {
	state, err := randomBrowserValue()
	if err != nil {
		return nil, err
	}
	nonce, err := randomBrowserValue()
	if err != nil {
		return nil, err
	}
	return &browserTransaction{state: state, nonce: nonce, expiresAt: now.Add(ttl)}, nil
}

func randomBrowserValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate OAuth transaction value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (transaction *browserTransaction) begin(state string, now time.Time) error {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if now.After(transaction.expiresAt) {
		return errBrowserExpired
	}
	if transaction.invalid || transaction.stateUsed || transaction.claimed {
		return errBrowserReplay
	}
	if !constantTimeEqual(state, transaction.state) {
		return errors.New("invalid OAuth callback state")
	}
	transaction.claimed = true
	transaction.stateUsed = true
	return nil
}

func (transaction *browserTransaction) finish(nonce string, now time.Time) error {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if now.After(transaction.expiresAt) {
		return errBrowserExpired
	}
	if !transaction.claimed || transaction.nonceUsed {
		return errBrowserReplay
	}
	if !constantTimeEqual(nonce, transaction.nonce) {
		// A nonce mismatch is terminal: the authorization response may not be
		// exchanged again with the same state, even if the first exchange failed.
		transaction.invalid = true
		transaction.claimed = false
		return errors.New("invalid OAuth callback nonce")
	}
	transaction.nonceUsed = true
	transaction.claimed = false
	return nil
}

func (transaction *browserTransaction) consume(state, nonce string, now time.Time) error {
	if err := transaction.begin(state, now); err != nil {
		return err
	}
	return transaction.finish(nonce, now)
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtleCompare([]byte(a), []byte(b))
}

func subtleCompare(a, b []byte) bool {
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// BeginBrowserLogin starts an explicit OIDC authorization-code + PKCE flow.
func (c *OAuthClient) BeginBrowserLogin(parent context.Context, options BrowserLoginOptions) (*BrowserLogin, error) {
	if parent == nil {
		parent = context.Background()
	}
	if strings.TrimSpace(options.AuthorizationURL) == "" {
		return nil, errors.New("OAuth authorization URL is required")
	}
	authorization, err := url.Parse(options.AuthorizationURL)
	if err != nil || authorization.Scheme != "https" || authorization.Hostname() == "" {
		return nil, errors.New("OAuth authorization URL must use HTTPS")
	}
	if options.TokenURL == "" {
		options.TokenURL = c.config.TokenURL
	}
	if err := validateOAuthEndpoint(options.TokenURL, "OAuth token URL"); err != nil {
		return nil, err
	}
	if options.ClientID == "" {
		options.ClientID = c.config.ClientID
	}
	if options.ClientID == "" {
		return nil, errors.New("OAuth client ID is required")
	}
	address := options.CallbackAddress
	if address == "" {
		address = "127.0.0.1:0"
	}
	if err := validateLoopbackEphemeral(address); err != nil {
		return nil, err
	}
	ttl := options.StateTTL
	if ttl <= 0 {
		ttl = defaultBrowserStateTTL
	}
	if ttl > 30*time.Minute {
		return nil, errors.New("OAuth state TTL is too long")
	}
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	transaction, err := newBrowserTransaction(now(), ttl)
	if err != nil {
		return nil, err
	}
	verifier, err := randomBrowserValue()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, fmt.Errorf("bind OAuth callback listener: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	login := &BrowserLogin{codeVerifier: verifier, transaction: transaction, listener: listener, result: make(chan browserLoginResult, 1), ctx: ctx, cancel: cancel, client: c, options: options}
	redirectURI := "http://" + listener.Addr().String() + "/callback"
	login.RedirectURI = redirectURI
	login.server = &http.Server{Handler: http.HandlerFunc(login.callbackHandler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: maxBrowserCallbackSize}
	query := authorization.Query()
	hash := sha256.Sum256([]byte(verifier))
	query.Set("response_type", "code")
	query.Set("client_id", options.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", transaction.state)
	query.Set("nonce", transaction.nonce)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(hash[:]))
	query.Set("code_challenge_method", "S256")
	if len(options.Scopes) > 0 {
		query.Set("scope", strings.Join(options.Scopes, " "))
	}
	authorization.RawQuery = query.Encode()
	login.AuthorizationURL = authorization.String()
	go func() {
		if err := login.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			login.publish(nil, errors.New("OAuth callback server failed"))
		}
	}()
	if options.OpenBrowser {
		opener := options.BrowserOpener
		if opener == nil {
			opener = openBrowser
		}
		login.BrowserOpened = opener(login.AuthorizationURL) == nil
	}
	return login, nil
}

func validateOAuthEndpoint(raw, label string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackBrowserHost(parsed.Hostname()))) {
		return fmt.Errorf("%s must use HTTPS", label)
	}
	return nil
}

func isLoopbackBrowserHost(host string) bool { return host == "127.0.0.1" || host == "localhost" }

func validateLoopbackEphemeral(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || portText != "0" {
		return errors.New("OAuth callback address must be loopback with an ephemeral port")
	}
	return nil
}

func (login *BrowserLogin) callbackHandler(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if len(request.RequestURI) > maxBrowserCallbackSize || request.ContentLength > maxBrowserCallbackSize {
		writer.WriteHeader(http.StatusRequestURITooLong)
		return
	}
	if request.Host != login.listener.Addr().String() || request.URL.Path != "/callback" {
		http.Error(writer, "OAuth callback redirect does not match", http.StatusBadRequest)
		return
	}
	state := request.URL.Query().Get("state")
	if err := login.transaction.begin(state, login.now()); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if callbackErr := strings.TrimSpace(request.URL.Query().Get("error")); callbackErr != "" {
		login.publish(nil, errors.New("OAuth authorization was denied"))
		http.Error(writer, "OAuth authorization was denied", http.StatusBadRequest)
		go login.shutdown()
		return
	}
	code := strings.TrimSpace(request.URL.Query().Get("code"))
	if code == "" || len(code) > 4096 {
		http.Error(writer, "OAuth authorization code is required", http.StatusBadRequest)
		return
	}
	tokens, err := login.exchange(request.Context(), code)
	if err == nil {
		err = login.transaction.finish(tokens.oidcNonce, login.now())
	}
	if err != nil {
		login.publish(nil, err)
		http.Error(writer, "OAuth token exchange failed", http.StatusBadGateway)
		return
	}
	if err := login.client.SaveOAuthTokens(tokens.OAuthTokens); err != nil {
		login.publish(nil, errors.New("save OAuth credentials failed"))
		http.Error(writer, "OAuth credential storage failed", http.StatusInternalServerError)
		return
	}
	login.publish(tokens.OAuthTokens, nil)
	_, _ = io.WriteString(writer, "Authorization complete. You may close this window.")
	go login.shutdown()
}

func (login *BrowserLogin) now() time.Time {
	if login.options.Now != nil {
		return login.options.Now()
	}
	return time.Now()
}

type browserExchangeTokens struct {
	*OAuthTokens
	oidcNonce string
}

func (login *BrowserLogin) exchange(ctx context.Context, code string) (browserExchangeTokens, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {login.RedirectURI}, "client_id": {login.options.ClientID}, "code_verifier": {login.codeVerifier}}
	exchangeContext, cancel := context.WithCancel(login.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer func() { stop(); cancel() }()
	request, err := http.NewRequestWithContext(exchangeContext, http.MethodPost, login.options.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return browserExchangeTokens{}, errors.New("create OAuth token request failed")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := login.options.HTTPClient
	if client == nil {
		client = login.client.httpClient
	}
	response, err := client.Do(request)
	if err != nil {
		return browserExchangeTokens{}, errors.New("OAuth token request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return browserExchangeTokens{}, fmt.Errorf("OAuth token exchange rejected (HTTP %d)", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBrowserTokenResponse+1))
	if err != nil || len(body) > maxBrowserTokenResponse {
		return browserExchangeTokens{}, errors.New("OAuth token response is invalid")
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		IDToken      string `json:"id_token"`
	}
	if json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.AccessToken) == "" {
		return browserExchangeTokens{}, errors.New("OAuth token response is invalid")
	}
	nonce, err := oidcNonce(payload.IDToken)
	if err != nil {
		return browserExchangeTokens{}, err
	}
	expiresAt := int64(0)
	if payload.ExpiresIn > 0 {
		expiresAt = login.now().Add(time.Duration(payload.ExpiresIn) * time.Second).UnixMilli()
	}
	return browserExchangeTokens{OAuthTokens: &OAuthTokens{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, ExpiresAt: expiresAt, Scopes: parseScopes(payload.Scope)}, oidcNonce: nonce}, nil
}

func oidcNonce(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", errors.New("OAuth identity token is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(payload) > maxBrowserCallbackSize {
		return "", errors.New("OAuth identity token is invalid")
	}
	var claims struct {
		Nonce string `json:"nonce"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Nonce == "" {
		return "", errors.New("OAuth identity token is invalid")
	}
	return claims.Nonce, nil
}

func (login *BrowserLogin) publish(tokens *OAuthTokens, err error) {
	select {
	case login.result <- browserLoginResult{tokens: tokens, err: err}:
	default:
	}
}

func (login *BrowserLogin) Wait(ctx context.Context) (*OAuthTokens, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case result := <-login.result:
		login.shutdown()
		return result.tokens, result.err
	case <-ctx.Done():
		login.cancel()
		login.shutdown()
		return nil, ctx.Err()
	}
}

func (login *BrowserLogin) shutdown() {
	login.mu.Lock()
	defer login.mu.Unlock()
	if login.server != nil {
		_ = login.server.Shutdown(context.Background())
		login.server = nil
	}
	if login.cancel != nil {
		login.cancel()
		login.cancel = nil
	}
}

// Logout revokes remote tokens when possible and always clears locally stored credentials.
func (c *OAuthClient) Logout(ctx context.Context, revocationURL string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tokens, loadErr := c.LoadOAuthTokens()
	var localErr error
	if c.store != nil {
		if err := c.store.Save(context.Background(), []byte(`{}`)); err != nil {
			localErr = err
		}
	}
	c.ClearOAuthTokenCache()
	if loadErr != nil && !errors.Is(loadErr, credential.ErrNotFound) {
		return errors.New("local OAuth credentials cleared; unable to load prior credentials")
	}
	if tokens == nil || (tokens.AccessToken == "" && tokens.RefreshToken == "") {
		if localErr != nil {
			return errors.New("local OAuth credentials could not be cleared")
		}
		return nil
	}
	if strings.TrimSpace(revocationURL) == "" {
		return errors.New("local OAuth credentials cleared; remote token revocation is unavailable")
	}
	if err := validateOAuthEndpoint(revocationURL, "OAuth revocation URL"); err != nil {
		return errors.New("local OAuth credentials cleared; remote token revocation is unavailable")
	}
	var revokeErr error
	for _, token := range []string{tokens.RefreshToken, tokens.AccessToken} {
		if token == "" {
			continue
		}
		if err := c.revoke(ctx, revocationURL, token); err != nil {
			revokeErr = err
		}
	}
	if revokeErr != nil {
		if localErr != nil {
			return errors.New("local OAuth credential cleanup and remote token revocation failed")
		}
		return errors.New("local OAuth credentials cleared; remote token revocation failed")
	}
	if localErr != nil {
		return errors.New("remote tokens revoked; local OAuth credentials could not be cleared")
	}
	return nil
}

func (c *OAuthClient) revoke(ctx context.Context, endpoint, token string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(url.Values{"token": {token}, "client_id": {c.config.ClientID}}.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxBrowserCallbackSize))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("revocation rejected")
	}
	return nil
}

func openBrowser(rawURL string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{rawURL}
	case "linux":
		command, args = "xdg-open", []string{rawURL}
	case "windows":
		command, args = "rundll32.exe", []string{"url.dll,FileProtocolHandler", rawURL}
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
	return exec.Command(command, args...).Start()
}
