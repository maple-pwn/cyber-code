import {
  initialProductState,
  project,
  validateEvent,
  type ProductState,
  type RawProductEvent,
  type ValidatedProductEvent,
} from '@cyber/protocol';

import type { EventSource, RuntimeSnapshot, Unsubscribe } from './index';
import {
  negotiateHandshake,
  validateCommandEnvelope,
  validateCommandReceipt,
  validateRuntimeSnapshot,
  type RuntimeCommandEnvelope,
  type RuntimeCommandReceipt,
  type RuntimeHandshakeRequest,
  type RuntimeHandshakeResponse,
} from './conformance';

export type LocalRequest = {
  id: string;
  type: 'handshake' | 'events' | 'snapshot' | 'command' | 'health' | 'close';
  handshake?: RuntimeHandshakeRequest;
  command?: RuntimeCommandEnvelope;
  taskId?: string;
  afterCursor?: number;
};

export interface LocalTransport {
  start(): Promise<unknown>;
  request(request: LocalRequest): Promise<unknown>;
  restart(): Promise<unknown>;
  stop(): Promise<unknown>;
}

export type LocalEventSourceOptions = {
  pollIntervalMs?: number;
};

type RecordValue = Record<string, unknown>;
export type ParsedRuntimeResponse =
  | { id: string; type: 'handshake'; handshake: RuntimeHandshakeResponse }
  | { id: string; type: 'events'; events: RawProductEvent[] }
  | { id: string; type: 'snapshot'; snapshot: RuntimeSnapshot }
  | { id: string; type: 'command'; receipt: RuntimeCommandReceipt }
  | { id: string; type: 'health'; ready: boolean }
  | { id: string; type: 'closed' };

const productStateKeys = [
  'activeRuntime',
  'task',
  'scope',
  'controlLease',
  'highestCommittedLeaseRevision',
  'agents',
  'timeline',
  'approvals',
  'findings',
  'evidence',
  'report',
  'rawEvents',
  'committedCursor',
  'canonicalEvents',
] as const;

const isRecord = (value: unknown): value is RecordValue =>
  value !== null && typeof value === 'object' && !Array.isArray(value)
  && (Object.getPrototypeOf(value) === Object.prototype || Object.getPrototypeOf(value) === null);

const hasExactKeys = (
  value: RecordValue,
  required: readonly string[],
  optional: readonly string[] = [],
): boolean => {
  const allowed = new Set([...required, ...optional]);
  return required.every((key) => Object.hasOwn(value, key))
    && Object.keys(value).every((key) => allowed.has(key));
};

const nonEmpty = (value: unknown): value is string =>
  typeof value === 'string' && value.trim().length > 0;

const safeCursor = (value: unknown): value is number =>
  Number.isSafeInteger(value) && (value as number) >= 0;

const stringRecord = (value: unknown): value is Record<string, string> =>
  isRecord(value) && Object.values(value).every((item) => typeof item === 'string');

const canonicalize = (value: unknown): string => JSON.stringify(value, (_key, item: unknown) => {
  if (!isRecord(item)) return item;
  return Object.fromEntries(Object.entries(item).sort(([left], [right]) => left.localeCompare(right)));
});

function cleanEvent(value: unknown): RawProductEvent {
  if (!isRecord(value)) throw new Error('invalid_event');
  const validated = validateEvent(value as unknown as RawProductEvent);
  return {
    schemaVersion: validated.schemaVersion,
    eventId: validated.eventId,
    taskId: validated.taskId,
    cursor: validated.cursor,
    occurredAt: validated.occurredAt,
    type: validated.type,
    source: validated.source,
    payload: validated.payload,
  };
}

function cleanProductState(value: unknown): ProductState {
  if (!isRecord(value) || !hasExactKeys(value, productStateKeys)
    || !safeCursor(value.committedCursor)
    || !Number.isSafeInteger(value.highestCommittedLeaseRevision)
    || (value.highestCommittedLeaseRevision as number) < 0
    || !isRecord(value.agents)
    || !Array.isArray(value.timeline)
    || !isRecord(value.approvals)
    || !isRecord(value.findings)
    || !isRecord(value.evidence)
    || !Array.isArray(value.rawEvents)
    || !stringRecord(value.canonicalEvents)) {
    throw new Error('invalid_snapshot_state');
  }
  const state = structuredClone(value) as unknown as ProductState;
  state.timeline = value.timeline.map((item) => validateEvent(cleanEvent(item))) as ValidatedProductEvent[];
  state.rawEvents = value.rawEvents.map((item) => validateEvent(cleanEvent(item))) as ValidatedProductEvent[];
  try {
    const events = [...state.timeline, ...state.rawEvents].sort((left, right) => left.cursor - right.cursor);
    const proven = events.reduce((current, event) => {
      const result = project(current, event);
      if (result.kind === 'resync-required') throw new Error('snapshot_event_gap');
      return result.state;
    }, initialProductState());
    if (canonicalize(proven) !== canonicalize(state)) throw new Error('snapshot_state_mismatch');
  } catch {
    throw new Error('invalid_snapshot_state');
  }
  return state;
}

function parseHandshake(value: unknown): RuntimeHandshakeResponse {
  if (!isRecord(value) || !hasExactKeys(value, [
    'protocolVersion',
    'runtimeId',
    'principal',
    'role',
    'capabilities',
    'source',
  ]) || !isRecord(value.source) || !hasExactKeys(value.source, [
    'mode',
    'runtimeId',
    'principal',
    'capabilities',
  ])) {
    throw new Error('invalid_handshake_response');
  }
  return value as unknown as RuntimeHandshakeResponse;
}

function parseReceipt(value: unknown): RuntimeCommandReceipt {
  if (!isRecord(value) || !nonEmpty(value.idempotencyKey)) {
    throw new Error('invalid_command_receipt');
  }
  if (value.status === 'accepted' && hasExactKeys(value, ['idempotencyKey', 'status'])) {
    return value as RuntimeCommandReceipt;
  }
  if (value.status === 'rejected'
    && hasExactKeys(value, ['idempotencyKey', 'status', 'errorCode'])
    && nonEmpty(value.errorCode)) {
    return value as RuntimeCommandReceipt;
  }
  throw new Error('invalid_command_receipt');
}

export function parseRuntimeResponse(value: unknown, expectedId?: string): ParsedRuntimeResponse {
  if (!isRecord(value) || !nonEmpty(value.id) || !nonEmpty(value.type)) {
    throw new Error('runtime_response_invalid');
  }
  if (expectedId !== undefined && value.id !== expectedId) {
    throw new Error('runtime_response_id_mismatch');
  }
  if (value.type === 'error') {
    if (!hasExactKeys(value, ['id', 'type', 'errorCode']) || !nonEmpty(value.errorCode)) {
      throw new Error('runtime_response_invalid');
    }
    throw new Error(value.errorCode);
  }
  switch (value.type) {
    case 'handshake':
      if (!hasExactKeys(value, ['id', 'type', 'handshake'])) throw new Error('runtime_response_invalid');
      return { id: value.id, type: 'handshake', handshake: parseHandshake(value.handshake) };
    case 'events':
      if (!hasExactKeys(value, ['id', 'type', 'events']) || !Array.isArray(value.events)) {
        throw new Error('runtime_response_invalid');
      }
      return { id: value.id, type: 'events', events: value.events.map(cleanEvent) };
    case 'snapshot': {
      if (!hasExactKeys(value, ['id', 'type', 'snapshot']) || !isRecord(value.snapshot)
        || !hasExactKeys(value.snapshot, ['cursor', 'state']) || !safeCursor(value.snapshot.cursor)) {
        throw new Error('runtime_response_invalid');
      }
      return {
        id: value.id,
        type: 'snapshot',
        snapshot: { cursor: value.snapshot.cursor, state: cleanProductState(value.snapshot.state) },
      };
    }
    case 'command':
      if (!hasExactKeys(value, ['id', 'type', 'receipt']) || !isRecord(value.receipt)) {
        throw new Error('runtime_response_invalid');
      }
      return { id: value.id, type: 'command', receipt: parseReceipt(value.receipt) };
    case 'health':
      if (!hasExactKeys(value, ['id', 'type', 'ready']) || typeof value.ready !== 'boolean') {
        throw new Error('runtime_response_invalid');
      }
      return { id: value.id, type: 'health', ready: value.ready };
    case 'closed':
      if (!hasExactKeys(value, ['id', 'type'])) throw new Error('runtime_response_invalid');
      return { id: value.id, type: 'closed' };
    default:
      throw new Error('runtime_response_invalid');
  }
}

export class LocalEventSource implements EventSource {
  private readonly pollIntervalMs: number;
  private requestSequence = 0;
  private started = false;
  private restartRequired = false;
  private subscriptions = new Set<{ active: boolean; timer?: ReturnType<typeof setTimeout> }>();

  constructor(
    private readonly transport: LocalTransport,
    options: LocalEventSourceOptions = {},
  ) {
    this.pollIntervalMs = options.pollIntervalMs ?? 250;
    if (!Number.isSafeInteger(this.pollIntervalMs) || this.pollIntervalMs < 1) {
      throw new Error('invalid_poll_interval');
    }
  }

  async handshake(request: RuntimeHandshakeRequest): Promise<RuntimeHandshakeResponse> {
    let lifecycleChanged = false;
    try {
      let response: ParsedRuntimeResponse;
      let restarted = false;
      if (!this.started) {
        const raw = await this.transportCall(() => this.transport.start());
        this.started = true;
        lifecycleChanged = true;
        response = parseRuntimeResponse(raw);
      } else if (this.restartRequired) {
        const raw = await this.transportCall(() => this.transport.restart());
        lifecycleChanged = true;
        response = parseRuntimeResponse(raw);
        restarted = true;
      } else {
        response = await this.request({ type: 'handshake', handshake: request });
      }
      if (response.type !== 'handshake') throw new Error('runtime_response_invalid');
      const metadata = negotiateHandshake(request, response.handshake);
      if (metadata.mode !== 'local') throw new Error('local_source_required');
      if (restarted) this.restartRequired = false;
      return response.handshake;
    } catch (error) {
      if (lifecycleChanged) {
        try {
          await this.transport.stop();
        } catch {
          // Preserve the protocol failure that made the runtime untrusted.
        }
        this.started = false;
        this.restartRequired = false;
      }
      throw error;
    }
  }

  async subscribe(
    afterCursor: number,
    onEvent: (event: RawProductEvent) => void,
    onError?: (error: unknown) => void,
  ): Promise<Unsubscribe> {
    if (!safeCursor(afterCursor)) throw new Error('invalid_trusted_cursor');
    const subscription: { active: boolean; timer?: ReturnType<typeof setTimeout> } = { active: true };
    this.subscriptions.add(subscription);
    let cursor = afterCursor;

    const poll = async (): Promise<void> => {
      const response = await this.request({ type: 'events', afterCursor: cursor });
      if (response.type !== 'events') throw new Error('runtime_response_invalid');
      if (!subscription.active) return;
      for (const raw of response.events) {
        if ((raw.cursor as number) > cursor) cursor = raw.cursor as number;
        onEvent(raw);
      }
    };

    try {
      await poll();
    } catch (error) {
      subscription.active = false;
      this.subscriptions.delete(subscription);
      throw error;
    }

    const schedule = (): void => {
      if (!subscription.active) return;
      subscription.timer = setTimeout(() => {
        void poll().then(schedule).catch((error: unknown) => {
          subscription.active = false;
          this.subscriptions.delete(subscription);
          onError?.(error);
        });
      }, this.pollIntervalMs);
    };
    schedule();

    return () => {
      subscription.active = false;
      if (subscription.timer !== undefined) clearTimeout(subscription.timer);
      this.subscriptions.delete(subscription);
    };
  }

  async getSnapshot(): Promise<RuntimeSnapshot> {
    const response = await this.request({ type: 'snapshot' });
    if (response.type !== 'snapshot') throw new Error('runtime_response_invalid');
    return validateRuntimeSnapshot(response.snapshot, 0);
  }

  async send(envelope: RuntimeCommandEnvelope): Promise<RuntimeCommandReceipt> {
    validateCommandEnvelope(envelope);
    const response = await this.request({ type: 'command', command: envelope });
    if (response.type !== 'command') throw new Error('runtime_response_invalid');
    return validateCommandReceipt(response.receipt, envelope);
  }

  async close(): Promise<void> {
    for (const subscription of this.subscriptions) {
      subscription.active = false;
      if (subscription.timer !== undefined) clearTimeout(subscription.timer);
    }
    this.subscriptions.clear();
    try {
      if (this.started) {
        const response = await this.request({ type: 'close' });
        if (response.type !== 'closed') throw new Error('runtime_response_invalid');
      }
    } finally {
      await this.transportCall(() => this.transport.stop());
      this.started = false;
      this.restartRequired = false;
    }
  }

  private async request(request: Omit<LocalRequest, 'id'>): Promise<ParsedRuntimeResponse> {
    if (!this.started) throw new Error('local_runtime_not_started');
    const value: LocalRequest = {
      id: `local-${(++this.requestSequence).toString(36)}`,
      ...request,
    };
    let raw: unknown;
    try {
      raw = await this.transport.request(value);
    } catch {
      this.restartRequired = true;
      throw new Error('local_transport_unavailable');
    }
    return parseRuntimeResponse(raw, value.id);
  }

  private async transportCall<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch {
      throw new Error('local_transport_unavailable');
    }
  }
}
