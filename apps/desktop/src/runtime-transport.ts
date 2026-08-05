import type { LocalRequest, LocalTransport } from '@cyber/runtime-client';

import { createNativeClient } from './native';

export type DesktopRuntimeBridge = Pick<
  ReturnType<typeof createNativeClient>,
  'runtimeStart' | 'runtimeRequest' | 'runtimeRestart' | 'runtimeStop'
>;

export function createDesktopRuntimeTransport(
  bridge: DesktopRuntimeBridge = createNativeClient(),
): LocalTransport {
  return {
    start: () => bridge.runtimeStart(),
    request: (request: LocalRequest) => bridge.runtimeRequest(request),
    restart: () => bridge.runtimeRestart(),
    stop: () => bridge.runtimeStop(),
  };
}
