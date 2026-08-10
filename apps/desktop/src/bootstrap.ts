import { createAppStore, type AppRoute } from '@cyber/product-app';

import {
  createDesktopSourceFactory,
  readDesktopRuntimeConfiguration,
  type DesktopRuntimeConfiguration,
} from './source-factory';
const desktopRouteKey = 'cyber.desktop.route.v1';
const restorableDesktopRoutes: readonly AppRoute[] = ['new-task', 'scope-review', 'mission-control', 'findings', 'reports'];

function restoredDesktopRoute(): AppRoute {
  try {
    const route = window.localStorage.getItem(desktopRouteKey);
    if (route === 'editor') return 'findings';
    return restorableDesktopRoutes.find((candidate) => candidate === route) ?? 'new-task';
  } catch {
    return 'new-task';
  }
}

export function createDesktopBootstrap(options: { speedMs?: number; runtime?: DesktopRuntimeConfiguration } = {}) {
  const runtime = options.runtime ?? readDesktopRuntimeConfiguration();
  const sources = createDesktopSourceFactory({ ...runtime, demoSpeedMs: options.speedMs });
  const store = createAppStore(sources, restoredDesktopRoute());
  store.subscribe(() => {
    try {
      window.localStorage.setItem(desktopRouteKey, store.getSnapshot().route);
    } catch {
      // A disabled storage backend must not prevent the desktop shell from running.
    }
  });
  return { sources, store, runtimes: sources.options() };
}
