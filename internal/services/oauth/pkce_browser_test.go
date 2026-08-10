package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/constants"
)

func TestBrowserLoginBuildsS256AuthorizationURLAndBindsOIDCNonce(t *testing.T) {
	var tokenRequest url.Values
	var nonce string
	tokenServer := newIPv4Server(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		tokenRequest, _ = url.ParseQuery(string(body))
		idToken := unsignedOIDCToken(nonce)
		_, _ = io.WriteString(w, `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":60,"scope":"user:inference","id_token":"`+idToken+`"}`)
	}))
	defer tokenServer.Close()

	client := NewOAuthClientWithOptions(OAuthClientOptions{HTTPClient: tokenServer.Client(), Store: &memoryStore{}, Config: constants.OAuthConfig{TokenURL: tokenServer.URL + "/token", ClientID: "client"}})
	login, err := client.BeginBrowserLogin(context.Background(), BrowserLoginOptions{AuthorizationURL: "https://issuer.example/authorize", TokenURL: tokenServer.URL + "/token", ClientID: "client", Scopes: []string{"openid", "user:inference"}, Now: func() time.Time { return time.Unix(100, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	authorize, err := url.Parse(login.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := authorize.Query()
	nonce = query.Get("nonce")
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || nonce == "" {
		t.Fatalf("authorization query = %v", query)
	}
	verifierHash := sha256.Sum256([]byte(login.codeVerifier))
	if query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(verifierHash[:]) {
		t.Fatal("S256 challenge does not match verifier")
	}
	if query.Get("redirect_uri") != login.RedirectURI {
		t.Fatalf("redirect_uri = %q, want %q", query.Get("redirect_uri"), login.RedirectURI)
	}
	response, err := http.Get(login.RedirectURI + "?code=code-1&state=" + url.QueryEscape(query.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d", response.StatusCode)
	}
	tokens, err := login.Wait(context.Background())
	if err != nil || tokens.AccessToken != "access" {
		t.Fatalf("tokens=%#v err=%v", tokens, err)
	}
	if tokenRequest.Get("code_verifier") != login.codeVerifier {
		t.Fatal("code verifier was not sent")
	}
}

func TestBrowserLoginRejectsReplayAndRedirectMismatch(t *testing.T) {
	var nonce string
	tokenServer := newIPv4Server(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"access","id_token":"`+unsignedOIDCToken(nonce)+`"}`)
	}))
	defer tokenServer.Close()
	client := NewOAuthClientWithOptions(OAuthClientOptions{HTTPClient: tokenServer.Client(), Store: &memoryStore{}, Config: constants.OAuthConfig{TokenURL: tokenServer.URL, ClientID: "client"}})
	login, err := client.BeginBrowserLogin(context.Background(), BrowserLoginOptions{AuthorizationURL: "https://issuer.example/authorize", TokenURL: tokenServer.URL, ClientID: "client"})
	if err != nil {
		t.Fatal(err)
	}
	authorize, _ := url.Parse(login.AuthorizationURL)
	state := authorize.Query().Get("state")
	nonce = authorize.Query().Get("nonce")
	request := httptest.NewRequest(http.MethodGet, login.RedirectURI+"?code=code&state="+url.QueryEscape(state), nil)
	request.Host = "127.0.0.1:1"
	response := httptest.NewRecorder()
	login.callbackHandler(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "redirect") {
		t.Fatalf("mismatch response = %d %q", response.Code, response.Body.String())
	}
	good := httptest.NewRecorder()
	goodRequest := httptest.NewRequest(http.MethodGet, login.RedirectURI+"?code=code&state="+url.QueryEscape(state), nil)
	login.callbackHandler(good, goodRequest)
	if good.Code != http.StatusOK {
		t.Fatalf("valid callback after mismatch = %d %q", good.Code, good.Body.String())
	}
	replay := httptest.NewRecorder()
	login.callbackHandler(replay, goodRequest)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("callback replay status = %d", replay.Code)
	}
}

func TestBrowserLoginRejectsNonLoopbackOrNonEphemeralCallback(t *testing.T) {
	client := NewOAuthClientWithOptions(OAuthClientOptions{Config: constants.OAuthConfig{TokenURL: "https://issuer.example/token", ClientID: "client"}})
	for _, address := range []string{"0.0.0.0:0", "127.0.0.1:43123", "[::1]:0", "localhost:0"} {
		_, err := client.BeginBrowserLogin(context.Background(), BrowserLoginOptions{AuthorizationURL: "https://issuer.example/authorize", CallbackAddress: address})
		if err == nil {
			t.Fatalf("callback address %q was accepted", address)
		}
	}
}

func TestBrowserLoginStateAndNonceExpireAndAreSingleUse(t *testing.T) {
	now := time.Unix(100, 0)
	transaction, err := newBrowserTransaction(now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.consume(transaction.state, transaction.nonce, now); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if err := transaction.consume(transaction.state, transaction.nonce, now); !errors.Is(err, errBrowserReplay) {
		t.Fatalf("replay error = %v, want %v", err, errBrowserReplay)
	}
	expired, err := newBrowserTransaction(now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := expired.consume(expired.state, expired.nonce, now.Add(2*time.Second)); !errors.Is(err, errBrowserExpired) {
		t.Fatalf("expired error = %v, want %v", err, errBrowserExpired)
	}
}

func TestBrowserLoginDoesNotReleaseStateAfterNonceFailure(t *testing.T) {
	requests := 0
	tokenServer := newIPv4Server(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = io.WriteString(w, `{"access_token":"access","id_token":"`+unsignedOIDCToken("wrong-nonce")+`"}`)
	}))
	defer tokenServer.Close()
	client := NewOAuthClientWithOptions(OAuthClientOptions{HTTPClient: tokenServer.Client(), Store: &memoryStore{}, Config: constants.OAuthConfig{TokenURL: tokenServer.URL, ClientID: "client"}})
	login, err := client.BeginBrowserLogin(context.Background(), BrowserLoginOptions{AuthorizationURL: "https://issuer.example/authorize", TokenURL: tokenServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	authorize, _ := url.Parse(login.AuthorizationURL)
	callback := login.RedirectURI + "?code=code&state=" + url.QueryEscape(authorize.Query().Get("state"))
	first := httptest.NewRecorder()
	login.callbackHandler(first, httptest.NewRequest(http.MethodGet, callback, nil))
	if first.Code != http.StatusBadGateway {
		t.Fatalf("nonce mismatch status = %d", first.Code)
	}
	replay := httptest.NewRecorder()
	login.callbackHandler(replay, httptest.NewRequest(http.MethodGet, callback, nil))
	if replay.Code != http.StatusBadRequest || requests != 1 {
		t.Fatalf("replay status=%d token requests=%d", replay.Code, requests)
	}
}

func TestBrowserLoginBoundsCallbackAndCancelsCodeExchange(t *testing.T) {
	exchangeStarted := make(chan struct{})
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(exchangeStarted)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	client := NewOAuthClientWithOptions(OAuthClientOptions{HTTPClient: httpClient, Store: &memoryStore{}, Config: constants.OAuthConfig{TokenURL: "https://issuer.example/token", ClientID: "client"}})
	login, err := client.BeginBrowserLogin(ctx, BrowserLoginOptions{AuthorizationURL: "https://issuer.example/authorize", TokenURL: "https://issuer.example/token"})
	if err != nil {
		t.Fatal(err)
	}
	authorize, _ := url.Parse(login.AuthorizationURL)
	callback := login.RedirectURI + "?code=code&state=" + url.QueryEscape(authorize.Query().Get("state"))
	methodResponse := httptest.NewRecorder()
	login.callbackHandler(methodResponse, httptest.NewRequest(http.MethodPost, callback, strings.NewReader(strings.Repeat("x", 9000))))
	if methodResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST callback status = %d", methodResponse.Code)
	}
	tooLarge := httptest.NewRecorder()
	login.callbackHandler(tooLarge, httptest.NewRequest(http.MethodGet, callback+"&padding="+strings.Repeat("x", 9000), nil))
	if tooLarge.Code != http.StatusRequestURITooLong {
		t.Fatalf("large callback status = %d", tooLarge.Code)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		response := httptest.NewRecorder()
		login.callbackHandler(response, httptest.NewRequest(http.MethodGet, callback, nil))
	}()
	select {
	case <-exchangeStarted:
	case <-time.After(time.Second):
		t.Fatal("code exchange did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("code exchange did not inherit login context cancellation")
	}
}

func TestLogoutClearsLocalCredentialsWhenRevocationFails(t *testing.T) {
	secret := "access-secret-value"
	store := &memoryStore{}
	client := NewOAuthClientWithOptions(OAuthClientOptions{Store: store, HTTPClient: http.DefaultClient, Config: constants.OAuthConfig{ClientID: "client"}})
	if err := client.SaveOAuthTokens(&OAuthTokens{AccessToken: secret, RefreshToken: "refresh-secret"}); err != nil {
		t.Fatal(err)
	}
	revocationRequests := 0
	revocation := newIPv4Server(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		revocationRequests++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_token","access_token":"`+secret+`"}`)
	}))
	defer revocation.Close()
	err := client.Logout(context.Background(), revocation.URL)
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "refresh-secret") {
		t.Fatalf("logout error = %v", err)
	}
	if revocationRequests != 2 {
		t.Fatalf("revocation requests = %d, want both refresh and access tokens attempted", revocationRequests)
	}
	client.ClearOAuthTokenCache()
	tokens, loadErr := client.LoadOAuthTokens()
	if loadErr != nil || (tokens != nil && (tokens.AccessToken != "" || tokens.RefreshToken != "")) {
		t.Fatalf("local credentials were not cleared: %#v %v", tokens, loadErr)
	}
}

func newIPv4Server(handler http.Handler) *httptest.Server {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func unsignedOIDCToken(nonce string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]string{"nonce": nonce})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}
