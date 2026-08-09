import {
  initialProductState,
  project,
  validateEvent,
  type ProductState,
  type RawProductEvent,
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
	  type RuntimeSourceMode,
} from './conformance';

export type CyberAgentRequest = {
  method: 'GET' | 'POST';
  path: string;
  body?: unknown;
  idempotencyKey?: string;
  signal?: AbortSignal;
};

export interface CyberAgentTransport {
  request(request: CyberAgentRequest): Promise<unknown>;
  events(sessionId: string, afterSequence: number, signal: AbortSignal): AsyncIterable<unknown>;
  close?(): Promise<void>;
}

export type CyberAgentTokenProvider = () => string | Promise<string>;
export type FetchCyberAgentTransportOptions = {
  fetch?: typeof globalThis.fetch;
  allowInsecureLoopback?: boolean;
  maxResponseBytes?: number;
};

export class FetchCyberAgentTransport implements CyberAgentTransport {
  private readonly endpoint: URL;
  private readonly fetch: typeof globalThis.fetch;
  private readonly maxResponseBytes: number;
  private readonly active = new Set<AbortController>();

  constructor(endpoint: string, private readonly tokenProvider: CyberAgentTokenProvider, options: FetchCyberAgentTransportOptions = {}) {
    const parsed = new URL(endpoint);
    const loopback = parsed.hostname === '127.0.0.1' || parsed.hostname === 'localhost' || parsed.hostname === '[::1]';
    if (parsed.protocol !== 'https:' && !(options.allowInsecureLoopback === true && parsed.protocol === 'http:' && loopback)) {
      throw new Error('cyber_agent_tls_required');
    }
    if (parsed.username || parsed.password || parsed.hash) throw new Error('invalid_cyber_agent_endpoint');
    this.endpoint = parsed;
    this.fetch = options.fetch ?? globalThis.fetch;
    this.maxResponseBytes = options.maxResponseBytes ?? (1 << 20);
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new Error('invalid_cyber_agent_response_limit');
  }

  async request(request: CyberAgentRequest): Promise<unknown> {
    const { response, cleanup } = await this.perform(request.path, request.method, request.body, request.idempotencyKey, request.signal, 'application/json');
    try {
      const bytes = await readBounded(response, this.maxResponseBytes);
      try {
        return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)) as unknown;
      } catch {
        throw new Error('invalid_cyber_agent_response');
      }
    } finally {
      cleanup();
    }
  }

  async *events(sessionId: string, afterSequence: number, signal: AbortSignal): AsyncIterable<unknown> {
    if (!text(sessionId) || !integer(afterSequence)) throw new Error('invalid_cyber_agent_cursor');
    const path = `/v1/sessions/${encodeURIComponent(sessionId)}/events?after_sequence=${afterSequence}`;
    const { response, cleanup } = await this.perform(path, 'GET', undefined, undefined, signal, 'text/event-stream');
    if (!response.headers.get('content-type')?.toLowerCase().includes('text/event-stream') || response.body === null) {
      cleanup();
      throw new Error('invalid_cyber_agent_event_stream');
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder('utf-8', { fatal: true });
    let buffered = '';
    let data: string[] = [];
    let eventBytes = 0;
    const flush = (): unknown | undefined => {
      if (data.length === 0) return undefined;
      const encoded = data.join('\n');
      data = [];
      eventBytes = 0;
      try { return JSON.parse(encoded) as unknown; } catch { throw new Error('invalid_cyber_agent_event'); }
    };
    try {
      for (;;) {
        const chunk = await reader.read();
        if (chunk.done) break;
        buffered += decoder.decode(chunk.value, { stream: true });
        for (;;) {
          const newline = buffered.indexOf('\n');
          if (newline < 0) break;
          let line = buffered.slice(0, newline);
          buffered = buffered.slice(newline + 1);
          if (line.endsWith('\r')) line = line.slice(0, -1);
          if (line === '') {
            const value = flush();
            if (value !== undefined) yield value;
          } else if (line.startsWith('data:')) {
            const value = line.slice(5).replace(/^ /, '');
            eventBytes += value.length;
            if (eventBytes > this.maxResponseBytes) throw new Error('cyber_agent_event_too_large');
            data.push(value);
          }
        }
      }
      buffered += decoder.decode();
      if (buffered.startsWith('data:')) data.push(buffered.slice(5).replace(/^ /, ''));
      const value = flush();
      if (value !== undefined) yield value;
    } finally {
      await reader.cancel().catch(() => undefined);
      reader.releaseLock();
      cleanup();
    }
  }

  async close(): Promise<void> {
    for (const controller of this.active) controller.abort();
    this.active.clear();
  }

  private async perform(path: string, method: 'GET' | 'POST', body: unknown, idempotencyKey: string | undefined, parentSignal: AbortSignal | undefined, accept: string): Promise<{ response: Response; cleanup: () => void }> {
    const token = (await this.tokenProvider()).trim();
    if (!token) throw new Error('unauthorized');
    const controller = new AbortController();
    const abort = (): void => controller.abort();
    parentSignal?.addEventListener('abort', abort, { once: true });
    this.active.add(controller);
    try {
      const headers = new Headers({ Accept: accept, Authorization: `Bearer ${token}` });
      if (body !== undefined) headers.set('Content-Type', 'application/json');
      if (idempotencyKey !== undefined) headers.set('Idempotency-Key', idempotencyKey);
      const response = await this.fetch(new URL(path, this.endpoint), {
        method, headers, redirect: 'error', credentials: 'omit', signal: controller.signal,
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      if (!response.ok) {
        await response.body?.cancel().catch(() => undefined);
        if (response.status === 401) throw new Error('unauthorized');
        if (response.status === 426) throw new Error('incompatible');
        if (response.status === 409) throw new Error('cyber_agent_conflict');
        throw new Error(`cyber_agent_http_${response.status}`);
      }
      return {
        response,
        cleanup: () => {
          parentSignal?.removeEventListener('abort', abort);
          this.active.delete(controller);
        },
      };
    } catch (error) {
      parentSignal?.removeEventListener('abort', abort);
      this.active.delete(controller);
      throw error;
    }
  }
}

async function readBounded(response: Response, limit: number): Promise<Uint8Array> {
  if (response.body === null) return new Uint8Array();
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.byteLength;
      if (length > limit) throw new Error('cyber_agent_response_too_large');
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const result = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) { result.set(chunk, offset); offset += chunk.byteLength; }
  return result;
}

type SourceEvent = {
  event_id: string;
  task_id: string;
  session_id: string;
  sequence: number;
  topic: string;
  payload: Record<string, unknown>;
  emitted_at: string;
};

type SessionSnapshot = {
  session_id: string;
  task_id: string;
  event_cursor: { session_id: string; sequence: number; event_id: string };
  pending_interaction?: { interaction_id: string; session_id: string } | null;
};

const capabilities = ['events', 'snapshot', 'commands'];
const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value);
const text = (value: unknown): value is string => typeof value === 'string' && value.trim().length > 0;
const integer = (value: unknown): value is number => Number.isSafeInteger(value) && (value as number) >= 0;

export class CyberAgentEventSource implements EventSource {
  private connected = false;
  private sessionId?: string;
  private taskId?: string;
  private state: ProductState = initialProductState();
  private readonly subscriptions = new Set<AbortController>();

  constructor(
    private readonly transport: CyberAgentTransport,
    private readonly runtimeId = 'cyber-agent-remote',
	private readonly mode: RuntimeSourceMode = 'remote',
  ) {}

  async handshake(request: RuntimeHandshakeRequest): Promise<RuntimeHandshakeResponse> {
    this.connected = false;
    const raw = await this.transport.request({ method: 'GET', path: '/v1/capabilities' });
    if (!object(raw) || raw.product !== 'cyber-agent' || !text(raw.runtime_version)
      || !integer(raw.protocol_version) || !Array.isArray(raw.capabilities)
      || !raw.capabilities.includes('session.events.v1')) throw new Error('invalid_cyber_agent_capabilities');
    const response: RuntimeHandshakeResponse = {
      protocolVersion: raw.protocol_version,
      runtimeId: this.runtimeId,
      principal: 'cyber-agent',
      role: 'owner',
      capabilities: [...capabilities],
      source: { mode: this.mode, runtimeId: this.runtimeId, principal: 'cyber-agent', capabilities: [...capabilities] },
    };
    negotiateHandshake(request, response);
    this.connected = true;
    return response;
  }

  async subscribe(afterCursor: number, onEvent: (event: RawProductEvent) => void, onError?: (error: unknown) => void): Promise<Unsubscribe> {
    if (!this.connected) throw new Error('cyber_agent_not_connected');
    if (!integer(afterCursor)) throw new Error('invalid_trusted_cursor');
    if (!this.sessionId) return () => undefined;
    const controller = new AbortController();
    this.subscriptions.add(controller);
    void this.pump(this.sessionId, afterCursor, controller.signal, onEvent)
      .catch((error: unknown) => { if (!controller.signal.aborted) onError?.(error); })
      .finally(() => this.subscriptions.delete(controller));
    return () => {
      controller.abort();
      this.subscriptions.delete(controller);
    };
  }

  async getSnapshot(): Promise<RuntimeSnapshot> {
    if (!this.connected) throw new Error('cyber_agent_not_connected');
    if (!this.sessionId) return { cursor: 0, state: initialProductState() };
    const snapshot = parseSnapshot(await this.transport.request({ method: 'GET', path: `/v1/sessions/${encodeURIComponent(this.sessionId)}` }));
    this.bind(snapshot);
    if (snapshot.event_cursor.sequence < this.state.committedCursor) throw new Error('snapshot_cursor_behind');
    if (snapshot.event_cursor.sequence > this.state.committedCursor) {
      this.state = await this.rebuild(snapshot.event_cursor.sequence);
    }
    return validateRuntimeSnapshot({ cursor: snapshot.event_cursor.sequence, state: this.state }, 0);
  }

  async send(envelope: RuntimeCommandEnvelope): Promise<RuntimeCommandReceipt> {
    validateCommandEnvelope(envelope);
    if (!this.connected) throw new Error('cyber_agent_not_connected');
    const { command } = envelope;
    try {
      let raw: unknown;
      switch (command.type) {
        case 'task.create': {
          raw = await this.transport.request({
            method: 'POST', path: '/v1/sessions', idempotencyKey: envelope.idempotencyKey,
            body: { kind: 'natural_language', task_id: `security-task-${globalThis.crypto.randomUUID()}`, content: command.objective },
          });
          const created = parseSnapshot(raw);
          this.state = initialProductState();
          this.sessionId = undefined;
          this.taskId = undefined;
          this.bind(created);
          break;
        }
        case 'instruction.send':
          raw = await this.sessionMutation('/turns', envelope.idempotencyKey, { content: command.content });
          this.bind(parseSnapshot(raw));
          break;
        case 'task.resume':
          raw = await this.sessionMutation('/resume', envelope.idempotencyKey);
          this.bind(parseSnapshot(raw));
          break;
        case 'task.cancel':
          raw = await this.sessionMutation('/cancel', envelope.idempotencyKey);
          this.bind(parseSnapshot(raw));
          break;
        case 'scope.confirm':
        case 'approval.respond': {
          const current = parseSnapshot(await this.sessionRequest());
          if (!current.pending_interaction) return rejected(envelope, 'interaction_unavailable');
          const approved = command.type === 'scope.confirm' || command.decision === 'allow_once';
          raw = await this.sessionMutation('/interactions', envelope.idempotencyKey, {
            interaction_id: current.pending_interaction.interaction_id,
            session_id: current.session_id,
            approved,
            responded_at: new Date().toISOString(),
          });
          this.bind(parseSnapshot(raw));
          break;
        }
        default:
          return rejected(envelope, 'command_unsupported');
      }
      return validateCommandReceipt({ idempotencyKey: envelope.idempotencyKey, status: 'accepted' }, envelope);
    } catch (error) {
      if (error instanceof Error && error.message === 'unauthorized') throw error;
      throw error;
    }
  }

  async close(): Promise<void> {
    for (const subscription of this.subscriptions) subscription.abort();
    this.subscriptions.clear();
    this.connected = false;
    await this.transport.close?.();
  }

  private async pump(sessionId: string, after: number, signal: AbortSignal, onEvent: (event: RawProductEvent) => void): Promise<void> {
    for await (const raw of this.transport.events(sessionId, after, signal)) {
      if (signal.aborted) return;
      const event = mapSourceEvent(parseSourceEvent(raw), this.runtimeId);
      const result = project(this.state, validateEvent(event));
      if (result.kind !== 'resync-required') this.state = result.state;
      onEvent(event);
    }
  }

  private async rebuild(targetCursor: number): Promise<ProductState> {
    if (!this.sessionId) return initialProductState();
    const controller = new AbortController();
    let state = initialProductState();
    try {
      for await (const raw of this.transport.events(this.sessionId, 0, controller.signal)) {
        const event = mapSourceEvent(parseSourceEvent(raw), this.runtimeId);
        const result = project(state, validateEvent(event));
        if (result.kind === 'resync-required') throw new Error('cyber_agent_replay_gap');
        state = result.state;
        if (state.committedCursor === targetCursor) break;
      }
    } finally {
      controller.abort();
    }
    if (state.committedCursor !== targetCursor) throw new Error('cyber_agent_snapshot_resync_incomplete');
    return state;
  }

  private async sessionRequest(): Promise<unknown> {
    if (!this.sessionId) throw new Error('cyber_agent_session_unavailable');
    return this.transport.request({ method: 'GET', path: `/v1/sessions/${encodeURIComponent(this.sessionId)}` });
  }

  private async sessionMutation(suffix: string, idempotencyKey: string, body?: unknown): Promise<unknown> {
    if (!this.sessionId) throw new Error('cyber_agent_session_unavailable');
    return this.transport.request({ method: 'POST', path: `/v1/sessions/${encodeURIComponent(this.sessionId)}${suffix}`, body, idempotencyKey });
  }

  private bind(snapshot: SessionSnapshot): void {
    if ((this.sessionId && this.sessionId !== snapshot.session_id) || (this.taskId && this.taskId !== snapshot.task_id)) {
      throw new Error('cyber_agent_session_binding_conflict');
    }
    this.sessionId = snapshot.session_id;
    this.taskId = snapshot.task_id;
  }
}

function rejected(envelope: RuntimeCommandEnvelope, errorCode: string): RuntimeCommandReceipt {
  return validateCommandReceipt({ idempotencyKey: envelope.idempotencyKey, status: 'rejected', errorCode }, envelope);
}

function parseSnapshot(value: unknown): SessionSnapshot {
  if (!object(value) || !text(value.session_id) || !text(value.task_id) || !object(value.event_cursor)
    || value.event_cursor.session_id !== value.session_id || !integer(value.event_cursor.sequence)
    || typeof value.event_cursor.event_id !== 'string') throw new Error('invalid_cyber_agent_snapshot');
  const pending = value.pending_interaction;
  if (pending !== undefined && pending !== null && (!object(pending) || !text(pending.interaction_id) || pending.session_id !== value.session_id)) {
    throw new Error('invalid_cyber_agent_snapshot');
  }
  return value as SessionSnapshot;
}

function parseSourceEvent(value: unknown): SourceEvent {
  if (!object(value) || !text(value.event_id) || !text(value.task_id) || !text(value.session_id)
    || !Number.isSafeInteger(value.sequence) || (value.sequence as number) < 1 || !text(value.topic)
    || !object(value.payload) || !text(value.emitted_at) || Number.isNaN(Date.parse(value.emitted_at as string))) {
    throw new Error('invalid_cyber_agent_event');
  }
  return value as SourceEvent;
}

function mapSourceEvent(source: SourceEvent, runtimeId: string): RawProductEvent {
  let type = `cyber-agent.${source.topic}`;
  let payload: Record<string, unknown> = { ...source.payload };
  switch (source.topic) {
    case 'session.created':
      type = 'task.created'; payload = { title: `Security task ${source.task_id}` }; break;
    case 'session.status':
    case 'session.terminal': {
      const status = source.payload.status;
      if (status === 'active') { type = 'task.started'; payload = { title: `Security task ${source.task_id}` }; }
      else if (status === 'waiting_interaction') { type = 'task.paused'; payload = {}; }
      else if (status === 'completed') { type = 'task.completed'; payload = {}; }
      else if (status === 'cancelled') { type = 'task.cancelled'; payload = {}; }
      else if (status === 'failed') { type = 'task.failed'; payload = { reason: 'cyber-agent session failed' }; }
      break;
    }
    case 'scope.proposed':
    case 'scope.confirmed':
      type = source.topic;
      payload = { scope: {
        id: source.payload.scope_id, principal: 'cyber-agent', workspace: 'security-runtime', validity: 'session',
        targets: source.payload.targets, allowedActions: [], deniedActions: [], riskCeiling: 'runtime-authoritative',
      } };
      break;
    case 'finding.created':
      type = 'finding.created'; payload = { finding: {
        id: source.payload.finding_id, title: source.payload.title, severity: source.payload.severity,
        status: 'candidate', confidence: 'runtime-verified', evidenceIds: source.payload.evidence_refs ?? [],
      } }; break;
    case 'finding.verifying': case 'finding.confirmed':
      type = source.topic; payload = { findingId: source.payload.finding_id }; break;
    case 'finding.rejected':
      type = source.topic; payload = { findingId: source.payload.finding_id, reason: source.payload.reason ?? 'rejected by cyber-agent' }; break;
  }
  return {
    schemaVersion: 1,
    eventId: source.event_id,
    taskId: source.task_id,
    cursor: source.sequence,
    occurredAt: source.emitted_at,
    type,
    source: { runtimeId },
    payload,
  };
}
