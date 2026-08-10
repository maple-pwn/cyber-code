package observability

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryExporter struct {
	mu      sync.Mutex
	batches []Batch
	err     error
}

func (exporter *memoryExporter) Export(_ context.Context, batch Batch) error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	exporter.batches = append(exporter.batches, batch)
	return exporter.err
}

func (exporter *memoryExporter) Shutdown(context.Context) error { return nil }

func (exporter *memoryExporter) snapshot() []Batch {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return append([]Batch(nil), exporter.batches...)
}

func TestObserverRedactsAttributesAndPropagatesRequestID(t *testing.T) {
	exporter := &memoryExporter{}
	observer, err := New(Options{Exporter: exporter, SampleRate: 1, QueueCapacity: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Shutdown(context.Background())
	ctx := WithRequestID(context.Background(), "request-123")
	observer.Observe(ctx, Event{
		SpanName: "runtime.http", Status: SpanOK, Duration: 25 * time.Millisecond,
		Attributes: map[string]string{
			"component": "runtime", "route": "/runtime", "method": "POST", "status_class": "2xx",
			"authorization": "Bearer secret", "prompt": "do not export me",
		},
		Metrics: []Metric{{Name: "cyber.http.requests", Value: 1, Unit: "1"}},
	})
	if err := observer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	batches := exporter.snapshot()
	if len(batches) != 1 || len(batches[0].Spans) != 1 || len(batches[0].Metrics) != 1 {
		t.Fatalf("batches=%#v", batches)
	}
	span := batches[0].Spans[0]
	if span.RequestID != "request-123" || span.Attributes["component"] != "runtime" || span.Attributes["route"] != "/runtime" {
		t.Fatalf("span=%#v", span)
	}
	if _, exists := span.Attributes["authorization"]; exists {
		t.Fatalf("authorization leaked: %#v", span.Attributes)
	}
	if _, exists := span.Attributes["prompt"]; exists {
		t.Fatalf("prompt leaked: %#v", span.Attributes)
	}
}

func TestObserverSamplingKeepsSLOMetrics(t *testing.T) {
	exporter := &memoryExporter{}
	observer, err := New(Options{Exporter: exporter, SampleRate: 0, QueueCapacity: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Shutdown(context.Background())
	observer.ObserveHTTP(context.Background(), HTTPObservation{RequestID: "request-1", Route: "/admin", Method: "POST", Status: 503, Duration: 40 * time.Millisecond})
	if err := observer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	batches := exporter.snapshot()
	if len(batches) != 1 || len(batches[0].Spans) != 0 || len(batches[0].Metrics) < 2 {
		t.Fatalf("sampled batches=%#v", batches)
	}
}

func TestObserverExporterFailureDoesNotFailCaller(t *testing.T) {
	exporter := &memoryExporter{err: errors.New("collector unavailable")}
	observer, err := New(Options{Exporter: exporter, SampleRate: 1, QueueCapacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	observer.ObserveAuthorization(context.Background(), AuthorizationObservation{Operation: "invite", Allowed: false, Duration: 10 * time.Millisecond})
	if err := observer.Flush(context.Background()); err != nil {
		t.Fatalf("exporter failure escaped flush: %v", err)
	}
	snapshot := observer.Snapshot()
	if snapshot.ExportFailures != 1 || snapshot.Exported != 0 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
