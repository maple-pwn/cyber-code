package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type webRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip webRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type failingWebReader struct{}

func (failingWebReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingWebReader) Close() error             { return nil }

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

func TestDuckDuckGoSearchParsesBoundedResponse(t *testing.T) {
	if NewDuckDuckGoSearch(nil) == nil {
		t.Fatal("default search is nil")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("q") != "cyber-code" {
			t.Fatalf("query=%s", request.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"Heading":"Cyber","AbstractText":"Agent","AbstractURL":"https://example.com","RelatedTopics":[{"Text":"Docs","FirstURL":"https://example.com/docs"}]}`))
	}))
	defer server.Close()
	search := newDuckDuckGoSearch(server.Client(), server.URL, func(context.Context, *url.URL) error { return nil })
	results, err := search(context.Background(), "cyber-code", 2)
	if err != nil || len(results) != 2 || results[0].Title != "Cyber" || results[1].Title != "Docs" {
		t.Fatalf("results=%#v err=%v", results, err)
	}
}

func TestDuckDuckGoSearchRejectsInvalidResponses(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "bad", http.StatusBadGateway) },
		"json":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not-json")) },
		"large": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxWebResponseBytes+1)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			search := newDuckDuckGoSearch(server.Client(), server.URL, func(context.Context, *url.URL) error { return nil })
			if _, err := search(context.Background(), "q", 1); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	search := newDuckDuckGoSearch(http.DefaultClient, "://bad", func(context.Context, *url.URL) error { return nil })
	if _, err := search(context.Background(), "q", 1); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	search = newDuckDuckGoSearch(http.DefaultClient, "https://example.com", func(context.Context, *url.URL) error { return errors.New("blocked") })
	if _, err := search(context.Background(), "q", 1); err == nil || err.Error() != "blocked" {
		t.Fatalf("validation error=%v", err)
	}
}

func TestDuckDuckGoSearchReadAndRedirectLimits(t *testing.T) {
	readClient := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: failingWebReader{}}, nil
	})}
	search := newDuckDuckGoSearch(readClient, "https://example.com", func(context.Context, *url.URL) error { return nil })
	if _, err := search(context.Background(), "q", 1); err == nil || err.Error() != "read failed" {
		t.Fatalf("read error=%v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(w, request, "/redirect", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, `{"RelatedTopics":[{"Text":"one","FirstURL":"https://one"},{"Text":"two","FirstURL":"https://two"}]}`)
	}))
	defer server.Close()
	search = newDuckDuckGoSearch(server.Client(), server.URL, func(context.Context, *url.URL) error { return nil })
	results, err := search(context.Background(), "q", 1)
	if err != nil || len(results) != 1 || results[0].Title != "one" {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	search = newDuckDuckGoSearch(server.Client(), server.URL+"/redirect", func(context.Context, *url.URL) error { return nil })
	if _, err := search(context.Background(), "q", 1); err == nil || !strings.Contains(err.Error(), "redirect limit") {
		t.Fatalf("redirect error=%v", err)
	}
}
