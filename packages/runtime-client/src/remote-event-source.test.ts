import { describe, expect, test, vi } from 'vitest';

import {
  initialProductState,
  project,
  validateEvent,
  type RawProductEvent,
} from '@cyber/protocol';

import {
  FetchRemoteTransport,
  RemoteEventSource,
  type RemoteRequest,
  type RemoteTransport,
} from './remote-event-source';
import { RuntimeClient } from './client';

const handshake = {
  id: 'remote-1',
  type: 'handshake',
  handshake: {
    protocolVersion: 1,
    runtimeId: 'runtime-remote',
    principal: 'operator@example.test',
    role: 'operator',
    capabilities: ['events', 'snapshot', 'commands'],
    source: {
      mode: 'remote',
      runtimeId: 'runtime-remote',
      principal: 'operator@example.test',
      capabilities: ['events', 'snapshot', 'commands'],
    },
  },
};

const event = (cursor: number, type = 'task.created'): RawProductEvent => ({
  schemaVersion: 1,
  eventId: `remote-${cursor}`,
  taskId: 'task-remote',
  cursor,
  occurredAt: `2026-08-05T00:00:0${cursor}.000Z`,
  type,
  source: { runtimeId: 'runtime-remote' },
  payload: { title: 'Remote audit' },
});

class TestTransport implements RemoteTransport {
  readonly requests: RemoteRequest[] = [];
  eventFailures = 0;

  async request(request: RemoteRequest): Promise<unknown> {
    this.requests.push(structuredClone(request));
    if (request.type === 'handshake') return { ...handshake, id: request.id };
    if (request.type === 'events') {
      if (this.eventFailures > 0) {
        this.eventFailures -= 1;
        throw new Error('remote_transport_unavailable');
      }
      return { id: request.id, type: 'events', events: [] };
    }
    if (request.type === 'close') return { id: request.id, type: 'closed' };
    throw new Error('unexpected_request');
  }
}

describe('RemoteEventSource', () => {
  test('requires HTTPS and keeps the access token out of URL and request body', async () => {
    expect(() => new FetchRemoteTransport('http://runtime.example.test/v1/runtime', () => 'secret'))
      .toThrow('remote_tls_required');

    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(handshake), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    }));
    const transport = new FetchRemoteTransport(
      'https://runtime.example.test/v1/runtime',
      () => 'secret',
      { fetch },
    );

    await transport.request({
      id: 'remote-1',
      type: 'handshake',
      handshake: { supportedProtocolVersions: [1], afterCursor: 0 },
    });

    expect(fetch).toHaveBeenCalledWith('https://runtime.example.test/v1/runtime', expect.objectContaining({
      credentials: 'include',
      headers: expect.objectContaining({ Authorization: 'Bearer secret' }),
    }));
    const request = fetch.mock.calls[0]?.[1] as RequestInit;
    expect(String(request.body)).not.toContain('secret');
    expect(fetch.mock.calls[0]?.[0]).not.toContain('secret');
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });

  test('accepts only remote source metadata', async () => {
    const source = new RemoteEventSource(new TestTransport());
    await expect(source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 }))
      .resolves.toMatchObject({ source: { mode: 'remote' } });

    const local = new TestTransport();
    local.request = async (request) => ({
      ...handshake,
      id: request.id,
      handshake: { ...handshake.handshake, source: { ...handshake.handshake.source, mode: 'local' } },
    });
    await expect(new RemoteEventSource(local).handshake({ supportedProtocolVersions: [1], afterCursor: 0 }))
      .rejects.toThrow('remote_source_required');
  });

  test('clears prior connection authority before re-handshake', async () => {
    const transport = new TestTransport();
    const source = new RemoteEventSource(transport);
    await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
    transport.request = async () => { throw new Error('unauthorized'); };

    await expect(source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 }))
      .rejects.toThrow('unauthorized');
    await expect(source.subscribe(0, () => undefined)).rejects.toThrow('remote_runtime_not_connected');
  });

  test('enforces the response limit in UTF-8 bytes', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response('"界"', { status: 200 }));
    const transport = new FetchRemoteTransport(
      'https://runtime.example.test/v1/runtime',
      () => 'secret',
      { fetch, maxResponseBytes: 4 },
    );

    await expect(transport.request({ id: 'remote-1', type: 'health' }))
      .rejects.toThrow('remote_response_too_large');
  });

  test.each([
    [401, 'unauthorized'],
    [426, 'incompatible'],
  ])('maps an empty HTTP %i before decoding its body', async (status, code) => {
    const fetch = vi.fn().mockResolvedValue(new Response('', { status }));
    const transport = new FetchRemoteTransport(
      'https://runtime.example.test/v1/runtime',
      () => 'secret',
      { fetch },
    );

    await expect(transport.request({ id: 'remote-1', type: 'health' })).rejects.toThrow(code);
  });

  test('blocks redirects and aborts a request at the configured timeout', async () => {
    vi.useFakeTimers();
    try {
      const fetch = vi.fn((_url: string | URL | Request, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
        expect(init?.redirect).toBe('error');
        init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
      }));
      const transport = new FetchRemoteTransport(
        'https://runtime.example.test/v1/runtime',
        () => 'secret',
        { fetch, requestTimeoutMs: 25 },
      );
      const pending = transport.request({ id: 'remote-1', type: 'health' });
      const rejection = expect(pending).rejects.toThrow('remote_transport_unavailable');
      await vi.advanceTimersByTimeAsync(25);
      await rejection;
    } finally {
      vi.useRealTimers();
    }
  });

  test('uses capped reconnect backoff and resumes from the last subscription cursor', async () => {
    vi.useFakeTimers();
    try {
      const transport = new TestTransport();
      transport.eventFailures = 2;
      const source = new RemoteEventSource(transport, {
        pollIntervalMs: 60_000,
        reconnectInitialMs: 10,
        reconnectMaxMs: 15,
        reconnectAttempts: 3,
      });
      await source.handshake({ supportedProtocolVersions: [1], afterCursor: 7 });

      const subscribed = source.subscribe(7, () => undefined);
      await vi.advanceTimersByTimeAsync(10);
      await vi.advanceTimersByTimeAsync(15);
      const unsubscribe = await subscribed;

      expect(transport.requests.filter((request) => request.type === 'events'))
        .toEqual(Array.from({ length: 3 }, () => expect.objectContaining({ afterCursor: 7 })));
      unsubscribe();
      await source.close();
    } finally {
      vi.useRealTimers();
    }
  });

  test('cancels reconnect backoff without sending another credentialed request', async () => {
    vi.useFakeTimers();
    try {
      const transport = new TestTransport();
      transport.eventFailures = 2;
      const source = new RemoteEventSource(transport, {
        reconnectInitialMs: 50,
        reconnectMaxMs: 50,
        reconnectAttempts: 3,
      });
      await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
      const subscribing = source.subscribe(0, () => undefined);
      await source.close();
      await vi.advanceTimersByTimeAsync(50);
      await subscribing;

      expect(transport.requests.filter((request) => request.type === 'events')).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });

  test('maps terminal remote partition through RuntimeClient without discarding trusted state', async () => {
    const transport = new TestTransport();
    transport.eventFailures = 1;
    const source = new RemoteEventSource(transport, { reconnectAttempts: 1 });
    const client = new RuntimeClient(source);

    await client.connect();

    expect(client.getView().connection).toEqual({
      status: 'degraded',
      lastTrustedCursor: 0,
      errorCode: 'remote_transport_unavailable',
    });
  });

  test('resumes after the latest delivered cursor following a transient partition', async () => {
    vi.useFakeTimers();
    try {
      const transport = new TestTransport();
      let eventCalls = 0;
      transport.request = async (request) => {
        transport.requests.push(structuredClone(request));
        if (request.type === 'handshake') return { ...handshake, id: request.id };
        if (request.type === 'events') {
          eventCalls += 1;
          if (eventCalls === 1) return { id: request.id, type: 'events', events: [event(8)] };
          if (eventCalls === 2) throw new Error('remote_transport_unavailable');
          return { id: request.id, type: 'events', events: [] };
        }
        if (request.type === 'close') return { id: request.id, type: 'closed' };
        throw new Error('unexpected_request');
      };
      const source = new RemoteEventSource(transport, {
        pollIntervalMs: 10, reconnectInitialMs: 5, reconnectMaxMs: 5, reconnectAttempts: 2,
      });
      await source.handshake({ supportedProtocolVersions: [1], afterCursor: 7 });
      const unsubscribe = await source.subscribe(7, () => undefined);
      await vi.advanceTimersByTimeAsync(15);

      expect(transport.requests.filter((request) => request.type === 'events').map((request) => request.afterCursor))
        .toEqual([7, 8, 8]);
      unsubscribe();
      await source.close();
    } finally {
      vi.useRealTimers();
    }
  });

  test('recovers a remote event gap through a trusted snapshot', async () => {
    const first = event(1);
    const second = event(2, 'task.started');
    const projected = project(initialProductState(), validateEvent(first));
    if (projected.kind === 'resync-required') throw new Error('invalid_test_fixture');
    const transport = new TestTransport();
    transport.request = async (request) => {
      if (request.type === 'handshake') return { ...handshake, id: request.id };
      if (request.type === 'events') {
        return { id: request.id, type: 'events', events: request.afterCursor === 0 ? [second] : [second] };
      }
      if (request.type === 'snapshot') {
        return { id: request.id, type: 'snapshot', snapshot: { cursor: 1, state: projected.state } };
      }
      if (request.type === 'close') return { id: request.id, type: 'closed' };
      throw new Error('unexpected_request');
    };
    const client = new RuntimeClient(new RemoteEventSource(transport, { pollIntervalMs: 60_000 }));

    await client.connect();
    await vi.waitFor(() => expect(client.getView().connection.status).toBe('healthy'));

    expect(client.getView().connection.lastTrustedCursor).toBe(2);
    expect(client.getView().product.task?.status).toBe('running');
  });

  test('disables writes when an established remote source loses capability', async () => {
    vi.useFakeTimers();
    try {
      const transport = new TestTransport();
      let eventCalls = 0;
      transport.request = async (request) => {
        if (request.type === 'handshake') return { ...handshake, id: request.id };
        if (request.type === 'events' && eventCalls++ === 0) return { id: request.id, type: 'events', events: [] };
        if (request.type === 'events') throw new Error('capability_lost');
        throw new Error('unexpected_request');
      };
      const client = new RuntimeClient(new RemoteEventSource(transport, { pollIntervalMs: 10 }));
      await client.connect();
      await vi.advanceTimersByTimeAsync(10);

      expect(client.getView().connection).toMatchObject({ status: 'degraded', errorCode: 'capability_lost' });
      await expect(client.dispatch({ type: 'task.pause' })).rejects.toThrow('writes_disabled:degraded');
    } finally {
      vi.useRealTimers();
    }
  });

  test.each([
    [401, 'unauthorized'],
    [426, 'incompatible'],
  ])('maps remote HTTP %i into RuntimeClient state %s', async (status, expected) => {
    const fetch = vi.fn().mockResolvedValue(new Response('', { status }));
    const source = new RemoteEventSource(new FetchRemoteTransport(
      'https://runtime.example.test/v1/runtime', () => 'secret', { fetch },
    ));
    const client = new RuntimeClient(source);

    await client.connect();

    expect(client.getView().connection.status).toBe(expected);
  });
});
