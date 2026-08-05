import { describe, expect, test, vi } from 'vitest';

import { RuntimeClient } from '@cyber/runtime-client';
import { RuntimeSourceFactory } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

import { createAppStore } from './app-store';

test('switches the actual event source before creating a task', async () => {
  const demo = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  const alternate = new ScenarioPlayer({ runtimeId: 'scenario-remote', speedMs: 0 });
  const factory = new RuntimeSourceFactory([
    { id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic'], available: true, create: () => demo },
    { id: 'alternate-demo', mode: 'demo', label: 'Alternate Demo', capabilities: ['deterministic'], available: true, create: () => alternate },
  ]);
  const store = createAppStore(factory, 'new-task');
  await store.connect();

  await store.dispatch({ type: 'task.create', objective: 'Inspect juice-shop.lab', runtimeId: 'alternate-demo' });

  expect(demo.events()).toHaveLength(0);
  expect(alternate.events().map((event) => event.type)).toEqual(['task.created', 'scope.proposed']);
  expect(store.getSnapshot().view.source).toMatchObject({ mode: 'demo', runtimeId: 'scenario-remote' });
  store.destroy();
});

test('rejects a source whose trusted handshake mode differs from its factory definition', async () => {
  const disguisedDemo = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  const factory = new RuntimeSourceFactory([
    { id: 'local', mode: 'local', label: 'Local', capabilities: ['real-runtime'], available: true, create: () => disguisedDemo },
  ]);
  const store = createAppStore(factory, 'new-task');

  await expect(store.connect()).rejects.toThrow('runtime_source_mode_mismatch:local:demo');
  expect(disguisedDemo.events()).toHaveLength(0);
  store.destroy();
});

test('keeps the connected source when a replacement cannot be created', async () => {
  const demo = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  const close = vi.spyOn(demo, 'close');
  const factory = new RuntimeSourceFactory([
    { id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic'], available: true, create: () => demo },
    {
      id: 'broken-local', mode: 'local', label: 'Broken Local', capabilities: ['real-runtime'], available: true,
      create: () => { throw new Error('local_runtime_start_failed'); },
    },
  ]);
  const store = createAppStore(factory, 'new-task');
  await store.connect();

  await expect(store.dispatch({
    type: 'task.create', objective: 'Inspect local target', runtimeId: 'broken-local',
  })).rejects.toThrow('local_runtime_start_failed');

  expect(close).not.toHaveBeenCalled();
  expect(store.getSnapshot().view.connection.status).toBe('healthy');
  await store.dispatch({ type: 'task.create', objective: 'Inspect juice-shop.lab', runtimeId: 'demo' });
  expect(demo.events().map((event) => event.type)).toEqual(['task.created', 'scope.proposed']);
  store.destroy();
});

test('keeps the connected source when a replacement cannot establish a trusted handshake', async () => {
  const demo = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  const close = vi.spyOn(demo, 'close');
  const broken = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  vi.spyOn(broken, 'handshake').mockRejectedValue(new Error('local_transport_unavailable'));
  const factory = new RuntimeSourceFactory([
    { id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic'], available: true, create: () => demo },
    { id: 'broken-local', mode: 'local', label: 'Broken Local', capabilities: ['real-runtime'], available: true, create: () => broken },
  ]);
  const store = createAppStore(factory, 'new-task');
  await store.connect();

  await expect(store.dispatch({
    type: 'task.create', objective: 'Inspect local target', runtimeId: 'broken-local',
  })).rejects.toThrow('runtime_source_connect_failed:degraded:local_transport_unavailable');

  expect(close).not.toHaveBeenCalled();
  expect(store.getSnapshot().view.connection.status).toBe('healthy');
  store.destroy();
});

test('serializes concurrent task creation across runtime boundaries', async () => {
  let releaseLocal!: () => void;
  const localReady = new Promise<void>((resolve) => { releaseLocal = resolve; });
  const demo = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  const local = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  const remote = new ScenarioPlayer({ runtimeId: 'scenario-remote', speedMs: 0 });
  const localHandshake = local.handshake.bind(local);
  const handshake = vi.spyOn(local, 'handshake').mockImplementation(async (request) => {
    await localReady;
    return localHandshake(request);
  });
  const factory = new RuntimeSourceFactory([
    { id: 'demo', mode: 'demo', label: 'Demo', capabilities: [], available: true, create: () => demo },
    { id: 'local', mode: 'demo', label: 'Local', capabilities: [], available: true, create: () => local },
    { id: 'remote', mode: 'demo', label: 'Remote', capabilities: [], available: true, create: () => remote },
  ]);
  const store = createAppStore(factory, 'new-task');
  await store.connect();

  const localTask = store.dispatch({ type: 'task.create', objective: 'Inspect juice-shop.lab', runtimeId: 'local' });
  await vi.waitFor(() => expect(handshake).toHaveBeenCalled());
  const remoteTask = store.dispatch({ type: 'task.create', objective: 'Inspect juice-shop.lab', runtimeId: 'remote' });
  releaseLocal();
  await Promise.all([localTask, remoteTask]);

  expect(local.events().map((event) => event.type)).toEqual(['task.created', 'scope.proposed']);
  expect(remote.events().map((event) => event.type)).toEqual(['task.created', 'scope.proposed']);
  expect(store.getSnapshot().view.source?.runtimeId).toBe('scenario-remote');
  store.destroy();
});

test('creates a fresh task context when reusing the selected source', async () => {
  const players: ScenarioPlayer[] = [];
  const factory = new RuntimeSourceFactory([{
    id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic'], available: true,
    create: () => {
      const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
      players.push(player);
      return player;
    },
  }]);
  const store = createAppStore(factory, 'new-task');
  await store.connect();

  await store.dispatch({ type: 'task.create', objective: 'Inspect juice-shop.lab', runtimeId: 'demo' });
  await store.dispatch({ type: 'task.create', objective: 'Inspect juice-shop.lab', runtimeId: 'demo' });

  expect(players).toHaveLength(2);
  expect(players.map((player) => player.events().map((item) => item.type))).toEqual([
    ['task.created', 'scope.proposed'],
    ['task.created', 'scope.proposed'],
  ]);
  store.destroy();
});

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
