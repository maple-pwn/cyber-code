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

export type DesktopCredentialBridge = Pick<ReturnType<typeof createNativeClient>, 'loadSecret'>;

export function createDesktopRemoteTokenProvider(
  credentialId: string,
  bridge: DesktopCredentialBridge = createNativeClient(),
): () => Promise<string> {
  if (!credentialId.trim()) throw new Error('invalid_remote_credential_id');
  return async () => {
    const credential = await bridge.loadSecret(credentialId);
    return credential.secret;
  };
}
