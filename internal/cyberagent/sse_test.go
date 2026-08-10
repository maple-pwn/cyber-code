package cyberagent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSSEIteratorParsesMultilineDataCommentsAndReconnectCursor(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("after_sequence") != "3" || request.URL.Query().Get("after_event_id") != "event-3" {
			http.Error(response, "bad cursor", http.StatusBadRequest)
			return
		}
		if request.Header.Get("Authorization") != "Bearer stream-token" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = fmt.Fprint(response, ": keepalive\n")
		_, _ = fmt.Fprint(response, "id: event-4\n")
		_, _ = fmt.Fprint(response, "event: finding.created\n")
		_, _ = fmt.Fprint(response, `data: {"event_id":"event-4","task_id":"task-client",`+"\n")
		_, _ = fmt.Fprint(response, `data: "session_id":"session-client","sequence":4,"topic":"finding.created","payload":{"finding_id":"finding-1"},"emitted_by":"system","emitted_at":"2026-08-09T12:00:00Z","causation_id":"event-3"}`+"\n\n")
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{
		BaseURL:       server.URL,
		TokenProvider: func(context.Context) (string, error) { return "stream-token", nil },
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	stream, err := client.Events(context.Background(), "session-client", &EventCursor{
		SessionID: "session-client", Sequence: 3, EventID: "event-3",
	})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	defer stream.Close()
	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if event.EventID != "event-4" || event.Topic != "finding.created" || event.Sequence != 4 {
		t.Fatalf("event = %#v", event)
	}
}

func TestSSEIteratorRejectsMalformedAndOversizedEvents(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		body  string
		limit int64
		want  string
	}{
		{name: "malformed json", body: "data: {not-json}\n\n", limit: 1024, want: "decode"},
		{name: "oversized", body: "data: " + strings.Repeat("x", 128) + "\n\n", limit: 32, want: "exceeds"},
		{name: "missing cursor", body: `data: {"event_id":"event-1","task_id":"task-client","topic":"session.status","payload":{},"emitted_by":"system","emitted_at":"2026-08-09T12:00:00Z"}` + "\n\n", limit: 1024, want: "session cursor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(response, test.body)
			}))
			defer server.Close()
			client, err := NewClient(ClientOptions{BaseURL: server.URL, ResponseLimit: test.limit})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			stream, err := client.Events(context.Background(), "session-client", nil)
			if err != nil {
				t.Fatalf("Events: %v", err)
			}
			defer stream.Close()
			_, err = stream.Next(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Next error = %v, want %q", err, test.want)
			}
		})
	}
}
