import { createAppStore, type RuntimeOption } from '@cyber/product-app';
import { RuntimeClient } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

export const desktopRuntimes: readonly RuntimeOption[] = [
  { id: 'scenario-local', label: 'Demo', capabilities: ['deterministic', 'demo-only'] },
];

export function createDesktopBootstrap(options: { speedMs?: number } = {}) {
  const source = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: options.speedMs ?? 80 });
  const store = createAppStore(new RuntimeClient(source), 'new-task');
  return { source, store, runtimes: desktopRuntimes };
}
