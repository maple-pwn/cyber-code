import { describe, expect, test, vi } from 'vitest';

import {
  CyberAgentEventSource,
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
