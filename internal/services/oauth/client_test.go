package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cyber-code/internal/constants"
)

func TestOAuthClientRefreshesOnceWithInjectedStore(t *testing.T) {
	store := &memoryStore{}
	expired := OAuthTokens{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Hour).UnixMilli(), Scopes: []string{constants.ClaudeAIInferenceScope}}
	encoded, err := json.Marshal(expired)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), encoded); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		if request.URL.Path == "/token" {
			_, _ = io.WriteString(response, `{"access_token":"new","refresh_token":"refresh","expires_in":3600,"scope":"user:inference"}`)
			return
		}
		if request.URL.Path == "/api/oauth/profile" {
			_, _ = io.WriteString(response, `{"organization":{"organization_type":"claude_pro","rate_limit_tier":"standard"}}`)
			return
		}
		http.NotFound(response, request)
	}))
	defer server.Close()

	client := NewOAuthClientWithOptions(OAuthClientOptions{Store: store, HTTPClient: server.Client(), Config: constants.OAuthConfig{TokenURL: server.URL + "/token", BaseAPIURL: server.URL}})
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if changed, err := client.CheckAndRefreshOAuthTokenIfNeeded(context.Background(), false); err != nil || !changed {
				t.Errorf("refresh = %t, %v", changed, err)
			}
		}()
	}
	group.Wait()
	mu.Lock()
	gotRequests := requests
	mu.Unlock()
	if gotRequests != 2 {
		t.Fatalf("HTTP requests = %d, want one token and one profile request", gotRequests)
	}
	tokens, err := client.LoadOAuthTokens()
	if err != nil || tokens == nil || tokens.AccessToken != "new" {
		t.Fatalf("stored tokens = %#v, %v", tokens, err)
	}
}

func TestOAuthRefreshWaiterHonorsCancellation(t *testing.T) {
	store := &memoryStore{}
	expired := OAuthTokens{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Hour).UnixMilli(), Scopes: []string{constants.ClaudeAIInferenceScope}}
	encoded, err := json.Marshal(expired)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), encoded); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			close(started)
			<-release
			_, _ = io.WriteString(response, `{"access_token":"new","refresh_token":"refresh","expires_in":3600,"scope":"user:inference"}`)
			return
		}
		_, _ = io.WriteString(response, `{}`)
	}))
	defer server.Close()
	client := NewOAuthClientWithOptions(OAuthClientOptions{Store: store, HTTPClient: server.Client(), Config: constants.OAuthConfig{TokenURL: server.URL + "/token", BaseAPIURL: server.URL}})
	leaderDone := make(chan error, 1)
	go func() {
		_, err := client.CheckAndRefreshOAuthTokenIfNeeded(context.Background(), false)
		leaderDone <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	waiterDone := make(chan error, 1)
	go func() { _, err := client.CheckAndRefreshOAuthTokenIfNeeded(ctx, false); waiterDone <- err }()
	select {
	case err := <-waiterDone:
		if err != context.Canceled {
			t.Fatalf("waiter error = %v, want context.Canceled", err)
		}
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-leaderDone
		t.Fatal("OAuth refresh waiter ignored cancellation")
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader refresh: %v", err)
	}
}

type memoryStore struct {
	mu   sync.Mutex
	data []byte
}

func (store *memoryStore) Load(context.Context) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.data == nil {
		return nil, errMemoryNotFound{}
	}
	return append([]byte(nil), store.data...), nil
}
func (store *memoryStore) Save(_ context.Context, value []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.data = append([]byte(nil), value...)
	return nil
}

type errMemoryNotFound struct{}

func (errMemoryNotFound) Error() string { return "not found" }
