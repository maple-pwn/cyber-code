# Telemetry and SLO Operations

cyber-code telemetry is opt-in. Local-only deployments do not start an exporter and do not make network requests for telemetry. Production deployments may inject an OpenTelemetry-compatible exporter through the runtime observer boundary.

## Exported Signals

| Signal | Metrics | Default objective |
| --- | --- | --- |
| Runtime HTTP | `cyber.http.requests`, `cyber.http.errors`, `cyber.http.latency` | 99.9% successful requests over 30 days; p95 latency below 500 ms excluding provider streaming time |
| Authorization | `cyber.authorization.decisions`, `cyber.authorization.latency` | p99 decision latency below 100 ms; denied decisions are outcomes, not service errors |
| Event delivery | `cyber.event.lag` | p95 committed-event delivery lag below 2 s |
| Terminal service | `cyber.terminal.availability`, `cyber.terminal.latency` | 99.5% successful terminal operations over 30 days; p95 operation latency below 1 s |

Spans use the bounded names `runtime.http`, `authorization.decision`, `runtime.event_lag`, and `runtime.terminal`. Sampling applies to spans only; SLO metrics remain enabled when the span sample rate is zero.

## Alert Thresholds

- Page when the runtime HTTP error ratio exceeds 5% for 10 minutes or the 30-day availability budget is exhausted.
- Warn when runtime HTTP p95 latency exceeds 500 ms for 15 minutes; page above 2 s for 10 minutes.
- Warn when authorization p99 latency exceeds 100 ms for 15 minutes; page above 500 ms for 10 minutes.
- Warn when event delivery p95 lag exceeds 2 s for 10 minutes; page above 10 s for 5 minutes.
- Warn when terminal availability falls below 99.5% for 30 minutes; page below 95% for 10 minutes.
- Alert on a sustained increase in observer `Dropped` or `ExportFailures`; these indicate telemetry loss, not user-request failure.

Tune thresholds only after collecting at least one representative workload window. Cloud-provider latency and model streaming latency should be measured separately from runtime handler latency.

## Cardinality and Redaction

Exported attributes are allowlisted and bounded. The runtime exports only component, normalized route, HTTP method, status class, authorization operation/outcome, event kind, and terminal availability state. Request IDs are bounded to 128 bytes and are used only for correlation.

Do not add access tokens, authorization headers, prompts, command content, file contents, policy signatures, plugin payloads, or emergency-access reasons as attributes. Unknown attribute keys and unsafe values are discarded before enqueueing.

## Failure Behavior

Telemetry export is asynchronous, bounded, and best effort. A full queue increments `Dropped`. Export timeouts or collector failures increment `ExportFailures`. Neither condition changes the HTTP response, authorization outcome, terminal result, or local-only operation.

During graceful shutdown, call `Flush` and then `Shutdown` with a bounded context. A flush barrier waits for all previously accepted batches, while exporter errors remain observable through `Snapshot` rather than escaping into user requests.

## Deployment Checklist

1. Leave the exporter unset for local-only installations.
2. Configure an exporter, sample rate, bounded queue capacity, and export timeout for managed deployments.
3. Verify the collector rejects unauthenticated ingestion and uses TLS outside loopback development.
4. Send a health request with `X-Request-ID` and confirm the same ID appears in the exported span.
5. Simulate an unavailable collector and confirm user requests still succeed while `ExportFailures` increases.
6. Create dashboards and alerts for all four SLO groups before enabling paging.

## Queue and Runtime drain

Durable task workers must be registered with the Runtime service before it begins serving work. During shutdown:

1. stop accepting new remote mutations;
2. call Runtime `Drain` with a bounded deployment shutdown context;
3. allow the active queue item to finish while leaving pending work durable;
4. flush and shut down telemetry after the worker drain completes;
5. close terminal managers and reconcile expired queue leases on the next startup;
6. run terminal-item cleanup only with an explicit retention interval.

If the drain deadline expires, terminate the instance and rely on lease reconciliation rather than marking an unknown task as completed. Queue payloads and handler errors are bounded; do not put credentials, raw authorization headers, or unredacted prompts in queue metadata.
