import {
  initialProductState,
  project,
  validateEvent,
  type ProductState,
  type RawProductEvent,
} from '@cyber/protocol';

import type {
  ConnectionState,
  ConnectionStatus,
  EventSource,
  RuntimeCommand,
  RuntimeView,
  Unsubscribe,
} from './index';
import {
  negotiateHandshake,
  validateCommandReceipt,
  validateRuntimeSnapshot,
  type RuntimeCommandReceipt,
  type RuntimeSourceMetadata,
} from './conformance';

const blockedStatuses = new Set<ConnectionStatus>([
  'connecting',
  'degraded',
  'reconnecting',
  'resyncing',
  'offline',
  'incompatible',
  'unauthorized',
]);

const errorCode = (error: unknown): string =>
  error instanceof Error && error.message ? error.message : 'unknown_error';

export class RuntimeClient {
  private product: ProductState;
  private connection: ConnectionState;
  private sourceMetadata: RuntimeSourceMetadata | null = null;
  private readonly listeners = new Set<(view: RuntimeView) => void>();
  private unsubscribeSource?: Unsubscribe;
  private lifecycleRevision = 0;
  private subscribingRevision?: number;
  private pendingResyncRevision?: number;
  private recoveringRevision?: number;
  private commandSequence = 0;

  constructor(private readonly source: EventSource, initial = initialProductState()) {
    this.product = initial;
    this.connection = {
      status: 'offline',
      lastTrustedCursor: initial.committedCursor,
    };
  }

  async connect(): Promise<void> {
    const revision = ++this.lifecycleRevision;
    this.stopSubscription();
    await this.establishSubscription('connecting', revision);
  }

  async reconnect(): Promise<void> {
    const revision = ++this.lifecycleRevision;
    this.stopSubscription();
    await this.establishSubscription('reconnecting', revision);
  }

  async disconnect(): Promise<void> {
    this.lifecycleRevision += 1;
    this.stopSubscription();
    this.setConnection('offline');
    await this.source.close();
  }

  async dispatch(command: RuntimeCommand): Promise<RuntimeCommandReceipt> {
    if (blockedStatuses.has(this.connection.status)) {
      throw new Error(`writes_disabled:${this.connection.status}`);
    }
    const envelope = {
      idempotencyKey: `cmd-${globalThis.crypto.randomUUID()}-${(++this.commandSequence).toString(36)}`,
      command,
    };
    const receipt = validateCommandReceipt(await this.source.send(envelope), envelope);
    if (receipt.status === 'rejected') {
      throw new Error(`command_rejected:${receipt.errorCode}`);
    }
    return receipt;
  }

  getView(): RuntimeView {
    return { connection: this.connection, product: this.product, source: this.sourceMetadata };
  }

  subscribe(listener: (view: RuntimeView) => void): Unsubscribe {
    this.listeners.add(listener);
    listener(this.getView());
    return () => this.listeners.delete(listener);
  }

  private async establishSubscription(
    status: 'connecting' | 'reconnecting' | 'resyncing',
    revision: number,
  ): Promise<void> {
    if (revision !== this.lifecycleRevision) return;
    this.setConnection(status);
    this.subscribingRevision = revision;
    if (this.pendingResyncRevision === revision) this.pendingResyncRevision = undefined;

    try {
      const handshake = await this.source.handshake({
        supportedProtocolVersions: [1],
        afterCursor: this.connection.lastTrustedCursor,
      });
      if (revision !== this.lifecycleRevision) return;
      this.sourceMetadata = negotiateHandshake({
        supportedProtocolVersions: [1],
        afterCursor: this.connection.lastTrustedCursor,
      }, handshake);
      const unsubscribe = await this.source.subscribe(
        this.connection.lastTrustedCursor,
        (raw) => this.receive(raw, revision),
        (error) => this.sourceFailed(error, revision),
      );
      if (this.subscribingRevision === revision) this.subscribingRevision = undefined;

      if (revision !== this.lifecycleRevision) {
        unsubscribe();
        return;
      }

      if (this.connection.status === 'incompatible') {
        unsubscribe();
        return;
      }

      this.unsubscribeSource = unsubscribe;
      if (this.pendingResyncRevision === revision) {
        this.pendingResyncRevision = undefined;
        if (this.recoveringRevision === revision) throw new Error('resync_gap_after_snapshot');
        await this.recoverSnapshot(revision);
        return;
      }

      if (this.connection.status === status) this.setConnection('healthy');
    } catch (error) {
      if (this.subscribingRevision === revision) this.subscribingRevision = undefined;
      if (revision !== this.lifecycleRevision) return;
      if (this.recoveringRevision === revision) throw error;
      this.setConnection(this.mapSourceError(error), errorCode(error));
    }
  }

  private receive(raw: RawProductEvent, revision: number): void {
    if (
      revision !== this.lifecycleRevision
      || this.connection.status === 'incompatible'
      || this.connection.status === 'unauthorized'
      || this.connection.status === 'offline'
    ) return;
    try {
      const result = project(this.product, validateEvent(raw));
      if (result.kind === 'resync-required') {
        this.setConnection('resyncing');
        if (this.subscribingRevision === revision) {
          this.pendingResyncRevision = revision;
        } else if (this.recoveringRevision !== revision) {
          void this.recoverSnapshot(revision);
        }
        return;
      }

      this.product = result.state;
      this.connection = {
        status: this.connection.status,
        lastTrustedCursor: result.state.committedCursor,
      };
      this.notify();
    } catch (error) {
      this.setConnection('incompatible', errorCode(error));
      this.stopSubscription();
    }
  }

  private sourceFailed(error: unknown, revision: number): void {
    if (revision !== this.lifecycleRevision) return;
    this.stopSubscription();
    this.setConnection(this.mapSourceError(error), errorCode(error));
  }

  private async recoverSnapshot(revision: number): Promise<void> {
    if (revision !== this.lifecycleRevision || this.recoveringRevision === revision) return;
    this.recoveringRevision = revision;
    this.setConnection('resyncing');

    try {
      const snapshot = await this.source.getSnapshot();
      if (revision !== this.lifecycleRevision) return;
      validateRuntimeSnapshot(snapshot, this.connection.lastTrustedCursor);
      this.product = snapshot.state;
      this.connection = {
        status: 'resyncing',
        lastTrustedCursor: snapshot.cursor,
      };
      this.notify();
      this.stopSubscription();
      await this.establishSubscription('resyncing', revision);
    } catch (error) {
      if (revision !== this.lifecycleRevision) return;
      this.stopSubscription();
      this.setConnection('offline', errorCode(error));
    } finally {
      if (this.recoveringRevision === revision) this.recoveringRevision = undefined;
    }
  }

  private stopSubscription(): void {
    this.unsubscribeSource?.();
    this.unsubscribeSource = undefined;
  }

  private mapSourceError(error: unknown): ConnectionStatus {
    const code = errorCode(error);
    if (code === 'unauthorized') return 'unauthorized';
    if (code === 'incompatible') return 'incompatible';
    return 'degraded';
  }

  private setConnection(status: ConnectionStatus, code?: string): void {
    this.connection = {
      status,
      lastTrustedCursor: this.connection.lastTrustedCursor,
      ...(code ? { errorCode: code } : {}),
    };
    this.notify();
  }

  private notify(): void {
    const view = this.getView();
    for (const listener of this.listeners) listener(view);
  }
}
