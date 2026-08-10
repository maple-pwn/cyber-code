package runtimeapi

import (
	"context"
	"sync/atomic"
	"time"

	"cyber-code/internal/observability"
)

type TeamObservation struct {
	Context   context.Context
	RequestID string
	Path      string
	Method    string
	Status    int
	Duration  time.Duration
}

type telemetryTeamObserver struct{ observer *observability.Observer }

func NewTelemetryTeamObserver(observer *observability.Observer) TeamObserver {
	if observer == nil {
		return nil
	}
	return telemetryTeamObserver{observer: observer}
}

func (observer telemetryTeamObserver) Observe(value TeamObservation) {
	observer.observer.ObserveHTTP(value.Context, observability.HTTPObservation{
		RequestID: value.RequestID,
		Route:     value.Path,
		Method:    value.Method,
		Status:    value.Status,
		Duration:  value.Duration,
	})
}

type TeamObserver interface{ Observe(TeamObservation) }

type TeamObserverFunc func(TeamObservation)

func (observer TeamObserverFunc) Observe(value TeamObservation) {
	if observer != nil {
		observer(value)
	}
}

type TeamMetricSnapshot struct {
	Requests       uint64
	Failures       uint64
	Runtime        uint64
	Administration uint64
	Health         uint64
	NotFound       uint64
	Duration       time.Duration
}

type TeamMetrics struct {
	requests, failures, runtime, administration, health, notFound, durationNanos atomic.Uint64
}

func (metrics *TeamMetrics) Observe(value TeamObservation) {
	if metrics == nil {
		return
	}
	metrics.requests.Add(1)
	if value.Status >= 400 {
		metrics.failures.Add(1)
	}
	switch value.Path {
	case "/runtime":
		metrics.runtime.Add(1)
	case "/admin":
		metrics.administration.Add(1)
	case "/healthz", "/readyz":
		metrics.health.Add(1)
	default:
		metrics.notFound.Add(1)
	}
	if value.Duration > 0 {
		metrics.durationNanos.Add(uint64(value.Duration))
	}
}

func (metrics *TeamMetrics) Snapshot() TeamMetricSnapshot {
	if metrics == nil {
		return TeamMetricSnapshot{}
	}
	return TeamMetricSnapshot{
		Requests: metrics.requests.Load(), Failures: metrics.failures.Load(), Runtime: metrics.runtime.Load(),
		Administration: metrics.administration.Load(), Health: metrics.health.Load(), NotFound: metrics.notFound.Load(),
		Duration: time.Duration(metrics.durationNanos.Load()),
	}
}
