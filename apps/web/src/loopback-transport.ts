import type { LocalRequest, LocalTransport } from '@cyber/runtime-client';

export type WebLoopbackBridge = LocalTransport;

const unavailable = async (): Promise<never> => {
  throw new Error('local_loopback_unavailable');
};

export function createWebLoopbackTransport(
  bridge: WebLoopbackBridge | undefined,
): LocalTransport {
  if (bridge === undefined) {
    return {
      start: unavailable,
      request: unavailable,
      restart: unavailable,
      stop: unavailable,
    };
  }
  return {
    start: () => bridge.start(),
    request: (request: LocalRequest) => {
      if (Object.hasOwn(request, 'bearer')) throw new Error('invalid_loopback_request');
      return bridge.request(structuredClone(request));
    },
    restart: () => bridge.restart(),
    stop: () => bridge.stop(),
  };
}
