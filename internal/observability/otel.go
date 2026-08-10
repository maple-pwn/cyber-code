package observability

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type SpanStatus string

const (
	SpanUnset SpanStatus = "unset"
	SpanOK    SpanStatus = "ok"
	SpanError SpanStatus = "error"
)

type Span struct {
	Name       string            `json:"name"`
	RequestID  string            `json:"request_id,omitempty"`
	StartedAt  time.Time         `json:"started_at"`
	EndedAt    time.Time         `json:"ended_at"`
	Status     SpanStatus        `json:"status"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type Metric struct {
	Name       string            `json:"name"`
	Value      float64           `json:"value"`
	Unit       string            `json:"unit"`
	ObservedAt time.Time         `json:"observed_at"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type Batch struct {
	Spans   []Span   `json:"spans,omitempty"`
	Metrics []Metric `json:"metrics,omitempty"`
}

type Exporter interface {
	Export(context.Context, Batch) error
	Shutdown(context.Context) error
}

type Options struct {
	Exporter      Exporter
	SampleRate    float64
	QueueCapacity int
	ExportTimeout time.Duration
	Clock         func() time.Time
	Sample        func() float64
}

type Snapshot struct {
	Accepted       uint64 `json:"accepted"`
	Exported       uint64 `json:"exported"`
	ExportFailures uint64 `json:"export_failures"`
	Dropped        uint64 `json:"dropped"`
}

type Event struct {
	SpanName   string
	RequestID  string
	Status     SpanStatus
	Duration   time.Duration
	Attributes map[string]string
	Metrics    []Metric
}

type HTTPObservation struct {
	RequestID string
	Route     string
	Method    string
	Status    int
	Duration  time.Duration
}

type AuthorizationObservation struct {
	Operation string
	Allowed   bool
	Duration  time.Duration
}

type EventLagObservation struct {
	Kind string
	Lag  time.Duration
}

type TerminalObservation struct {
	Available bool
	Duration  time.Duration
}

type requestIDContextKey struct{}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requestID = safeRequestID(requestID)
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return safeRequestID(requestID)
}

type exportEnvelope struct {
	batch   Batch
	barrier chan struct{}
}

type Observer struct {
	exporter      Exporter
	sampleRate    float64
	exportTimeout time.Duration
	clock         func() time.Time
	sample        func() float64
	queue         chan exportEnvelope
	done          chan struct{}
	mu            sync.RWMutex
	closed        bool
	shutdownOnce  sync.Once
	shutdownErr   error
	accepted      atomic.Uint64
	exported      atomic.Uint64
	failures      atomic.Uint64
	dropped       atomic.Uint64
}

func New(options Options) (*Observer, error) {
	if options.SampleRate < 0 || options.SampleRate > 1 {
		return nil, fmt.Errorf("telemetry sample rate must be between 0 and 1")
	}
	if options.QueueCapacity <= 0 {
		options.QueueCapacity = 256
	}
	if options.ExportTimeout <= 0 {
		options.ExportTimeout = 2 * time.Second
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Sample == nil {
		options.Sample = randomSample
	}
	observer := &Observer{
		exporter: options.Exporter, sampleRate: options.SampleRate, exportTimeout: options.ExportTimeout,
		clock: options.Clock, sample: options.Sample, queue: make(chan exportEnvelope, options.QueueCapacity), done: make(chan struct{}),
	}
	go observer.run()
	return observer, nil
}

func (observer *Observer) Observe(ctx context.Context, event Event) {
	if observer == nil || observer.exporter == nil {
		return
	}
	now := observer.clock().UTC()
	attributes := safeAttributes(event.Attributes)
	requestID := safeRequestID(event.RequestID)
	if requestID == "" {
		requestID = RequestIDFromContext(ctx)
	}
	batch := Batch{Metrics: make([]Metric, 0, len(event.Metrics))}
	if observer.sampleRate >= 1 || (observer.sampleRate > 0 && observer.sample() < observer.sampleRate) {
		duration := event.Duration
		if duration < 0 {
			duration = 0
		}
		batch.Spans = []Span{{
			Name: safeSpanName(event.SpanName), RequestID: requestID, StartedAt: now.Add(-duration), EndedAt: now,
			Status: safeSpanStatus(event.Status), Attributes: attributes,
		}}
	}
	for _, metric := range event.Metrics {
		name := safeMetricName(metric.Name)
		if name == "" {
			continue
		}
		metric.Name = name
		metric.Unit = safeMetricUnit(metric.Unit)
		metric.ObservedAt = now
		metric.Attributes = mergeSafeAttributes(attributes, metric.Attributes)
		batch.Metrics = append(batch.Metrics, metric)
	}
	if len(batch.Spans) == 0 && len(batch.Metrics) == 0 {
		return
	}
	observer.accepted.Add(1)
	observer.mu.RLock()
	defer observer.mu.RUnlock()
	if observer.closed {
		observer.dropped.Add(1)
		return
	}
	select {
	case observer.queue <- exportEnvelope{batch: batch}:
	default:
		observer.dropped.Add(1)
	}
}

func (observer *Observer) ObserveHTTP(ctx context.Context, value HTTPObservation) {
	statusClass := fmt.Sprintf("%dxx", value.Status/100)
	status := SpanOK
	if value.Status >= 400 {
		status = SpanError
	}
	metrics := []Metric{
		{Name: "cyber.http.requests", Value: 1, Unit: "1"},
		{Name: "cyber.http.latency", Value: milliseconds(value.Duration), Unit: "ms"},
	}
	if value.Status >= 400 {
		metrics = append(metrics, Metric{Name: "cyber.http.errors", Value: 1, Unit: "1"})
	}
	observer.Observe(ctx, Event{
		SpanName: "runtime.http", RequestID: value.RequestID, Status: status, Duration: value.Duration,
		Attributes: map[string]string{"component": "runtime", "route": safeRoute(value.Route), "method": safeMethod(value.Method), "status_class": statusClass},
		Metrics:    metrics,
	})
}

func (observer *Observer) ObserveAuthorization(ctx context.Context, value AuthorizationObservation) {
	outcome, status := "denied", SpanError
	if value.Allowed {
		outcome, status = "allowed", SpanOK
	}
	observer.Observe(ctx, Event{
		SpanName: "authorization.decision", Status: status, Duration: value.Duration,
		Attributes: map[string]string{"component": "authorization", "operation": safeOperation(value.Operation), "outcome": outcome},
		Metrics: []Metric{
			{Name: "cyber.authorization.decisions", Value: 1, Unit: "1"},
			{Name: "cyber.authorization.latency", Value: milliseconds(value.Duration), Unit: "ms"},
		},
	})
}

func (observer *Observer) ObserveAuthorizationDecision(ctx context.Context, operation string, allowed bool, duration time.Duration) {
	observer.ObserveAuthorization(ctx, AuthorizationObservation{Operation: operation, Allowed: allowed, Duration: duration})
}

func (observer *Observer) ObserveEventLag(ctx context.Context, value EventLagObservation) {
	observer.Observe(ctx, Event{
		SpanName: "runtime.event_lag", Status: SpanOK,
		Attributes: map[string]string{"component": "runtime", "event_kind": safeToken(value.Kind)},
		Metrics:    []Metric{{Name: "cyber.event.lag", Value: milliseconds(value.Lag), Unit: "ms"}},
	})
}

func (observer *Observer) ObserveTerminal(ctx context.Context, value TerminalObservation) {
	availability, status := "unavailable", SpanError
	metricValue := float64(0)
	if value.Available {
		availability, status, metricValue = "available", SpanOK, 1
	}
	observer.Observe(ctx, Event{
		SpanName: "runtime.terminal", Status: status, Duration: value.Duration,
		Attributes: map[string]string{"component": "terminal", "terminal_state": availability},
		Metrics: []Metric{
			{Name: "cyber.terminal.availability", Value: metricValue, Unit: "1"},
			{Name: "cyber.terminal.latency", Value: milliseconds(value.Duration), Unit: "ms"},
		},
	})
}

func (observer *Observer) Flush(ctx context.Context) error {
	if observer == nil || observer.exporter == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	barrier := make(chan struct{})
	observer.mu.RLock()
	defer observer.mu.RUnlock()
	if observer.closed {
		return nil
	}
	select {
	case observer.queue <- exportEnvelope{barrier: barrier}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-barrier:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (observer *Observer) Snapshot() Snapshot {
	if observer == nil {
		return Snapshot{}
	}
	return Snapshot{
		Accepted: observer.accepted.Load(), Exported: observer.exported.Load(),
		ExportFailures: observer.failures.Load(), Dropped: observer.dropped.Load(),
	}
}

func (observer *Observer) Shutdown(ctx context.Context) error {
	if observer == nil {
		return nil
	}
	observer.shutdownOnce.Do(func() {
		if err := observer.Flush(ctx); err != nil {
			observer.shutdownErr = err
		}
		observer.mu.Lock()
		if !observer.closed {
			observer.closed = true
			close(observer.queue)
		}
		observer.mu.Unlock()
		<-observer.done
		if observer.exporter != nil {
			if err := observer.exporter.Shutdown(ctx); observer.shutdownErr == nil {
				observer.shutdownErr = err
			}
		}
	})
	return observer.shutdownErr
}

func (observer *Observer) run() {
	defer close(observer.done)
	for envelope := range observer.queue {
		if envelope.barrier != nil {
			close(envelope.barrier)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), observer.exportTimeout)
		err := observer.exporter.Export(ctx, envelope.batch)
		cancel()
		if err != nil {
			observer.failures.Add(1)
		} else {
			observer.exported.Add(1)
		}
	}
}

var allowedAttributeKeys = map[string]struct{}{
	"component": {}, "route": {}, "method": {}, "status_class": {}, "operation": {}, "outcome": {},
	"event_kind": {}, "terminal_state": {},
}

func safeAttributes(attributes map[string]string) map[string]string {
	result := make(map[string]string)
	for key, value := range attributes {
		if _, allowed := allowedAttributeKeys[key]; !allowed {
			continue
		}
		value = safeAttributeValue(value)
		if value != "" {
			result[key] = value
		}
	}
	return result
}

func mergeSafeAttributes(base, extra map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range safeAttributes(extra) {
		result[key] = value
	}
	return result
}

func safeAttributeValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

func safeRequestID(value string) string {
	value = safeAttributeValue(value)
	if len(value) > 128 {
		return ""
	}
	return value
}

func safeSpanName(value string) string {
	switch value {
	case "runtime.http", "authorization.decision", "runtime.event_lag", "runtime.terminal":
		return value
	default:
		return "cyber.operation"
	}
}

func safeSpanStatus(value SpanStatus) SpanStatus {
	if value == SpanOK || value == SpanError {
		return value
	}
	return SpanUnset
}

func safeMetricName(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 || !strings.HasPrefix(value, "cyber.") || strings.ContainsAny(value, "\x00\r\n ") {
		return ""
	}
	return value
}

func safeMetricUnit(value string) string {
	switch value {
	case "1", "ms", "s", "By":
		return value
	default:
		return "1"
	}
}

func safeRoute(value string) string {
	switch value {
	case "/runtime", "/admin", "/healthz", "/readyz", "/scim/v2/Users":
		return value
	default:
		return "other"
	}
}

func safeMethod(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return "OTHER"
	}
}

func safeOperation(value string) string {
	switch value {
	case "invite", "accept_invitation", "role", "revoke", "members", "invitations", "audit", "emergency_grant", "emergency_revoke":
		return value
	default:
		return "other"
	}
}

func safeToken(value string) string {
	value = safeAttributeValue(value)
	if value == "" {
		return "other"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return "other"
		}
	}
	return value
}

func milliseconds(duration time.Duration) float64 {
	if duration < 0 {
		return 0
	}
	return float64(duration) / float64(time.Millisecond)
}

func randomSample() float64 {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return 0
	}
	return float64(binary.BigEndian.Uint64(data[:])>>11) / (1 << 53)
}
