import { afterEach, describe, expect, test, vi } from 'vitest';

import { initialProductState, type RawProductEvent } from '@cyber/protocol';

import {
  LocalEventSource,
  type LocalRequest,
  type LocalTransport,
} from './index';
import type { RuntimeCommandEnvelope, RuntimeHandshakeRequest } from './conformance';

const handshakeRequest: RuntimeHandshakeRequest = {
  supportedProtocolVersions: [1],
  afterCursor: 0,
};

const handshakeEnvelope = (id = 'desktop-startup') => ({
  id,
  type: 'handshake',
  handshake: {
    protocolVersion: 1,
    runtimeId: 'runtime-local',
    principal: 'local-user',
    role: 'owner',
    capabilities: ['events.replay', 'snapshot.read', 'command.send'],
    source: {
      mode: 'local',
      runtimeId: 'runtime-local',
      principal: 'local-user',
      capabilities: ['events.replay', 'snapshot.read', 'command.send'],
    },
  },
});

const event = (cursor: number): RawProductEvent => ({
  schemaVersion: 1,
  eventId: `event-${cursor}`,
  taskId: 'task-1',
  cursor,
  occurredAt: `2026-08-04T12:00:0${cursor}.000Z`,
  type: 'task.started',
  source: { runtimeId: 'runtime-local' },
  payload: { title: 'Local task' },
});

class TestTransport implements LocalTransport {
  readonly requests: LocalRequest[] = [];
  startCalls = 0;
  restartCalls = 0;
  stopCalls = 0;

  constructor(
    private readonly respond: (request: LocalRequest) => unknown | Promise<unknown> = (request) => {
      if (request.type === 'events') return { id: request.id, type: 'events', events: [] };
      throw new Error(`unexpected request: ${request.type}`);
    },
    private readonly startupResponse: unknown = handshakeEnvelope(),
  ) {}

  async start(): Promise<unknown> {
    this.startCalls += 1;
    return this.startupResponse;
  }

  async request(request: LocalRequest): Promise<unknown> {
    this.requests.push(structuredClone(request));
    return this.respond(request);
  }

  async restart(): Promise<unknown> {
    this.restartCalls += 1;
    return handshakeEnvelope('desktop-restart');
  }

  async stop(): Promise<unknown> {
    this.stopCalls += 1;
    return { stopped: true };
  }
}

afterEach(() => {
  vi.useRealTimers();
});

describe('LocalEventSource', () => {
  test('validates the startup handshake and exposes only a Local source', async () => {
    const transport = new TestTransport();
    const source = new LocalEventSource(transport);

    await expect(source.handshake(handshakeRequest)).resolves.toEqual(
      handshakeEnvelope().handshake,
    );
    expect(transport.startCalls).toBe(1);

    const remote = handshakeEnvelope();
    remote.handshake.source.mode = 'remote';
    const remoteTransport = new TestTransport(undefined, remote);
    await expect(new LocalEventSource(remoteTransport).handshake(handshakeRequest))
      .rejects.toThrow('local_source_required');
    expect(remoteTransport.stopCalls).toBe(1);

    const unexpected = handshakeEnvelope() as ReturnType<typeof handshakeEnvelope> & {
      handshake: { source: { unexpected?: boolean } };
    };
    unexpected.handshake.source.unexpected = true;
    const strictTransport = new TestTransport(undefined, unexpected);
    await expect(new LocalEventSource(strictTransport).handshake(handshakeRequest))
      .rejects.toThrow('invalid_handshake_response');
    expect(strictTransport.stopCalls).toBe(1);
  });

  test('requests replay strictly after the supplied cursor and validates every event', async () => {
    const transport = new TestTransport((request) => ({
      id: request.id,
      type: 'events',
      events: [event(3), event(4)],
    }));
    const source = new LocalEventSource(transport, { pollIntervalMs: 60_000 });
    await source.handshake(handshakeRequest);
    const received: RawProductEvent[] = [];

    const unsubscribe = await source.subscribe(2, (raw) => received.push(raw));
    unsubscribe();

    expect(transport.requests[0]).toMatchObject({ type: 'events', afterCursor: 2 });
    expect(received).toEqual([event(3), event(4)]);
  });

  test('rejects malformed events before delivering them', async () => {
    const transport = new TestTransport((request) => ({
      id: request.id,
      type: 'events',
      events: [{ ...event(1), schemaVersion: 2 }],
    }));
    const source = new LocalEventSource(transport);
    await source.handshake(handshakeRequest);
    const listener = vi.fn();

    await expect(source.subscribe(0, listener)).rejects.toThrow('unsupported_schema_version');
    expect(listener).not.toHaveBeenCalled();
  });

  test('validates snapshots and command receipts before returning them', async () => {
    const envelope: RuntimeCommandEnvelope = {
      idempotencyKey: 'cmd-1',
      command: { type: 'task.pause' },
    };
    const transport = new TestTransport((request) => {
      if (request.type === 'snapshot') {
        return {
          id: request.id,
          type: 'snapshot',
          snapshot: { cursor: 0, state: initialProductState() },
        };
      }
      if (request.type === 'command') {
        return {
          id: request.id,
          type: 'command',
          receipt: { idempotencyKey: 'cmd-1', status: 'accepted' },
        };
      }
      throw new Error(`unexpected request: ${request.type}`);
    });
    const source = new LocalEventSource(transport);
    await source.handshake(handshakeRequest);

    await expect(source.getSnapshot()).resolves.toEqual({ cursor: 0, state: initialProductState() });
    await expect(source.send(envelope)).resolves.toEqual({ idempotencyKey: 'cmd-1', status: 'accepted' });
  });

  test('rejects snapshot state that cannot be proven by its event log', async () => {
    const forgedState = {
      ...initialProductState(),
      task: { id: 'forged', title: 'Injected task', status: 'running' },
    };
    const transport = new TestTransport((request) => ({
      id: request.id,
      type: 'snapshot',
      snapshot: { cursor: 0, state: forgedState },
    }));
    const source = new LocalEventSource(transport);
    await source.handshake(handshakeRequest);

    await expect(source.getSnapshot()).rejects.toThrow('invalid_snapshot_state');
  });

  test('rejects command receipts with unknown nested fields', async () => {
    const envelope: RuntimeCommandEnvelope = {
      idempotencyKey: 'cmd-strict',
      command: { type: 'task.pause' },
    };
    const transport = new TestTransport((request) => ({
      id: request.id,
      type: 'command',
      receipt: { idempotencyKey: 'cmd-strict', status: 'accepted', unexpected: true },
    }));
    const source = new LocalEventSource(transport);
    await source.handshake(handshakeRequest);

    await expect(source.send(envelope)).rejects.toThrow('invalid_command_receipt');
  });

  test.each([
    [{ id: 'wrong-id', type: 'events', events: [] }, 'runtime_response_id_mismatch'],
    [{ id: 'REQUEST_ID', type: 'events', events: [], extra: true }, 'runtime_response_invalid'],
    [{ id: 'REQUEST_ID', type: 'mystery' }, 'runtime_response_invalid'],
  ])('rejects an untrusted response envelope %#', async (response, errorCode) => {
    const transport = new TestTransport((request) => JSON.parse(
      JSON.stringify(response).replace('REQUEST_ID', request.id),
    ));
    const source = new LocalEventSource(transport);
    await source.handshake(handshakeRequest);

    await expect(source.subscribe(0, vi.fn())).rejects.toThrow(errorCode);
  });

  test('surfaces runtime error envelopes and normalizes transport failures', async () => {
    const unauthorized = new LocalEventSource(new TestTransport((request) => ({
      id: request.id,
      type: 'error',
      errorCode: 'unauthorized',
    })));
    await unauthorized.handshake(handshakeRequest);
    await expect(unauthorized.getSnapshot()).rejects.toThrow('unauthorized');

    const unavailable = new LocalEventSource(new TestTransport(() => {
      throw new Error('host details must not cross the boundary');
    }));
    await unavailable.handshake(handshakeRequest);
    await expect(unavailable.getSnapshot()).rejects.toThrow('local_transport_unavailable');
  });

  test('unsubscribe stops later polling without closing or cancelling the runtime', async () => {
    vi.useFakeTimers();
    const transport = new TestTransport();
    const source = new LocalEventSource(transport, { pollIntervalMs: 25 });
    await source.handshake(handshakeRequest);

    const unsubscribe = await source.subscribe(0, vi.fn());
    expect(transport.requests).toHaveLength(1);
    unsubscribe();
    await vi.advanceTimersByTimeAsync(100);

    expect(transport.requests).toHaveLength(1);
    expect(transport.stopCalls).toBe(0);
    expect(transport.requests.some((request) => request.type === 'command')).toBe(false);
  });

  test('reports a transport failure that occurs after subscription is established', async () => {
    vi.useFakeTimers();
    let polls = 0;
    const transport = new TestTransport((request) => {
      if (request.type !== 'events') throw new Error('unexpected request');
      polls += 1;
      if (polls > 1) throw new Error('private host failure');
      return { id: request.id, type: 'events', events: [event(1)] };
    });
    const source = new LocalEventSource(transport, { pollIntervalMs: 25 });
    await source.handshake(handshakeRequest);
    const onError = vi.fn();

    await source.subscribe(0, vi.fn(), onError);
    await vi.advanceTimersByTimeAsync(25);

    expect(onError).toHaveBeenCalledWith(expect.objectContaining({ message: 'local_transport_unavailable' }));
  });

  test('uses an explicit runtime restart when reconnecting after transport failure', async () => {
    vi.useFakeTimers();
    let polls = 0;
    const transport = new TestTransport((request) => {
      if (request.type === 'events') {
        polls += 1;
        if (polls > 1) throw new Error('runtime crashed');
        return { id: request.id, type: 'events', events: [event(1)] };
      }
      throw new Error('stale process must not receive reconnect handshake');
    });
    const source = new LocalEventSource(transport, { pollIntervalMs: 25 });
    await source.handshake(handshakeRequest);
    await source.subscribe(0, vi.fn(), vi.fn());
    await vi.advanceTimersByTimeAsync(25);

    await expect(source.handshake({ ...handshakeRequest, afterCursor: 1 })).resolves
      .toMatchObject({ source: { mode: 'local' } });
    expect(transport.restartCalls).toBe(1);
    expect(transport.requests.filter((request) => request.type === 'handshake')).toHaveLength(0);
  });

  test('close releases the transport without sending task cancellation', async () => {
    const transport = new TestTransport((request) => {
      if (request.type === 'close') return { id: request.id, type: 'closed' };
      throw new Error(`unexpected request: ${request.type}`);
    });
    const source = new LocalEventSource(transport);
    await source.handshake(handshakeRequest);

    await source.close();

    expect(transport.requests.map((request) => request.type)).toEqual(['close']);
    expect(transport.stopCalls).toBe(1);
  });
});
