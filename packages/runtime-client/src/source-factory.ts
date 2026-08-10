import type { EventSource } from './index';
import type { RuntimeSourceMode } from './conformance';
import { CyberAgentEventSource, type CyberAgentTransport } from './cyber-agent-event-source';

export type RuntimeSourceOption = {
  id: string;
  mode: RuntimeSourceMode;
  label: string;
  capabilities: readonly string[];
  available: boolean;
  setupStatus?: string;
};

export type RuntimeSourceDefinition = RuntimeSourceOption & {
  create: () => EventSource;
};

export type RuntimeSourceFactoryOptions = {
  realSourcesEnabled?: boolean;
};

export function cyberAgentSourceDefinition(
  transport: CyberAgentTransport,
  options: { available?: boolean; setupStatus?: string; label?: string; mode?: RuntimeSourceMode } = {},
): RuntimeSourceDefinition {
  return {
    id: 'cyber-agent',
	mode: options.mode ?? 'remote',
    label: options.label ?? 'Security Runtime - cyber-agent',
    capabilities: ['security-runtime', 'session.events.v1', 'skills.lifecycle.v1'],
    available: options.available ?? true,
    ...(options.setupStatus === undefined ? {} : { setupStatus: options.setupStatus }),
	create: () => new CyberAgentEventSource(transport, `cyber-agent-${options.mode ?? 'remote'}`, options.mode ?? 'remote'),
  };
}

export class RuntimeSourceFactory {
  private readonly definitions = new Map<string, RuntimeSourceDefinition>();
  private readonly realSourcesEnabled: boolean;
  readonly defaultSourceId: string;

  constructor(
    definitions: readonly RuntimeSourceDefinition[],
    options: RuntimeSourceFactoryOptions = {},
  ) {
    this.realSourcesEnabled = options.realSourcesEnabled === true;
    for (const definition of definitions) {
      if (!definition.id.trim()) throw new Error('invalid_runtime_source_id');
      if (this.definitions.has(definition.id)) {
        throw new Error(`duplicate_runtime_source:${definition.id}`);
      }
      this.definitions.set(definition.id, definition);
    }
    const defaultSource = definitions.find((definition) => this.available(definition));
    if (defaultSource === undefined) throw new Error('runtime_source_default_unavailable');
    this.defaultSourceId = defaultSource.id;
  }

  options(): readonly RuntimeSourceOption[] {
    return [...this.definitions.values()].map((definition) => ({
      id: definition.id,
      mode: definition.mode,
      label: definition.label,
      capabilities: [...definition.capabilities],
      available: this.available(definition),
      ...(this.setupStatus(definition) === undefined ? {} : { setupStatus: this.setupStatus(definition) }),
    }));
  }

  create(id: string): EventSource {
    const definition = this.definitions.get(id);
    if (definition === undefined) throw new Error(`runtime_source_unknown:${id}`);
    if (definition.mode !== 'demo' && !this.realSourcesEnabled) {
      throw new Error(`runtime_source_capability_disabled:${id}`);
    }
    if (!definition.available) throw new Error(`runtime_source_unavailable:${id}`);
    return definition.create();
  }

  private available(definition: RuntimeSourceDefinition): boolean {
    return definition.available && (definition.mode === 'demo' || this.realSourcesEnabled);
  }

  private setupStatus(definition: RuntimeSourceDefinition): string | undefined {
    if (definition.mode !== 'demo' && !this.realSourcesEnabled) {
      return 'Enable the real runtime capability to use this source.';
    }
    return definition.setupStatus;
  }
}
