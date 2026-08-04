import { createAppStore, type AppRoute, type RuntimeOption } from '@cyber/product-app';
import { RuntimeClient } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

export const desktopRuntimes: readonly RuntimeOption[] = [
  { id: 'scenario-local', label: 'Demo', capabilities: ['deterministic', 'demo-only'] },
];
const desktopRouteKey = 'cyber.desktop.route.v1';
const desktopRoutes: readonly AppRoute[] = ['new-task', 'scope-review', 'mission-control', 'findings', 'reports'];

function restoredDesktopRoute(): AppRoute {
  try {
    const route = window.localStorage.getItem(desktopRouteKey);
    return desktopRoutes.find((candidate) => candidate === route) ?? 'new-task';
  } catch {
    return 'new-task';
  }
}

export function createDesktopBootstrap(options: { speedMs?: number } = {}) {
  const source = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: options.speedMs ?? 80 });
  const store = createAppStore(new RuntimeClient(source), restoredDesktopRoute());
  store.subscribe(() => {
    try {
      window.localStorage.setItem(desktopRouteKey, store.getSnapshot().route);
    } catch {
      // A disabled storage backend must not prevent the desktop shell from running.
    }
  });
  return { source, store, runtimes: desktopRuntimes };
}
