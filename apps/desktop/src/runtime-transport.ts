import { FetchCyberAgentTransport, type CyberAgentRequest, type CyberAgentTransport, type LocalRequest, type LocalTransport } from '@cyber/runtime-client';

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
export type DesktopCyberAgentBridge = Partial<Pick<ReturnType<typeof createNativeClient>, 'cyberAgentStart' | 'cyberAgentStop'>>;

export function createDesktopCyberAgentTransport(
  bridge: DesktopCyberAgentBridge = createNativeClient(),
): CyberAgentTransport {
  let transport: FetchCyberAgentTransport | undefined;
  let starting: Promise<FetchCyberAgentTransport> | undefined;
  const ensure = async (): Promise<FetchCyberAgentTransport> => {
    if (bridge.cyberAgentStart === undefined) throw new Error('cyber_agent_supervisor_unavailable');
    if (transport) return transport;
    starting ??= bridge.cyberAgentStart().then((ready) => {
      transport = new FetchCyberAgentTransport(ready.endpoint, () => ready.token, {
        allowInsecureLoopback: true,
        eventMode: 'poll',
      });
      return transport;
    }).finally(() => { starting = undefined; });
    return starting;
  };
  return {
    request: async (request: CyberAgentRequest) => (await ensure()).request(request),
    events: async function* (sessionId, afterSequence, signal) { yield* (await ensure()).events(sessionId, afterSequence, signal); },
    close: async () => {
      await transport?.close();
      transport = undefined;
      await bridge.cyberAgentStop?.();
    },
  };
}

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
