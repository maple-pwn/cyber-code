package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchRejectsPrivateAndUnsupportedTargets(t *testing.T) {
	fetch := NewWebFetch(nil)
	for _, raw := range []string{"http://127.0.0.1/", "http://10.0.0.1/", "file:///etc/passwd"} {
		args, _ := json.Marshal(map[string]string{"url": raw})
		if _, err := fetch.Run(context.Background(), args); err == nil {
			t.Fatalf("target %q was accepted", raw)
		}
	}
}

func TestWebFetchLimitsBodyAndRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/body", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", maxWebResponseBytes+1)))
	}))
	defer server.Close()
	fetch := NewWebFetch(server.Client())
	args, _ := json.Marshal(map[string]string{"url": server.URL + "/redirect"})
	if _, err := fetch.Run(context.Background(), args); err == nil {
		t.Fatal("oversized response was accepted")
	}
}

func TestWebSearchUsesInjectedProvider(t *testing.T) {
	search := NewWebSearch(func(_ context.Context, query string, limit int) ([]WebResult, error) {
		if query != "cyber-code" || limit != 3 {
			t.Fatalf("query=%q limit=%d", query, limit)
		}
		return []WebResult{{Title: "Cyber", URL: "https://example.com", Snippet: "code"}}, nil
	})
	args, _ := json.Marshal(map[string]any{"query": "cyber-code", "limit": 3})
	result, err := search.Run(context.Background(), args)
	if err != nil || !strings.Contains(result.Content[0].Text, "Cyber") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWebSearchBoundsProviderResults(t *testing.T) {
	search := NewWebSearch(func(context.Context, string, int) ([]WebResult, error) {
		return []WebResult{{Title: strings.Repeat("x", maxWebResultFieldRunes+1)}, {Title: "extra"}}, nil
	})
	args := json.RawMessage(`{"query":"bounded","limit":1}`)
	result, err := search.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].Text
	if strings.Contains(text, "extra") || len([]rune(text)) > maxWebResultFieldRunes+80 {
		t.Fatalf("unbounded result: %d runes", len([]rune(text)))
	}
}
