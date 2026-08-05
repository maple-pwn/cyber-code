import {
  FetchRemoteTransport,
  LocalEventSource,
  RemoteEventSource,
  RuntimeSourceFactory,
  type AccessTokenProvider,
} from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

import { createWebLoopbackTransport, type WebLoopbackBridge } from './loopback-transport';

export type WebRuntimeConfiguration = {
  loopbackBridge?: WebLoopbackBridge;
  remote?: { endpoint: string; tokenProvider: AccessTokenProvider };
  demoSpeedMs?: number;
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

export function readWebRuntimeConfiguration(host: unknown = globalThis): WebRuntimeConfiguration {
  if (!isRecord(host) || !isRecord(host.__CYBER_RUNTIME_CONFIG__)) return {};
  const configured = host.__CYBER_RUNTIME_CONFIG__;
  const result: WebRuntimeConfiguration = {};
  if (isRecord(configured.remote)
    && typeof configured.remote.endpoint === 'string'
    && typeof configured.remote.tokenProvider === 'function') {
    result.remote = {
      endpoint: configured.remote.endpoint,
      tokenProvider: configured.remote.tokenProvider as AccessTokenProvider,
    };
  }
  const bridge = configured.loopbackBridge;
  if (isRecord(bridge)
    && ['start', 'request', 'restart', 'stop'].every((key) => typeof bridge[key] === 'function')) {
    result.loopbackBridge = bridge as unknown as WebLoopbackBridge;
  }
  return result;
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

export function createWebSourceFactory(
  configuration: WebRuntimeConfiguration = {},
): RuntimeSourceFactory {
  const localAvailable = configuration.loopbackBridge !== undefined;
  const remoteAvailable = isSecureRemoteEndpoint(configuration.remote?.endpoint);
  return new RuntimeSourceFactory([
    {
      id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic', 'demo-only'],
      available: true,
      create: () => new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: configuration.demoSpeedMs ?? 80 }),
    },
    {
      id: 'local', mode: 'local', label: 'Local', capabilities: ['real-runtime', 'loopback'],
      available: localAvailable,
      ...(!localAvailable ? { setupStatus: 'Open CYBER in a trusted host bridge to enable Local.' } : {}),
      create: () => new LocalEventSource(createWebLoopbackTransport(configuration.loopbackBridge)),
    },
    {
      id: 'remote', mode: 'remote', label: 'Remote', capabilities: ['real-runtime', 'managed'],
      available: remoteAvailable,
      ...(!remoteAvailable ? { setupStatus: 'Configure an HTTPS runtime endpoint and access token.' } : {}),
      create: () => {
        if (configuration.remote === undefined) throw new Error('runtime_source_unavailable:remote');
        return new RemoteEventSource(new FetchRemoteTransport(
          configuration.remote.endpoint,
          configuration.remote.tokenProvider,
        ));
      },
    },
  ]);
}
