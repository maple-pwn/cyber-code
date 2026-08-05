import { describe, expect, test, vi } from 'vitest';

import type { LocalRequest } from '@cyber/runtime-client';

import { createDesktopRuntimeTransport } from './runtime-transport';

describe('desktop runtime transport', () => {
  test('delegates only the four local runtime lifecycle operations', async () => {
    const native = {
      runtimeStart: vi.fn().mockResolvedValue({ id: 'start', type: 'handshake' }),
      runtimeRequest: vi.fn().mockImplementation(async (request: LocalRequest) => ({
        id: request.id,
        type: 'events',
        events: [],
      })),
      runtimeRestart: vi.fn().mockResolvedValue({ id: 'restart', type: 'handshake' }),
      runtimeStop: vi.fn().mockResolvedValue({ stopped: true }),
    };
    const transport = createDesktopRuntimeTransport(native);
    const request: LocalRequest = { id: 'local-1', type: 'events', afterCursor: 4 };

    await expect(transport.start()).resolves.toMatchObject({ id: 'start' });
    await expect(transport.request(request)).resolves.toEqual({ id: 'local-1', type: 'events', events: [] });
    await expect(transport.restart()).resolves.toMatchObject({ id: 'restart' });
    await expect(transport.stop()).resolves.toEqual({ stopped: true });
    expect(native.runtimeRequest).toHaveBeenCalledWith(request);
  });
});
