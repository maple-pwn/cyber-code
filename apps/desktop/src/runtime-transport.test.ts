import { describe, expect, test, vi } from 'vitest';

import type { LocalRequest } from '@cyber/runtime-client';

import { createDesktopCyberAgentTransport, createDesktopRemoteTokenProvider, createDesktopRuntimeTransport } from './runtime-transport';

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

test('resolves remote access tokens from the native credential service', async () => {
  const bridge = {
    loadSecret: vi.fn().mockResolvedValue({ id: 'remote-primary', secret: 'remote-token' }),
  };
  const provider = createDesktopRemoteTokenProvider('remote-primary', bridge);

  await expect(provider()).resolves.toBe('remote-token');
  expect(bridge.loadSecret).toHaveBeenCalledWith('remote-primary');
});

test('uses finite event polling for the desktop cyber-agent bridge', async () => {
  const fetch = vi.fn(async (input: string | URL | Request) => {
    const url = String(input);
    if (url.includes('/event-batch')) {
      return new Response(JSON.stringify({ events: [], has_more: false }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    }
    return new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fetch);
  try {
    const transport = createDesktopCyberAgentTransport({
      cyberAgentStart: vi.fn().mockResolvedValue({
        endpoint: 'http://127.0.0.1:43127', token: 'desktop-token', version: '0.1.0',
      }),
      cyberAgentStop: vi.fn().mockResolvedValue({ stopped: true }),
    });
    const controller = new AbortController();
    const iterator = transport.events('session-desktop', 3, controller.signal)[Symbol.asyncIterator]();
    const pending = iterator.next();
    await vi.waitFor(() => expect(fetch).toHaveBeenCalled());
    controller.abort();
    await pending;

    expect(fetch.mock.calls[0]?.[0].toString()).toContain('/event-batch?after_sequence=3');
  } finally {
    vi.unstubAllGlobals();
  }
});
