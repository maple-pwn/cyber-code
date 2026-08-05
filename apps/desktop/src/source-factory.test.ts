import { expect, test, vi } from 'vitest';

import { LocalEventSource, RemoteEventSource } from '@cyber/runtime-client';

import { createDesktopSourceFactory, readDesktopRuntimeConfiguration } from './source-factory';

test('reads the shipped desktop remote endpoint and credential ID from environment configuration', () => {
  expect(readDesktopRuntimeConfiguration({
    VITE_CYBER_REAL_SOURCES_ENABLED: 'true',
    VITE_CYBER_REMOTE_ENDPOINT: 'https://runtime.example.test/v1/runtime',
    VITE_CYBER_REMOTE_CREDENTIAL_ID: 'cyber-remote-production',
  })).toEqual({
    realSourcesEnabled: true,
    remote: {
      endpoint: 'https://runtime.example.test/v1/runtime', credentialId: 'cyber-remote-production',
    },
  });
});

test('desktop source factory exposes native Local and configured Remote honestly', () => {
  const bridge = {
    runtimeStart: vi.fn().mockResolvedValue({}), runtimeRequest: vi.fn().mockResolvedValue({}),
    runtimeRestart: vi.fn().mockResolvedValue({}), runtimeStop: vi.fn().mockResolvedValue({ stopped: true }),
    loadSecret: vi.fn().mockResolvedValue({ id: 'remote-token', secret: 'secret' }),
  };
  const factory = createDesktopSourceFactory({
    realSourcesEnabled: true,
    bridge,
    remote: {
      endpoint: 'https://runtime.example.test/v1/runtime', credentialId: 'remote-token',
    },
  });

  expect(factory.options()).toEqual([
    expect.objectContaining({ id: 'demo', mode: 'demo', available: true }),
    expect.objectContaining({ id: 'local', mode: 'local', available: true }),
    expect.objectContaining({ id: 'remote', mode: 'remote', available: true }),
  ]);
  expect(factory.create('local')).toBeInstanceOf(LocalEventSource);
  expect(factory.create('remote')).toBeInstanceOf(RemoteEventSource);
});

test('desktop keeps Local and Remote capability-gated by default', () => {
  const factory = createDesktopSourceFactory({
    bridge: {
      runtimeStart: vi.fn(), runtimeRequest: vi.fn(), runtimeRestart: vi.fn(), runtimeStop: vi.fn(),
      loadSecret: vi.fn(),
    },
    remote: { endpoint: 'https://runtime.example.test/v1/runtime', credentialId: 'remote-token' },
  });

  expect(factory.options().filter((option) => option.mode !== 'demo')).toEqual([
    expect.objectContaining({ id: 'local', available: false, setupStatus: expect.stringContaining('capability') }),
    expect.objectContaining({ id: 'remote', available: false, setupStatus: expect.stringContaining('capability') }),
  ]);
});

test('desktop source factory disables Remote until endpoint and credential are configured', () => {
  const factory = createDesktopSourceFactory({
    realSourcesEnabled: true,
    bridge: {
      runtimeStart: vi.fn(), runtimeRequest: vi.fn(), runtimeRestart: vi.fn(), runtimeStop: vi.fn(),
      loadSecret: vi.fn(),
    },
  });

  expect(factory.options().find((option) => option.id === 'remote')).toMatchObject({
    available: false,
    setupStatus: expect.stringContaining('credential'),
  });
});

test('desktop source factory disables Remote when its configured endpoint is not HTTPS', () => {
  const factory = createDesktopSourceFactory({
    realSourcesEnabled: true,
    bridge: {
      runtimeStart: vi.fn(), runtimeRequest: vi.fn(), runtimeRestart: vi.fn(), runtimeStop: vi.fn(),
      loadSecret: vi.fn(),
    },
    remote: { endpoint: 'http://runtime.example.test/v1/runtime', credentialId: 'remote-token' },
  });

  expect(factory.options().find((option) => option.id === 'remote')).toMatchObject({
    available: false,
    setupStatus: expect.stringContaining('HTTPS'),
  });
  expect(() => factory.create('remote')).toThrow('runtime_source_unavailable:remote');
});
