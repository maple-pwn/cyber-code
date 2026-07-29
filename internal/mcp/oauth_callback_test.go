package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestOAuthAuthorizationFlowValidatesStateAndExchangesCode(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.FormValue("grant_type") != "authorization_code" || request.FormValue("code") != "code-1" {
			t.Fatalf("form=%v", request.Form)
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
