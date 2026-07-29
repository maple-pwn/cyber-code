package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthAuthorizationFlowValidatesStateAndExchangesCode(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.FormValue("grant_type") != "authorization_code" || request.FormValue("code") != "code-1" {
			t.Fatalf("form=%v", request.Form)
		}
		verifier := request.FormValue("code_verifier")
		if verifier == "" {
			t.Fatal("PKCE verifier is missing")
		}
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":60}`))
	}))
	defer tokenServer.Close()
	flow, err := NewOAuthAuthorizationFlow(OAuthAuthorizationOptions{AuthorizationURL: "https://auth.example.test/authorize", TokenURL: tokenServer.URL, ClientID: "client", HTTPClient: tokenServer.Client(), Now: func() time.Time { return time.Unix(100, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	login, err := flow.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	authorize, _ := url.Parse(login.AuthorizationURL)
	redirect := authorize.Query().Get("redirect_uri")
	state := authorize.Query().Get("state")
	if redirect == "" || state == "" {
		t.Fatalf("login=%#v", login)
	}
	if authorize.Query().Get("code_challenge_method") != "S256" || authorize.Query().Get("code_challenge") == "" {
		t.Fatalf("PKCE authorization parameters = %s", authorize.RawQuery)
	}
	challenge := authorize.Query().Get("code_challenge")
	verifierHash := sha256.Sum256([]byte(flow.codeVerifier))
	if challenge != base64.RawURLEncoding.EncodeToString(verifierHash[:]) {
		t.Fatal("PKCE challenge does not match verifier")
	}
	response, err := http.Get(redirect + "?code=code-1&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	credential, err := flow.Wait(context.Background())
	if err != nil || credential.AccessToken != "access" || credential.ExpiresAt != 160 {
		t.Fatalf("credential=%#v err=%v", credential, err)
	}
}

func TestOAuthBrowserOpeningIsExplicitAndInjectable(t *testing.T) {
	opened := 0
	opener := func(rawURL string) error {
		opened++
		if !strings.HasPrefix(rawURL, "https://auth.example.test/") {
			t.Fatalf("opened URL = %q", rawURL)
		}
		return nil
	}
	options := OAuthAuthorizationOptions{
		AuthorizationURL: "https://auth.example.test/authorize",
		TokenURL:         "https://token.example.test/token",
		ClientID:         "client",
		BrowserOpener:    opener,
	}
	flow, err := NewOAuthAuthorizationFlow(options)
	if err != nil {
		t.Fatal(err)
	}
	login, err := flow.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if login.BrowserOpened || opened != 0 {
		t.Fatalf("browser opened without explicit request: login=%#v calls=%d", login, opened)
	}
	cancel, stop := context.WithCancel(context.Background())
	stop()
	_, _ = flow.Wait(cancel)

	options.OpenBrowser = true
	flow, err = NewOAuthAuthorizationFlow(options)
	if err != nil {
		t.Fatal(err)
	}
	login, err = flow.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !login.BrowserOpened || opened != 1 {
		t.Fatalf("explicit browser open = %#v calls=%d", login, opened)
	}
	cancel, stop = context.WithCancel(context.Background())
	stop()
	_, _ = flow.Wait(cancel)
}

func TestOAuthTokenEndpointErrorsAreBoundedAndRedacted(t *testing.T) {
	secret := "top-secret-access-token"
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","access_token":"` + secret + `","error_description":"` + strings.Repeat("x", 4096) + `"}`))
	}))
	defer tokenServer.Close()
	flow, err := NewOAuthAuthorizationFlow(OAuthAuthorizationOptions{
		AuthorizationURL: "https://auth.example.test/authorize", TokenURL: tokenServer.URL,
		ClientID: "client", HTTPClient: tokenServer.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = flow.exchange(context.Background(), "code", "http://127.0.0.1/callback")
	if err == nil {
		t.Fatal("token endpoint rejection was accepted")
	}
	if strings.Contains(err.Error(), secret) || len(err.Error()) > 256 {
		t.Fatalf("token endpoint error leaked or was unbounded: %q", err)
	}
}

func TestOAuthRejectsWrongStateAndStopsServerOnCancellation(t *testing.T) {
	flow, err := NewOAuthAuthorizationFlow(OAuthAuthorizationOptions{
		AuthorizationURL: "https://auth.example.test/authorize",
		TokenURL:         "https://token.example.test/token",
		ClientID:         "client",
	})
	if err != nil {
		t.Fatal(err)
	}
	login, err := flow.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	authorize, _ := url.Parse(login.AuthorizationURL)
	redirect := authorize.Query().Get("redirect_uri")
	response, err := http.Get(redirect + "?code=code&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong state status = %d", response.StatusCode)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := flow.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait cancellation error = %v", err)
	}
	client := &http.Client{Timeout: 250 * time.Millisecond}
	if _, err := client.Get(redirect); err == nil {
		t.Fatal("OAuth callback server remained reachable after cancellation")
	}
}
