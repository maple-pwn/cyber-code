package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestGoogleAuthManagerRefreshesADCOnce(t *testing.T) {
	credentialsPath := filepath.Join(t.TempDir(), "adc.json")
	if err := os.WriteFile(credentialsPath, []byte(`{"type":"authorized_user","client_id":"client","client_secret":"secret","refresh_token":"refresh"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("client_id") != "client" || request.Form.Get("refresh_token") != "refresh" {
			t.Errorf("unexpected token form: %v", request.Form)
		}
		_, _ = io.WriteString(response, `{"access_token":"vertex-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()
	manager := NewGoogleAuthManager(GoogleAuthOptions{HTTPClient: server.Client(), TokenURL: server.URL, Now: func() time.Time { return time.Date(2026, 7, 28, 4, 0, 0, 0, time.UTC) }})
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			token, err := manager.GetAccessToken(context.Background())
			if err != nil || token != "vertex-token" {
				t.Errorf("token = %q, %v", token, err)
			}
		}()
	}
	group.Wait()
	mu.Lock()
	gotRequests := requests
	mu.Unlock()
	if gotRequests != 1 {
		t.Fatalf("token requests = %d, want 1", gotRequests)
	}
}
