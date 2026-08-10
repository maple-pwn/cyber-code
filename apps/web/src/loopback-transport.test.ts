import { describe, expect, test, vi } from 'vitest';

import type { LocalRequest } from '@cyber/runtime-client';

import { createWebLoopbackTransport } from './loopback-transport';

describe('Web loopback runtime transport', () => {
  test('stays explicitly unavailable without a trusted host bridge', async () => {
    const transport = createWebLoopbackTransport(undefined);

    await expect(transport.start()).rejects.toThrow('local_loopback_unavailable');
    await expect(transport.request({ id: 'local-1', type: 'health' })).rejects
      .toThrow('local_loopback_unavailable');
  });

  test('delegates credential-free requests without writing browser storage', async () => {
    const bridge = {
      start: vi.fn().mockResolvedValue({ id: 'start', type: 'handshake' }),
      request: vi.fn().mockImplementation(async (request: LocalRequest) => ({
        id: request.id,
        type: 'health',
        ready: true,
      })),
      restart: vi.fn().mockResolvedValue({ id: 'restart', type: 'handshake' }),
      stop: vi.fn().mockResolvedValue({ stopped: true }),
    };
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    const transport = createWebLoopbackTransport(bridge);
    const request: LocalRequest = { id: 'local-1', type: 'health' };

    await transport.start();
    await expect(transport.request(request)).resolves.toEqual({
      id: 'local-1',
      type: 'health',
      ready: true,
    });
    await transport.restart();
    await transport.stop();

    expect(bridge.request).toHaveBeenCalledWith(request);
    expect(setItem).not.toHaveBeenCalled();
    expect(JSON.stringify(bridge.request.mock.calls)).not.toContain('bearer');
    setItem.mockRestore();
  });
});
