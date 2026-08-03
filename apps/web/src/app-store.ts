import type { RuntimeClient, RuntimeCommand, RuntimeView, Unsubscribe } from '@cyber/runtime-client';

export type AppRoute = 'new-task' | 'scope-review' | 'mission-control' | 'findings' | 'reports';
export type AppSnapshot = { route: AppRoute; view: RuntimeView };

export class AppStore {
  private snapshot: AppSnapshot;
  private readonly listeners = new Set<() => void>();
  private readonly unsubscribeClient: Unsubscribe;

  constructor(private readonly client: RuntimeClient, initialRoute: AppRoute) {
    this.snapshot = { route: initialRoute, view: client.getView() };
    this.unsubscribeClient = client.subscribe((view) => {
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

  async dispatch(command: RuntimeCommand): Promise<void> {
    await this.client.dispatch(command);
  }

  connect(): Promise<void> { return this.client.connect(); }
  reconnect(): Promise<void> { return this.client.reconnect(); }
  disconnect(): Promise<void> { return this.client.disconnect(); }

  destroy(): void {
    this.unsubscribeClient();
    this.listeners.clear();
  }

  private emit(): void {
    for (const listener of this.listeners) listener();
  }
}

export function createAppStore(client: RuntimeClient, initialRoute: AppRoute): AppStore {
  return new AppStore(client, initialRoute);
}
