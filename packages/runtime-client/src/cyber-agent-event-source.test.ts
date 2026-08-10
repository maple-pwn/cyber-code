import { describe, expect, test, vi } from 'vitest';

import {
  CyberAgentEventSource,
  FetchCyberAgentTransport,
  type CyberAgentRequest,
  type CyberAgentTransport,
} from './cyber-agent-event-source';

const snapshot = {
  schema_version: 1,
  session_id: 'session-web',
  task_id: 'task-web',
  revision: 1,
  status: 'active',
  event_cursor: { session_id: 'session-web', sequence: 2, event_id: 'source-2' },
};

class FixtureTransport implements CyberAgentTransport {
  readonly requests: CyberAgentRequest[] = [];
  closed = false;

  async request(request: CyberAgentRequest): Promise<unknown> {
    this.requests.push(request);
    if (request.method === 'GET' && request.path === '/v1/capabilities') {
      return { product: 'cyber-agent', runtime_version: '0.1.0', protocol_version: 1, capabilities: ['session.events.v1'] };
    }
    if (request.method === 'POST' && request.path === '/v1/sessions') return snapshot;
    if (request.method === 'POST' && request.path.startsWith('/v1/inputs?')) return {
      schema_version: 1, upload_id: 'input_0123456789abcdef0123456789abcdef', filename: 'api.yaml',
      media_type: 'application/yaml', sha256: 'a'.repeat(64), size: 14, source_location: 'inputs/aa/api.yaml',
      parser_status: 'parsed', parent_upload_id: null, parser_error: null,
    };
    if (request.method === 'GET' && request.path === '/v1/sessions/session-web') return snapshot;
    throw new Error(`unexpected_request:${request.method}:${request.path}`);
  }

  async *events(sessionId: string, afterSequence: number, signal: AbortSignal): AsyncIterable<unknown> {
    expect(sessionId).toBe('session-web');
    expect(afterSequence).toBe(0);
    if (signal.aborted) return;
    yield sourceEvent(1, 'session.created', {
      schema_version: 1, session_id: 'session-web', task_id: 'task-web', revision: 1, status: 'active',
    });
    yield sourceEvent(2, 'runtime.extension', { value: 'preserved' });
  }

  async close(): Promise<void> { this.closed = true; }
}

describe('CyberAgentEventSource', () => {
  test('uploads task inputs before creating an input-manifest session', async () => {
    const transport = new FixtureTransport();
    const source = new CyberAgentEventSource(transport);
    await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
    await source.send({ idempotencyKey: 'create-input', command: {
      type: 'task.create', objective: 'Assess API', runtimeId: 'cyber-agent-remote',
      inputs: [{ filename: 'api.yaml', mediaType: 'application/yaml', bytes: new TextEncoder().encode('openapi: 3.0.0') }],
    } });
    expect(transport.requests[1]).toMatchObject({ method: 'POST', contentType: 'application/yaml' });
    expect(ArrayBuffer.isView(transport.requests[1]?.body)).toBe(true);
    expect(transport.requests[2]?.body).toEqual(expect.objectContaining({
      kind: 'input_manifest', input_ids: ['input_0123456789abcdef0123456789abcdef'], content: 'Assess API',
    }));
  });

  test('negotiates, binds a session, projects SSE replay, and preserves unknown events', async () => {
    const transport = new FixtureTransport();
    const source = new CyberAgentEventSource(transport);
    const handshake = await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
    expect(handshake).toMatchObject({
      protocolVersion: 1,
      runtimeId: 'cyber-agent-remote',
      principal: 'cyber-agent',
      source: { mode: 'remote', runtimeId: 'cyber-agent-remote' },
    });

    await expect(source.send({
      idempotencyKey: 'create-web-1',
      command: { type: 'task.create', objective: 'Assess the authorized lab', runtimeId: handshake.runtimeId },
    })).resolves.toEqual({ idempotencyKey: 'create-web-1', status: 'accepted' });

    const events: unknown[] = [];
    const complete = new Promise<void>((resolve) => {
      void source.subscribe(0, (event) => {
        events.push(event);
        if (events.length === 2) resolve();
      });
    });
    await complete;
    expect(events).toEqual([
      expect.objectContaining({ cursor: 1, type: 'task.created', taskId: 'task-web' }),
      expect.objectContaining({ cursor: 2, type: 'cyber-agent.runtime.extension', taskId: 'task-web' }),
    ]);
    expect(await source.getSnapshot()).toMatchObject({ cursor: 2, state: { committedCursor: 2 } });
    expect(transport.requests).toContainEqual(expect.objectContaining({
      method: 'POST', path: '/v1/sessions', idempotencyKey: 'create-web-1',
    }));

    await source.close();
    expect(transport.closed).toBe(true);
  });

  test('projects cyber-agent evidence and report products into the trusted product state', async () => {
    const productTransport = new FixtureTransport();
    const fixtureRequest = productTransport.request.bind(productTransport);
    productTransport.request = async (request) => {
      if (request.method === 'GET' && request.path === '/v1/sessions/session-web') {
        return { ...snapshot, event_cursor: { session_id: 'session-web', sequence: 10, event_id: 'source-10' } };
      }
      return fixtureRequest(request);
    };
    productTransport.events = async function* (_sessionId, _afterSequence, signal) {
      if (signal.aborted) return;
      yield sourceEvent(1, 'session.created', { schema_version: 1, session_id: 'session-web', task_id: 'task-web', revision: 1, status: 'active' });
      yield sourceEvent(2, 'evidence.available', {
        evidence_id: 'evidence-1', kind: 'http', summary: 'HTTP response', data: { status_code: 200 },
      });
      yield sourceEvent(3, 'asset.node.committed', {
        node_id: 'asset-target-1', kind: 'target', label: '127.0.0.1', status: 'active',
        attributes: { host: '127.0.0.1' }, evidence_refs: ['evidence-1'],
      });
      yield sourceEvent(4, 'asset.node.committed', {
        node_id: 'asset-service-1', kind: 'service', label: 'HTTP 127.0.0.1:18080', status: 'active',
        attributes: { host: '127.0.0.1', port: 18080, scheme: 'http' }, evidence_refs: ['evidence-1'],
      });
      yield sourceEvent(5, 'asset.edge.committed', {
        edge_id: 'asset-edge-1', kind: 'exposes', source_id: 'asset-target-1', target_id: 'asset-service-1',
        directed: true, evidence_refs: ['evidence-1'],
      });
      yield sourceEvent(6, 'finding.created', {
        finding_id: 'finding-1', title: 'Exposed endpoint', severity: 'medium', evidence_refs: ['evidence-1'],
      });
      yield sourceEvent(7, 'finding.verifying', { finding_id: 'finding-1' });
      yield sourceEvent(8, 'finding.confirmed', { finding_id: 'finding-1' });
      yield sourceEvent(9, 'report.drafted', {
        report_id: 'report-1', narrative: 'Verified assessment report', finding_ids: ['finding-1'], evidence_refs: ['evidence-1'],
      });
      yield sourceEvent(10, 'report.frozen', { report_id: 'report-1' });
    };
    const source = new CyberAgentEventSource(productTransport);
    await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
    await source.send({ idempotencyKey: 'create-products', command: { type: 'task.create', objective: 'Assess', runtimeId: 'cyber-agent-remote' } });

    await new Promise<void>((resolve) => {
      void source.subscribe(0, (event) => { if (event.cursor === 10) resolve(); });
    });
    const projected = await source.getSnapshot();

    expect(projected.state.evidence['evidence-1']).toMatchObject({ summary: 'HTTP response', data: { status_code: 200 } });
    expect(projected.state.assetNodes).toMatchObject({
      'asset-target-1': { kind: 'target', label: '127.0.0.1', provenance: { kind: 'evidence', evidenceIds: ['evidence-1'] } },
      'asset-service-1': { kind: 'service', label: 'HTTP 127.0.0.1:18080' },
    });
    expect(projected.state.assetEdges['asset-edge-1']).toMatchObject({
      kind: 'exposes', sourceId: 'asset-target-1', targetId: 'asset-service-1', directed: true,
    });
    expect(projected.state.findings['finding-1']).toMatchObject({ status: 'confirmed', evidenceIds: ['evidence-1'] });
    expect(projected.state.report).toMatchObject({
      id: 'report-1', status: 'frozen', narrative: 'Verified assessment report',
      findings: [{ finding: { id: 'finding-1', status: 'confirmed' }, evidence: [{ id: 'evidence-1' }], included: true }],
    });
  });

  test('rejects unauthorized and incompatible handshakes without becoming connected', async () => {
    const unauthorized: CyberAgentTransport = {
      request: vi.fn().mockRejectedValue(new Error('unauthorized')),
      events: async function* () { yield undefined; },
    };
    await expect(new CyberAgentEventSource(unauthorized).handshake({ supportedProtocolVersions: [1], afterCursor: 0 }))
      .rejects.toThrow('unauthorized');

    const incompatible: CyberAgentTransport = {
      request: vi.fn().mockResolvedValue({ product: 'cyber-agent', runtime_version: '0.1.0', protocol_version: 2, capabilities: ['session.events.v1'] }),
      events: async function* () { yield undefined; },
    };
    await expect(new CyberAgentEventSource(incompatible).handshake({ supportedProtocolVersions: [1], afterCursor: 0 }))
      .rejects.toThrow('incompatible');
  });

  test('replaces the active binding for a second task and sends complete interaction responses', async () => {
    let creates = 0;
    let interactionBody: unknown;
    const boundSnapshot = (id: number, pending = false) => ({
      session_id: `session-${id}`, task_id: `task-${id}`,
      event_cursor: { session_id: `session-${id}`, sequence: 0, event_id: '' },
      pending_interaction: pending ? { interaction_id: 'interaction-2', session_id: `session-${id}` } : null,
    });
    const transport: CyberAgentTransport = {
      request: vi.fn(async (request: CyberAgentRequest) => {
        if (request.path === '/v1/capabilities') return { product: 'cyber-agent', runtime_version: '0.1.0', protocol_version: 1, capabilities: ['session.events.v1'] };
        if (request.path === '/v1/sessions') { creates += 1; return boundSnapshot(creates, creates === 2); }
        if (request.method === 'GET' && request.path === '/v1/sessions/session-2') return boundSnapshot(2, true);
        if (request.path === '/v1/sessions/session-2/interactions') { interactionBody = request.body; return boundSnapshot(2); }
        throw new Error(`unexpected_request:${request.path}`);
      }),
      events: async function* () { yield undefined; },
    };
    const source = new CyberAgentEventSource(transport);
    await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
    await source.send({ idempotencyKey: 'create-1', command: { type: 'task.create', objective: 'First', runtimeId: 'cyber-agent-remote' } });
    await expect(source.send({ idempotencyKey: 'create-2', command: { type: 'task.create', objective: 'Second', runtimeId: 'cyber-agent-remote' } }))
      .resolves.toMatchObject({ status: 'accepted' });
    await source.send({ idempotencyKey: 'scope-2', command: { type: 'scope.confirm', scopeId: 'scope-2' } });
    expect(interactionBody).toMatchObject({
      interaction_id: 'interaction-2', session_id: 'session-2', approved: true,
      responded_at: expect.stringMatching(/Z$/),
    });
  });

  test('rebuilds a trusted product snapshot when the live stream is behind authority', async () => {
    let streams = 0;
    const transport: CyberAgentTransport = {
      request: vi.fn(async (request: CyberAgentRequest) => {
        if (request.path === '/v1/capabilities') return { product: 'cyber-agent', runtime_version: '0.1.0', protocol_version: 1, capabilities: ['session.events.v1'] };
        if (request.path === '/v1/sessions') return { ...snapshot, event_cursor: { session_id: 'session-web', sequence: 0, event_id: '' } };
        if (request.path === '/v1/sessions/session-web') return snapshot;
        throw new Error(`unexpected_request:${request.path}`);
      }),
      events: async function* (_sessionId, _after, signal) {
        streams += 1;
        if (signal.aborted) return;
        yield sourceEvent(1, 'session.created', { schema_version: 1, session_id: 'session-web', task_id: 'task-web', revision: 1, status: 'active' });
        if (streams > 1) yield sourceEvent(2, 'runtime.extension', { replayed: true });
      },
    };
    const source = new CyberAgentEventSource(transport);
    await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
    await source.send({ idempotencyKey: 'create-resync', command: { type: 'task.create', objective: 'Assess', runtimeId: 'cyber-agent-remote' } });
    await new Promise<void>((resolve) => { void source.subscribe(0, () => resolve()); });
    await expect(source.getSnapshot()).resolves.toMatchObject({ cursor: 2, state: { committedCursor: 2 } });
    expect(streams).toBe(2);
    await source.close();
  });
});

describe('FetchCyberAgentTransport', () => {
  test('binds the host fetch receiver for WebKit-compatible desktop requests', async () => {
    const host = globalThis;
    const receiverAwareFetch = vi.fn(function (this: unknown) {
      if (this !== host) throw new TypeError('fetch receiver must be Window');
      return Promise.resolve(new Response('{"ok":true}', { status: 200 }));
    });
    vi.stubGlobal('fetch', receiverAwareFetch);
    try {
      const transport = new FetchCyberAgentTransport(
        'http://127.0.0.1:43127',
        () => 'desktop-token',
        { allowInsecureLoopback: true },
      );

      await expect(transport.request({ method: 'GET', path: '/v1/capabilities' }))
        .resolves.toEqual({ ok: true });
    } finally {
      vi.unstubAllGlobals();
    }
  });

  test('sends bearer and idempotency metadata and parses multiline SSE events', async () => {
    const fetch = vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      if (url.includes('/events')) {
        return new Response('event: message\nid: source-1\ndata: {"value":\ndata: 1}\n\n', { status: 200, headers: { 'content-type': 'text/event-stream' } });
      }
      expect(new Headers(init?.headers).get('authorization')).toBe('Bearer web-token');
      expect(new Headers(init?.headers).get('idempotency-key')).toBe('mutation-1');
      return new Response('{"ok":true}', { status: 200, headers: { 'content-type': 'application/json' } });
    });
    const transport = new FetchCyberAgentTransport('https://agent.example.test', () => 'web-token', { fetch: fetch as typeof globalThis.fetch });
    await expect(transport.request({ method: 'POST', path: '/v1/sessions', body: { task: true }, idempotencyKey: 'mutation-1' }))
      .resolves.toEqual({ ok: true });
    const controller = new AbortController();
    const events: unknown[] = [];
    for await (const event of transport.events('session-web', 7, controller.signal)) events.push(event);
    expect(events).toEqual([{ value: 1 }]);
    expect(fetch.mock.calls[1]?.[0].toString()).toContain('after_sequence=7');
  });

  test('polls finite JSON event batches when streaming fetch is unavailable', async () => {
    let requests = 0;
    const fetch = vi.fn(async (input: string | URL | Request) => {
      const url = String(input);
      if (!url.includes('/event-batch')) throw new Error(`unexpected_url:${url}`);
      requests += 1;
      return new Response(JSON.stringify({
        events: requests === 1 ? [sourceEvent(8, 'scope.proposed', {
          schema_version: 1,
          scope_id: 'scope-web',
          targets: ['http://127.0.0.1:18080'],
        })] : [],
        has_more: false,
      }), { status: 200, headers: { 'content-type': 'application/json' } });
    });
    const transport = new FetchCyberAgentTransport(
      'http://127.0.0.1:43127',
      () => 'desktop-token',
      { allowInsecureLoopback: true, fetch: fetch as typeof globalThis.fetch, eventMode: 'poll', pollIntervalMs: 1 },
    );
    const controller = new AbortController();
    const iterator = transport.events('session-web', 7, controller.signal)[Symbol.asyncIterator]();

    await expect(iterator.next()).resolves.toEqual({
      done: false,
      value: expect.objectContaining({ sequence: 8, topic: 'scope.proposed' }),
    });
    const pending = iterator.next();
    await vi.waitFor(() => expect(fetch.mock.calls.length).toBeGreaterThanOrEqual(2));
    controller.abort();
    await expect(pending).resolves.toEqual({ done: true, value: undefined });

    expect(fetch.mock.calls[0]?.[0].toString()).toContain('/v1/sessions/session-web/event-batch?after_sequence=7');
    expect(fetch.mock.calls[1]?.[0].toString()).toContain('after_sequence=8&after_event_id=source-8');
  });

  test('requires HTTPS unless a trusted host explicitly enables loopback HTTP', () => {
    expect(() => new FetchCyberAgentTransport('http://127.0.0.1:8080', () => 'token')).toThrow('cyber_agent_tls_required');
    expect(() => new FetchCyberAgentTransport('http://127.0.0.1:8080', () => 'token', { allowInsecureLoopback: true })).not.toThrow();
    expect(() => new FetchCyberAgentTransport('http://example.test', () => 'token', { allowInsecureLoopback: true })).toThrow('cyber_agent_tls_required');
  });

  test('preserves known conflict diagnostics from the cyber-agent response', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ detail: 'idempotency key reused with different request' }),
      { status: 409, headers: { 'content-type': 'application/json' } },
    ));
    const transport = new FetchCyberAgentTransport(
      'https://agent.example.test',
      () => 'web-token',
      { fetch: fetch as typeof globalThis.fetch },
    );

    await expect(transport.request({
      method: 'POST',
      path: '/v1/sessions',
      body: { task: true },
      idempotencyKey: 'mutation-conflict',
    })).rejects.toThrow('cyber_agent_idempotency_conflict');
  });
});

function sourceEvent(sequence: number, topic: string, payload: Record<string, unknown>): unknown {
  return {
    event_id: `source-${sequence}`,
    task_id: 'task-web',
    session_id: 'session-web',
    sequence,
    topic,
    payload,
    emitted_by: 'system',
    emitted_at: '2026-08-09T12:00:00Z',
    causation_id: null,
  };
}
