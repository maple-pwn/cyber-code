import {
  RuntimeClient,
  RuntimeSourceFactory,
  type RuntimeCommand,
  type RuntimeView,
  type Unsubscribe,
} from '@cyber/runtime-client';

export type AppRoute = 'new-task' | 'scope-review' | 'mission-control' | 'findings' | 'reports';
export type AppSnapshot = { route: AppRoute; view: RuntimeView };

export class AppStore {
  private snapshot: AppSnapshot;
  private readonly listeners = new Set<() => void>();
  private unsubscribeClient: Unsubscribe;
  private client: RuntimeClient;
  private readonly sourceFactory?: RuntimeSourceFactory;
  private sourceId?: string;
  private commandQueue: Promise<void> = Promise.resolve();

  constructor(clientOrFactory: RuntimeClient | RuntimeSourceFactory, initialRoute: AppRoute) {
    if (clientOrFactory instanceof RuntimeSourceFactory) {
      this.sourceFactory = clientOrFactory;
      this.sourceId = clientOrFactory.defaultSourceId;
      this.client = new RuntimeClient(clientOrFactory.create(this.sourceId));
    } else {
      this.client = clientOrFactory;
    }
    this.snapshot = { route: initialRoute, view: this.client.getView() };
    this.unsubscribeClient = this.subscribeClient();
  }

  private subscribeClient(): Unsubscribe {
    return this.client.subscribe((view) => {
      this.snapshot = { ...this.snapshot, view };
      this.emit();
    });
  }

  readonly getSnapshot = (): AppSnapshot => this.snapshot;

  readonly subscribe = (listener: () => void): Unsubscribe => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  navigate(route: AppRoute): void {
    if (route === this.snapshot.route) return;
    this.snapshot = { ...this.snapshot, route };
    this.emit();
  }

  dispatch(command: RuntimeCommand): Promise<void> {
    const operation = this.commandQueue.then(() => this.dispatchCommand(command));
    this.commandQueue = operation.catch(() => undefined);
    return operation;
  }

  private async dispatchCommand(command: RuntimeCommand): Promise<void> {
    if (command.type === 'task.create' && this.sourceFactory !== undefined) {
      const replace = command.runtimeId !== this.sourceId || this.client.getView().product.task !== null;
      const candidate = replace ? await this.prepareSource(command.runtimeId) : this.client;
      const runtimeId = candidate.getView().source?.runtimeId;
      if (runtimeId === undefined) throw new Error('runtime_source_not_connected');
      try {
        await candidate.dispatch({ ...command, runtimeId });
      } catch (error) {
        if (replace) await candidate.disconnect().catch(() => undefined);
        throw error;
      }
      if (replace) await this.commitSource(command.runtimeId, candidate);
      return;
    }
    await this.client.dispatch(command);
  }

  connect(): Promise<void> { return this.connectSelectedSource(); }
  reconnect(): Promise<void> { return this.client.reconnect(); }
  disconnect(): Promise<void> { return this.client.disconnect(); }

  destroy(): void {
    this.unsubscribeClient();
    this.listeners.clear();
  }

  private emit(): void {
    for (const listener of this.listeners) listener();
  }

  private async prepareSource(sourceId: string): Promise<RuntimeClient> {
    if (this.sourceFactory === undefined) throw new Error('runtime_source_factory_unavailable');
    const candidate = new RuntimeClient(this.sourceFactory.create(sourceId));
    await candidate.connect();
    const expected = this.sourceFactory.options().find((option) => option.id === sourceId);
    const view = candidate.getView();
    if (view.connection.status !== 'healthy') {
      await candidate.disconnect().catch(() => undefined);
      const detail = view.connection.errorCode ? `:${view.connection.errorCode}` : '';
      throw new Error(`runtime_source_connect_failed:${view.connection.status}${detail}`);
    }
    if (expected !== undefined && view.source !== null && expected.mode !== view.source.mode) {
      await candidate.disconnect().catch(() => undefined);
      throw new Error(`runtime_source_mode_mismatch:${expected.mode}:${view.source.mode}`);
    }
    return candidate;
  }

  private async commitSource(sourceId: string, candidate: RuntimeClient): Promise<void> {
    const previousClient = this.client;
    this.unsubscribeClient();
    this.client = candidate;
    this.sourceId = sourceId;
    this.snapshot = { ...this.snapshot, view: this.client.getView() };
    this.unsubscribeClient = this.subscribeClient();
    await previousClient.disconnect().catch(() => undefined);
  }

  private async connectSelectedSource(): Promise<void> {
    await this.client.connect();
    if (this.sourceFactory === undefined || this.sourceId === undefined) return;
    const expected = this.sourceFactory.options().find((option) => option.id === this.sourceId);
    const actualMode = this.client.getView().source?.mode;
    if (expected !== undefined && actualMode !== undefined && expected.mode !== actualMode) {
      await this.client.disconnect();
      throw new Error(`runtime_source_mode_mismatch:${expected.mode}:${actualMode}`);
    }
  }
}

export function createAppStore(client: RuntimeClient | RuntimeSourceFactory, initialRoute: AppRoute): AppStore {
  return new AppStore(client, initialRoute);
}
