package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type credentialRefreshFunc func(context.Context, string, Credential) (Credential, error)

func (refresh credentialRefreshFunc) Refresh(ctx context.Context, server string, value Credential) (Credential, error) {
	return refresh(ctx, server, value)
}

func TestCredentialStorePersistsAndNeverIncludesSecretInErrors(t *testing.T) {
	store, err := NewCredentialStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "server", Credential{AccessToken: "token-secret", RefreshToken: "refresh-secret", ExpiresAt: 123}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewCredentialStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	credential, ok, err := reopened.Get(context.Background(), "server")
	if err != nil || !ok || credential.AccessToken != "token-secret" {
		t.Fatalf("credential=%#v ok=%v err=%v", credential, ok, err)
	}
	if _, _, err := reopened.Get(context.Background(), "missing"); err != nil && strings.Contains(err.Error(), "token-secret") {
		t.Fatal("secret leaked in error")
	}
	if info, err := os.Stat(store.path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode = %v, err=%v", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(store.directory); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("credential directory mode = %v, err=%v", info.Mode().Perm(), err)
	}
	if err := reopened.Delete(context.Background(), "server"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.Get(context.Background(), "server"); err != nil || ok {
		t.Fatalf("deleted credential = ok=%v err=%v", ok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reopened.Put(ctx, "server", Credential{AccessToken: "secret"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled put error = %v", err)
	}
}

func TestCredentialStoreRefreshesExpiredAccessToken(t *testing.T) {
	store, err := NewCredentialStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "server", Credential{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: 10}); err != nil {
		t.Fatal(err)
	}
	value, err := store.Resolve(context.Background(), "server", time.Unix(20, 0), credentialRefreshFunc(func(_ context.Context, server string, previous Credential) (Credential, error) {
		if server != "server" || previous.RefreshToken != "refresh" {
			t.Fatalf("server=%s value=%#v", server, previous)
		}
		return Credential{AccessToken: "new", RefreshToken: "refresh", TokenType: "Bearer", ExpiresAt: 100}, nil
	}))
	if err != nil || value.AccessToken != "new" {
		t.Fatalf("value=%#v err=%v", value, err)
	}
	persisted, ok, err := store.Get(context.Background(), "server")
	if err != nil || !ok || persisted.AccessToken != "new" {
		t.Fatalf("persisted=%#v ok=%v err=%v", persisted, ok, err)
	}
}

func TestOAuthRefresherExchangesRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.FormValue("grant_type") != "refresh_token" || request.FormValue("refresh_token") != "refresh" {
			t.Fatalf("request method=%s form=%v", request.Method, request.Form)
		}
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"next","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()
	refresher := OAuthRefresher{TokenURL: server.URL, ClientID: "client", ClientSecret: "secret", HTTPClient: server.Client(), Now: func() time.Time { return time.Unix(100, 0) }}
	value, err := refresher.Refresh(context.Background(), "server", Credential{RefreshToken: "refresh"})
	if err != nil || value.AccessToken != "new" || value.RefreshToken != "next" || value.ExpiresAt != 3700 {
		t.Fatalf("value=%#v err=%v", value, err)
	}
}
