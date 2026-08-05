import { describe, expect, test, vi } from 'vitest';

import { LocalEventSource, RemoteEventSource } from '@cyber/runtime-client';

import { createWebSourceFactory, readWebRuntimeConfiguration } from './source-factory';

describe('Web runtime source factory', () => {
  test('reads remote configuration from the trusted host without browser persistence', () => {
    const tokenProvider = vi.fn(() => 'memory-only-token');
    const configuration = readWebRuntimeConfiguration({
      __CYBER_RUNTIME_CONFIG__: {
        realSourcesEnabled: true,
        remote: { endpoint: 'https://runtime.example.test/v1/runtime', tokenProvider },
      },
    });

    expect(configuration.remote).toEqual({
      endpoint: 'https://runtime.example.test/v1/runtime', tokenProvider,
    });
    expect(configuration.realSourcesEnabled).toBe(true);
    expect(createWebSourceFactory(configuration).options().find((option) => option.id === 'remote'))
      .toMatchObject({ available: true });
  });

  test('keeps real sources disabled with actionable setup status without trusted configuration', () => {
    const factory = createWebSourceFactory({ realSourcesEnabled: true });

    expect(factory.options()).toEqual([
      expect.objectContaining({ id: 'demo', mode: 'demo', available: true }),
      expect.objectContaining({ id: 'local', mode: 'local', available: false, setupStatus: expect.stringContaining('host bridge') }),
      expect.objectContaining({ id: 'remote', mode: 'remote', available: false, setupStatus: expect.stringContaining('HTTPS') }),
    ]);
  });

  test('keeps an insecure remote endpoint disabled before source creation', () => {
    const factory = createWebSourceFactory({
      realSourcesEnabled: true,
      remote: { endpoint: 'http://runtime.example.test/v1/runtime', tokenProvider: () => 'token' },
    });

    expect(factory.options().find((option) => option.id === 'remote')).toMatchObject({
      available: false,
      setupStatus: expect.stringContaining('HTTPS'),
    });
    expect(() => factory.create('remote')).toThrow('runtime_source_unavailable:remote');
  });

  test('constructs configured local and remote sources without persisting browser credentials', () => {
    const bridge = {
      start: vi.fn().mockResolvedValue({}), request: vi.fn().mockResolvedValue({}),
      restart: vi.fn().mockResolvedValue({}), stop: vi.fn().mockResolvedValue({}),
    };
    const tokenProvider = vi.fn(() => 'memory-only-token');
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    const factory = createWebSourceFactory({
      realSourcesEnabled: true,
      loopbackBridge: bridge,
      remote: { endpoint: 'https://runtime.example.test/v1/runtime', tokenProvider },
    });

    expect(factory.create('local')).toBeInstanceOf(LocalEventSource);
    expect(factory.create('remote')).toBeInstanceOf(RemoteEventSource);
    expect(setItem).not.toHaveBeenCalled();
    setItem.mockRestore();
  });

  test('does not expose configured real transports until the trusted capability flag is enabled', () => {
    const factory = createWebSourceFactory({
      loopbackBridge: {
        start: vi.fn(), request: vi.fn(), restart: vi.fn(), stop: vi.fn(),
      },
      remote: { endpoint: 'https://runtime.example.test/v1/runtime', tokenProvider: () => 'token' },
    });

    expect(factory.options().filter((option) => option.mode !== 'demo')).toEqual([
      expect.objectContaining({ id: 'local', available: false, setupStatus: expect.stringContaining('capability') }),
      expect.objectContaining({ id: 'remote', available: false, setupStatus: expect.stringContaining('capability') }),
    ]);
  });

  test('keeps Demo execution deterministic across factory-created sessions', async () => {
    const run = async (): Promise<unknown[]> => {
      const source = createWebSourceFactory({ demoSpeedMs: 0 }).create('demo');
      const handshake = await source.handshake({ supportedProtocolVersions: [1], afterCursor: 0 });
      await source.send({
        idempotencyKey: 'deterministic-create',
        command: { type: 'task.create', objective: 'Inspect juice-shop.lab', runtimeId: handshake.runtimeId },
      });
      const events: unknown[] = [];
      const unsubscribe = await source.subscribe(0, (event) => events.push(event));
      unsubscribe();
      await source.close();
      return events;
    };

    expect(await run()).toEqual(await run());
  });
});
