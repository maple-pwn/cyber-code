import { describe, expect, test, vi } from 'vitest';

import { RuntimeClient } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

import { createAppStore } from './app-store';

describe('AppStore', () => {
  test('projects runtime snapshots and exposes explicit navigation', async () => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    const client = new RuntimeClient(player);
    const store = createAppStore(client, 'new-task');
    const listener = vi.fn();
    store.subscribe(listener);

    await store.connect();
    await store.dispatch({
      type: 'task.create', objective: '评估 juice-shop.lab', runtimeId: 'scenario-local',
    });
    store.navigate('scope-review');

    expect(store.getSnapshot().route).toBe('scope-review');
    expect(store.getSnapshot().view.product.scope?.targets).toContain('juice-shop.lab');
    expect(listener).toHaveBeenCalled();
  });
});
