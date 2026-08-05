import type { RawProductEvent } from '@cyber/protocol';

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
import {
  parseRuntimeResponse,
  type LocalRequest,
  type ParsedRuntimeResponse,
} from './local-event-source';

export type RemoteRequest = Omit<LocalRequest, 'taskId'>;

export interface RemoteTransport {
  request(request: RemoteRequest): Promise<unknown>;
  close?(): Promise<void>;
}

export type AccessTokenProvider = () => string | Promise<string>;
export type FetchRemoteTransportOptions = {
  fetch?: typeof globalThis.fetch;
  maxResponseBytes?: number;
  requestTimeoutMs?: number;
};

const defaultMaxResponseBytes = 1 << 20;

export class FetchRemoteTransport implements RemoteTransport {
  private readonly endpoint: string;
  private readonly fetch: typeof globalThis.fetch;
  private readonly maxResponseBytes: number;
  private readonly requestTimeoutMs: number;
  private readonly activeRequests = new Set<AbortController>();

  constructor(
    endpoint: string,
    private readonly tokenProvider: AccessTokenProvider,
    options: FetchRemoteTransportOptions = {},
  ) {
    const parsed = new URL(endpoint);
    if (parsed.protocol !== 'https:') throw new Error('remote_tls_required');
    if (parsed.username || parsed.password || parsed.hash) throw new Error('invalid_remote_endpoint');
    this.endpoint = parsed.toString();
    this.fetch = options.fetch ?? globalThis.fetch;
    this.maxResponseBytes = options.maxResponseBytes ?? defaultMaxResponseBytes;
    this.requestTimeoutMs = options.requestTimeoutMs ?? 15_000;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1
      || !Number.isSafeInteger(this.requestTimeoutMs) || this.requestTimeoutMs < 1) {
      throw new Error('invalid_remote_response_limit');
    }
  }

  async request(request: RemoteRequest): Promise<unknown> {
    const token = (await this.tokenProvider()).trim();
    if (!token) throw new Error('unauthorized');
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), this.requestTimeoutMs);
    this.activeRequests.add(controller);
    try {
      let response: Response;
      try {
        response = await this.fetch(this.endpoint, {
          method: 'POST',
          credentials: 'include',
          redirect: 'error',
          signal: controller.signal,
          headers: {
            Accept: 'application/json',
            Authorization: `Bearer ${token}`,
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(request),
        });
      } catch {
        throw new Error('remote_transport_unavailable');
      }
      if (response.status === 401) {
        await response.body?.cancel().catch(() => undefined);
        throw new Error('unauthorized');
      }
      if (response.status === 426) {
        await response.body?.cancel().catch(() => undefined);
        throw new Error('incompatible');
      }
      const text = await this.readBounded(response);
      let value: unknown;
      try {
        value = JSON.parse(text) as unknown;
      } catch {
        if (!response.ok) throw new Error('remote_transport_unavailable');
        throw new Error('runtime_response_invalid');
      }
      if (!response.ok) {
        const code = value !== null && typeof value === 'object' && !Array.isArray(value)
          && typeof (value as Record<string, unknown>).errorCode === 'string'
          ? (value as Record<string, string>).errorCode
          : undefined;
        throw new Error(code ?? 'remote_transport_unavailable');
      }
      return value;
    } finally {
      clearTimeout(timeout);
      this.activeRequests.delete(controller);
    }
  }

  async close(): Promise<void> {
    for (const controller of this.activeRequests) controller.abort();
    this.activeRequests.clear();
  }

  private async readBounded(response: Response): Promise<string> {
    const declaredLength = Number(response.headers.get('content-length'));
    if (Number.isFinite(declaredLength) && declaredLength > this.maxResponseBytes) {
      await response.body?.cancel().catch(() => undefined);
      throw new Error('remote_response_too_large');
    }
    if (response.body === null) return '';
    const reader = response.body.getReader();
    const chunks: Uint8Array[] = [];
    let length = 0;
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        length += value.byteLength;
        if (length > this.maxResponseBytes) {
          await reader.cancel().catch(() => undefined);
          throw new Error('remote_response_too_large');
        }
        chunks.push(value);
      }
    } catch (error) {
      if (error instanceof Error && error.message === 'remote_response_too_large') throw error;
      throw new Error('remote_transport_unavailable');
    } finally {
      reader.releaseLock();
    }
    const bytes = new Uint8Array(length);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.byteLength;
    }
    try {
      return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    } catch {
      throw new Error('runtime_response_invalid');
    }
  }
}

export type RemoteEventSourceOptions = {
  pollIntervalMs?: number;
  reconnectInitialMs?: number;
  reconnectMaxMs?: number;
  reconnectAttempts?: number;
};

const safePositiveInteger = (value: number): boolean => Number.isSafeInteger(value) && value > 0;

type RemoteSubscription = {
  active: boolean;
  scheduleTimer?: ReturnType<typeof setTimeout>;
  backoffTimer?: ReturnType<typeof setTimeout>;
  wakeBackoff?: () => void;
};

export class RemoteEventSource implements EventSource {
  private readonly pollIntervalMs: number;
  private readonly reconnectInitialMs: number;
  private readonly reconnectMaxMs: number;
  private readonly reconnectAttempts: number;
  private requestSequence = 0;
  private connected = false;
  private subscriptions = new Set<RemoteSubscription>();

  constructor(
    private readonly transport: RemoteTransport,
    options: RemoteEventSourceOptions = {},
  ) {
    this.pollIntervalMs = options.pollIntervalMs ?? 250;
    this.reconnectInitialMs = options.reconnectInitialMs ?? 250;
    this.reconnectMaxMs = options.reconnectMaxMs ?? 5_000;
    this.reconnectAttempts = options.reconnectAttempts ?? 4;
    if (![this.pollIntervalMs, this.reconnectInitialMs, this.reconnectMaxMs, this.reconnectAttempts]
      .every(safePositiveInteger)
      || this.reconnectInitialMs > this.reconnectMaxMs) {
      throw new Error('invalid_remote_reconnect_policy');
    }
  }

  async handshake(request: RuntimeHandshakeRequest): Promise<RuntimeHandshakeResponse> {
    this.connected = false;
    const response = await this.request({ type: 'handshake', handshake: request }, false);
    if (response.type !== 'handshake') throw new Error('runtime_response_invalid');
    const metadata = negotiateHandshake(request, response.handshake);
    if (metadata.mode !== 'remote') throw new Error('remote_source_required');
    this.connected = true;
    return response.handshake;
  }

  async subscribe(
    afterCursor: number,
    onEvent: (event: RawProductEvent) => void,
    onError?: (error: unknown) => void,
  ): Promise<Unsubscribe> {
    if (!Number.isSafeInteger(afterCursor) || afterCursor < 0) throw new Error('invalid_trusted_cursor');
    if (!this.connected) throw new Error('remote_runtime_not_connected');
    const subscription: RemoteSubscription = { active: true };
    this.subscriptions.add(subscription);
    let cursor = afterCursor;

    const poll = async (): Promise<void> => {
      let backoff = this.reconnectInitialMs;
      for (let attempt = 0; ; attempt += 1) {
        if (!subscription.active) return;
        try {
          const response = await this.request({ type: 'events', afterCursor: cursor });
          if (response.type !== 'events') throw new Error('runtime_response_invalid');
          if (!subscription.active) return;
          for (const raw of response.events) {
            if ((raw.cursor as number) > cursor) cursor = raw.cursor as number;
            onEvent(raw);
          }
          return;
        } catch (error) {
          if (!subscription.active) return;
          if (attempt >= this.reconnectAttempts - 1 || !this.retryable(error)) throw error;
          await this.waitForBackoff(subscription, backoff);
          backoff = Math.min(backoff * 2, this.reconnectMaxMs);
        }
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
      subscription.scheduleTimer = setTimeout(() => {
        void poll().then(schedule).catch((error: unknown) => {
          subscription.active = false;
          this.subscriptions.delete(subscription);
          onError?.(error);
        });
      }, this.pollIntervalMs);
    };
    schedule();
    return () => {
      this.cancelSubscription(subscription);
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
      this.cancelSubscription(subscription);
    }
    this.subscriptions.clear();
    try {
      if (this.connected) {
        const response = await this.request({ type: 'close' });
        if (response.type !== 'closed') throw new Error('runtime_response_invalid');
      }
    } finally {
      this.connected = false;
      await this.transport.close?.();
    }
  }

  private async request(
    request: Omit<RemoteRequest, 'id'>,
    requireConnected = true,
  ): Promise<ParsedRuntimeResponse> {
    if (requireConnected && !this.connected) throw new Error('remote_runtime_not_connected');
    const value: RemoteRequest = { id: `remote-${(++this.requestSequence).toString(36)}`, ...request };
    const raw = await this.transport.request(value);
    return parseRuntimeResponse(raw, value.id);
  }

  private retryable(error: unknown): boolean {
    return error instanceof Error && error.message === 'remote_transport_unavailable';
  }

  private waitForBackoff(subscription: RemoteSubscription, milliseconds: number): Promise<void> {
    return new Promise((resolve) => {
      if (!subscription.active) {
        resolve();
        return;
      }
      const wake = (): void => {
        if (subscription.backoffTimer !== undefined) clearTimeout(subscription.backoffTimer);
        subscription.backoffTimer = undefined;
        subscription.wakeBackoff = undefined;
        resolve();
      };
      subscription.wakeBackoff = wake;
      subscription.backoffTimer = setTimeout(wake, milliseconds);
    });
  }

  private cancelSubscription(subscription: RemoteSubscription): void {
    subscription.active = false;
    if (subscription.scheduleTimer !== undefined) clearTimeout(subscription.scheduleTimer);
    subscription.scheduleTimer = undefined;
    subscription.wakeBackoff?.();
    this.subscriptions.delete(subscription);
  }
}
