import { describe, expect, test, vi } from 'vitest';

import type { RawProductEvent } from '@cyber/protocol';

import { RemoteEventSource, type RemoteRequest, type RemoteTransport } from './remote-event-source';

const handshake = (id: string) => ({
  id,
  type: 'handshake',
  handshake: {
    protocolVersion: 1,
    runtimeId: 'runtime-load',
    principal: 'operator@example.test',
    role: 'operator',
    capabilities: ['events', 'snapshot', 'commands'],
    source: {
      mode: 'remote',
      runtimeId: 'runtime-load',
      principal: 'operator@example.test',
      capabilities: ['events', 'snapshot', 'commands'],
    },
  },
});

class StormTransport implements RemoteTransport {
  readonly requests: RemoteRequest[] = [];
  failures = 2;

  async request(request: RemoteRequest): Promise<unknown> {
    this.requests.push(structuredClone(request));
    if (request.type === 'handshake') return handshake(request.id);
    if (request.type === 'events') {
      if (this.failures-- > 0) throw new Error('remote_transport_unavailable');
      return { id: request.id, type: 'events', events: [] };
    }
    if (request.type === 'close') return { id: request.id, type: 'closed' };
    throw new Error('unexpected_request');
  }
}

describe('runtime load gates', () => {
  test('bounds a reconnect storm and releases every subscription', async () => {
    vi.useFakeTimers();
    try {
      const transports = Array.from({ length: 20 }, () => new StormTransport());
      const sources = transports.map((transport) => new RemoteEventSource(transport, {
        pollIntervalMs: 60_000, reconnectInitialMs: 5, reconnectMaxMs: 10, reconnectAttempts: 3,
      }));
      await Promise.all(sources.map((source) => source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 })));
      const subscriptions = sources.map((source) => source.subscribe(0, () => undefined));
      await vi.advanceTimersByTimeAsync(5);
      await vi.advanceTimersByTimeAsync(10);
      const unsubscribe = await Promise.all(subscriptions);

      expect(transports.map((transport) => transport.requests.filter((request) => request.type === 'events').length))
        .toEqual(Array.from({ length: 20 }, () => 3));
      unsubscribe.forEach((stop) => stop());
      await Promise.all(sources.map((source) => source.close()));
      await vi.advanceTimersByTimeAsync(60_000);
      expect(transports.every((transport) => transport.requests.filter((request) => request.type === 'events').length === 3))
        .toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  test('delivers a large batch to a slow consumer in cursor order without read-ahead', async () => {
    vi.useFakeTimers();
    try {
      const events: RawProductEvent[] = Array.from({ length: 200 }, (_, index) => ({
        schemaVersion: 1,
        eventId: `load-${index + 1}`,
        taskId: 'task-load',
        cursor: index + 1,
        occurredAt: '2026-08-05T00:00:00.000Z',
        type: index === 0 ? 'task.created' : 'unknown.load.event',
        source: { runtimeId: 'runtime-load' },
        payload: index === 0 ? { title: 'Load task' } : { sample: index },
      }));
      const requests: RemoteRequest[] = [];
      const transport: RemoteTransport = {
        request: vi.fn(async (request: RemoteRequest) => {
          requests.push(structuredClone(request));
          if (request.type === 'handshake') return handshake(request.id);
          if (request.type === 'events') return { id: request.id, type: 'events', events: requests.filter((item) => item.type === 'events').length === 1 ? events : [] };
          if (request.type === 'close') return { id: request.id, type: 'closed' };
          throw new Error('unexpected_request');
        }),
      };
      const source = new RemoteEventSource(transport, { pollIntervalMs: 25 });
      await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
      const delivered: number[] = [];
      const unsubscribe = await source.subscribe(0, (event) => {
        for (let spin = 0; spin < 1_000; spin += 1) Math.imul(spin, spin);
        delivered.push(event.cursor as number);
      });

      expect(delivered).toEqual(Array.from({ length: 200 }, (_, index) => index + 1));
      expect(requests.filter((request) => request.type === 'events')).toHaveLength(1);
      await vi.advanceTimersByTimeAsync(25);
      expect(requests.filter((request) => request.type === 'events')).toHaveLength(2);
      expect(requests.at(-1)).toMatchObject({ type: 'events', afterCursor: 200 });
      unsubscribe();
      await source.close();
    } finally {
      vi.useRealTimers();
    }
  });
});
