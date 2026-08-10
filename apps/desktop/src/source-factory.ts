import {
  CyberAgentEventSource,
  FetchRemoteTransport,
  LocalEventSource,
  RemoteEventSource,
  RuntimeSourceFactory,
} from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

import { createNativeClient } from './native';
import {
  createDesktopRemoteTokenProvider,
  createDesktopCyberAgentTransport,
  createDesktopRuntimeTransport,
  type DesktopCredentialBridge,
  type DesktopCyberAgentBridge,
  type DesktopRuntimeBridge,
} from './runtime-transport';

export type DesktopSourceBridge = DesktopRuntimeBridge & DesktopCredentialBridge & DesktopCyberAgentBridge;
export type DesktopRuntimeConfiguration = {
  realSourcesEnabled?: boolean;
  bridge?: DesktopSourceBridge;
  remote?: { endpoint: string; credentialId: string };
  demoSpeedMs?: number;
};

export function readDesktopRuntimeConfiguration(
  environment: Record<string, string | undefined> = (import.meta as ImportMeta & {
    env: Record<string, string | undefined>;
  }).env,
): DesktopRuntimeConfiguration {
  const endpoint = environment.VITE_CYBER_REMOTE_ENDPOINT?.trim();
  const credentialId = environment.VITE_CYBER_REMOTE_CREDENTIAL_ID?.trim();
  const realSourcesEnabled = environment.VITE_CYBER_REAL_SOURCES_ENABLED?.trim() === 'true';
  return {
    ...(realSourcesEnabled ? { realSourcesEnabled: true } : {}),
    ...(!endpoint || !credentialId ? {} : { remote: { endpoint, credentialId } }),
  };
}

const isSecureRemoteEndpoint = (endpoint: string | undefined): boolean => {
  if (endpoint === undefined) return false;
  try {
    const parsed = new URL(endpoint);
    return parsed.protocol === 'https:' && !parsed.username && !parsed.password && !parsed.hash;
  } catch {
    return false;
  }
};

export function createDesktopSourceFactory(
  configuration: DesktopRuntimeConfiguration = {},
): RuntimeSourceFactory {
  const bridge = configuration.bridge ?? createNativeClient();
  const remoteAvailable = isSecureRemoteEndpoint(configuration.remote?.endpoint)
    && Boolean(configuration.remote?.credentialId.trim());
  const cyberAgentAvailable = typeof bridge.cyberAgentStart === 'function'
    && typeof bridge.cyberAgentStop === 'function';
  return new RuntimeSourceFactory([
    {
      id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic', 'demo-only'],
      available: true,
      create: () => new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: configuration.demoSpeedMs ?? 80 }),
    },
    {
      id: 'local', mode: 'local', label: 'Local', capabilities: ['real-runtime', 'native'],
      available: true,
      create: () => new LocalEventSource(createDesktopRuntimeTransport(bridge)),
    },
    {
      id: 'remote', mode: 'remote', label: 'Remote', capabilities: ['real-runtime', 'managed'],
      available: remoteAvailable,
      ...(!remoteAvailable ? { setupStatus: 'Configure a remote HTTPS endpoint and credential in the OS keychain.' } : {}),
      create: () => {
        if (configuration.remote === undefined) throw new Error('runtime_source_unavailable:remote');
        return new RemoteEventSource(new FetchRemoteTransport(
          configuration.remote.endpoint,
          createDesktopRemoteTokenProvider(configuration.remote.credentialId, bridge),
        ));
      },
    },
    {
      id: 'cyber-agent', mode: 'local', label: 'Security Runtime - cyber-agent',
      capabilities: ['security-runtime', 'session.events.v1', 'skills.lifecycle.v1'],
      available: cyberAgentAvailable,
      ...(!cyberAgentAvailable ? { setupStatus: 'The native cyber-agent supervisor is unavailable in this build.' } : {}),
      create: () => {
        if (!cyberAgentAvailable) throw new Error('runtime_source_unavailable:cyber-agent');
        return new CyberAgentEventSource(
          createDesktopCyberAgentTransport(bridge), 'cyber-agent-local', 'local',
        );
      },
    },
  ], { realSourcesEnabled: configuration.realSourcesEnabled });
}
