import { describe, expect, test, vi } from 'vitest';

import {
  initialProductState,
  project,
  validateEvent,
  type ProductState,
  type RawProductEvent,
} from '@cyber/protocol';

import {
  RuntimeClient,
  type EventSource,
  type RuntimeSnapshot,
  type Unsubscribe,
} from './index';
import type {
  RuntimeCommandEnvelope,
  RuntimeCommandReceipt,
  RuntimeHandshakeRequest,
  RuntimeHandshakeResponse,
} from './conformance';

const event = (cursor: number, overrides: Partial<RawProductEvent> = {}): RawProductEvent => ({
  schemaVersion: 1,
  eventId: `event-${cursor}`,
  taskId: 'task-1',
  cursor,
  occurredAt: `2026-08-03T12:00:0${cursor}.000Z`,
  type: 'task.started',
  source: { runtimeId: 'runtime-1' },
  payload: { title: 'Authorized lab' },
  ...overrides,
});

const stateWith = (...events: RawProductEvent[]): ProductState => events.reduce((state, raw) => {
  const result = project(state, validateEvent(raw));
  if (result.kind === 'resync-required') throw new Error('invalid_test_fixture');
  return result.state;
}, initialProductState());

class FakeEventSource implements EventSource {
  readonly sent: unknown[] = [];
  readonly subscribeCalls: number[] = [];
  readonly handshakeCalls: RuntimeHandshakeRequest[] = [];
  closeCalls = 0;
  snapshotCalls = 0;
  subscribeError?: Error;
  snapshotError?: Error;
  rejectionCode?: string;
  snapshot: RuntimeSnapshot;
  private listener?: (event: RawProductEvent) => void;

  constructor(readonly events: RawProductEvent[] = [], snapshot?: RuntimeSnapshot) {
    this.snapshot = snapshot ?? { cursor: 0, state: initialProductState() };
  }

  async handshake(request: RuntimeHandshakeRequest): Promise<RuntimeHandshakeResponse> {
    this.handshakeCalls.push(request);
    return {
      protocolVersion: 1,
      runtimeId: 'runtime-1',
      principal: 'test-operator',
      role: 'operator',
      capabilities: ['events.replay', 'snapshot.read', 'command.send'],
      source: {
        mode: 'local',
        runtimeId: 'runtime-1',
        principal: 'test-operator',
        capabilities: ['events.replay', 'snapshot.read', 'command.send'],
      },
    };
  }

  async subscribe(afterCursor: number, onEvent: (event: RawProductEvent) => void): Promise<Unsubscribe> {
    this.subscribeCalls.push(afterCursor);
    if (this.subscribeError) throw this.subscribeError;
    this.listener = onEvent;
    for (const raw of this.events) if ((raw.cursor as number) > afterCursor) onEvent(raw);
    return () => {
      if (this.listener === onEvent) this.listener = undefined;
    };
  }

  async getSnapshot(): Promise<RuntimeSnapshot> {
    this.snapshotCalls += 1;
    if (this.snapshotError) throw this.snapshotError;
    return this.snapshot;
  }

  async send(envelope: RuntimeCommandEnvelope): Promise<RuntimeCommandReceipt> {
    this.sent.push(envelope);
    if (this.rejectionCode) {
      return {
        idempotencyKey: envelope.idempotencyKey,
        status: 'rejected',
        errorCode: this.rejectionCode,
      };
    }
    return { idempotencyKey: envelope.idempotencyKey, status: 'accepted' };
  }

  async close(): Promise<void> {
    this.closeCalls += 1;
    this.listener = undefined;
  }

  emit(raw: RawProductEvent): void {
    this.listener?.(raw);
  }
}

describe('RuntimeClient', () => {
  test('handshakes before subscribing and exposes trusted source metadata', async () => {
    const source = new FakeEventSource();
    const client = new RuntimeClient(source);

    await client.connect();

    expect(source.handshakeCalls).toEqual([{
      supportedProtocolVersions: [1],
      afterCursor: 0,
    }]);
    expect(client.getView().source).toEqual({
      mode: 'local',
      runtimeId: 'runtime-1',
      principal: 'test-operator',
      capabilities: ['events.replay', 'snapshot.read', 'command.send'],
    });
    expect(source.subscribeCalls).toEqual([0]);
  });

  test('projects source events and ignores an identical duplicate replay', async () => {
    const taskStartedEvent = event(1);
    const source = new FakeEventSource([taskStartedEvent, structuredClone(taskStartedEvent)]);
    const client = new RuntimeClient(source);

    await client.connect();

    expect(client.getView().connection).toEqual({ status: 'healthy', lastTrustedCursor: 1 });
    expect('connection' in client.getView().product).toBe(false);
    expect(client.getView().product.task?.status).toBe('running');
    expect(client.getView().product.timeline).toHaveLength(1);
  });

  test('resyncs from a trusted snapshot and reconnects after its cursor', async () => {
    const snapshotEvent = event(1, { type: 'task.created' });
    const gapEvent = event(2);
    const snapshotState = stateWith(snapshotEvent);
    const source = new FakeEventSource([gapEvent], { cursor: 1, state: snapshotState });
    const client = new RuntimeClient(source);

    await client.connect();
    await vi.waitFor(() => expect(client.getView().connection.status).toBe('healthy'));

    expect(source.snapshotCalls).toBe(1);
    expect(source.subscribeCalls).toEqual([0, 1]);
    expect(client.getView().connection.lastTrustedCursor).toBe(2);
    expect(client.getView().product.task?.status).toBe('running');
  });

  test('keeps writes disabled while a resync snapshot is pending', async () => {
    let resolveSnapshot!: (snapshot: RuntimeSnapshot) => void;
    const source = new FakeEventSource();
    source.getSnapshot = vi.fn(() => new Promise<RuntimeSnapshot>((resolve) => {
      resolveSnapshot = resolve;
    }));
    const client = new RuntimeClient(source);

    await client.connect();
    source.emit(event(2));
    await vi.waitFor(() => expect(client.getView().connection.status).toBe('resyncing'));
    await expect(client.dispatch({ type: 'approval.respond', challengeId: 'a-1', decision: 'allow_once' }))
      .rejects.toThrow('writes_disabled:resyncing');

    resolveSnapshot({ cursor: 1, state: stateWith(event(1, { type: 'task.created' })) });
    await vi.waitFor(() => expect(client.getView().connection.status).toBe('healthy'));
  });

  test.each([
    [{ cursor: 4, state: stateWith(event(1), event(2), event(3), event(4)) }, 'snapshot_cursor_behind'],
    [{ cursor: 6, state: stateWith(event(1), event(2), event(3), event(4), event(5)) }, 'snapshot_cursor_mismatch'],
  ] satisfies [RuntimeSnapshot, string][])('rejects an untrusted snapshot %#', async (snapshot, errorCode) => {
    const trusted = stateWith(event(1), event(2), event(3), event(4), event(5));
    const source = new FakeEventSource([event(7)], snapshot);
    const client = new RuntimeClient(source, trusted);

    await client.connect();
    await vi.waitFor(() => expect(client.getView().connection.status).toBe('offline'));

    expect(client.getView().connection).toEqual({
      status: 'offline',
      lastTrustedCursor: 5,
      errorCode,
    });
    await expect(client.dispatch({ type: 'task.pause' })).rejects.toThrow('writes_disabled:offline');
  });

  test.each([
    ['unauthorized', 'unauthorized'],
    ['incompatible', 'incompatible'],
    ['network_down', 'degraded'],
  ] as const)('maps subscription error %s to %s', async (message, status) => {
    const source = new FakeEventSource();
    source.subscribeError = new Error(message);
    const client = new RuntimeClient(source);

    await client.connect();

    expect(client.getView().connection).toEqual({
      status,
      lastTrustedCursor: 0,
      errorCode: message,
    });
  });

  test('marks invalid source events incompatible without advancing trusted state', async () => {
    const source = new FakeEventSource([
      event(1, { schemaVersion: 2 }),
      event(1, { eventId: 'valid-after-invalid' }),
    ]);
    const client = new RuntimeClient(source);

    await client.connect();

    expect(client.getView().connection).toEqual({
      status: 'incompatible',
      lastTrustedCursor: 0,
      errorCode: 'unsupported_schema_version',
    });
    expect(client.getView().product.committedCursor).toBe(0);
  });

  test('notifies reconnecting and sends mutating commands in idempotent envelopes', async () => {
    const source = new FakeEventSource();
    const client = new RuntimeClient(source);
    const statuses: string[] = [];
    client.subscribe((view) => statuses.push(view.connection.status));

    await client.connect();
    await client.reconnect();

    expect(statuses).toContain('reconnecting');
    expect(source.sent).toEqual([]);

    await client.dispatch({ type: 'control.take', expectedRevision: 7 });
    expect(source.sent).toHaveLength(1);
    expect(source.sent[0]).toMatchObject({
      idempotencyKey: expect.stringMatching(/^cmd-/),
      command: { type: 'control.take', expectedRevision: 7 },
    });
  });

  test('sends only the approval contract and never persists endpoint tokens', async () => {
    const source = new FakeEventSource();
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    const client = new RuntimeClient(source);

    await client.connect();
    await client.dispatch({ type: 'approval.respond', challengeId: 'a-1', decision: 'deny' });

    expect(source.sent).toEqual([
      expect.objectContaining({
        command: { type: 'approval.respond', challengeId: 'a-1', decision: 'deny' },
      }),
    ]);
    expect(setItem).not.toHaveBeenCalled();
    setItem.mockRestore();
  });

  test('surfaces a rejected command receipt as an actionable dispatch error', async () => {
    const source = new FakeEventSource();
    source.rejectionCode = 'stale_lease';
    const client = new RuntimeClient(source);
    await client.connect();

    await expect(client.dispatch({ type: 'control.take', expectedRevision: 4 }))
      .rejects.toThrow('command_rejected:stale_lease');
    expect(source.sent).toHaveLength(1);
  });

  test('disconnects into offline mode, closes the source, and blocks commands', async () => {
    const source = new FakeEventSource();
    const client = new RuntimeClient(source);

    await client.connect();
    await client.disconnect();

    expect(client.getView().connection).toEqual({ status: 'offline', lastTrustedCursor: 0 });
    expect(source.closeCalls).toBe(1);
    await expect(client.dispatch({ type: 'instruction.send', content: 'continue' }))
      .rejects.toThrow('writes_disabled:offline');
  });

  test('does not resume a stale recovery after disconnecting', async () => {
    let resolveSnapshot!: (snapshot: RuntimeSnapshot) => void;
    const source = new FakeEventSource();
    source.getSnapshot = vi.fn(() => new Promise<RuntimeSnapshot>((resolve) => {
      resolveSnapshot = resolve;
    }));
    const client = new RuntimeClient(source);

    await client.connect();
    source.emit(event(2));
    await vi.waitFor(() => expect(client.getView().connection.status).toBe('resyncing'));
    await client.disconnect();

    resolveSnapshot({ cursor: 1, state: stateWith(event(1, { type: 'task.created' })) });
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(client.getView().connection.status).toBe('offline');
    expect(source.subscribeCalls).toEqual([0]);
  });
});
