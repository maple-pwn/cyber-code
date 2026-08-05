import type { EventSource } from './index';
import type { RuntimeSourceMode } from './conformance';

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

export class RuntimeSourceFactory {
  private readonly definitions = new Map<string, RuntimeSourceDefinition>();
  readonly defaultSourceId: string;

  constructor(definitions: readonly RuntimeSourceDefinition[]) {
    for (const definition of definitions) {
      if (!definition.id.trim()) throw new Error('invalid_runtime_source_id');
      if (this.definitions.has(definition.id)) {
        throw new Error(`duplicate_runtime_source:${definition.id}`);
      }
      this.definitions.set(definition.id, definition);
    }
    const defaultSource = definitions.find((definition) => definition.available);
    if (defaultSource === undefined) throw new Error('runtime_source_default_unavailable');
    this.defaultSourceId = defaultSource.id;
  }

  options(): readonly RuntimeSourceOption[] {
    return [...this.definitions.values()].map((definition) => ({
      id: definition.id,
      mode: definition.mode,
      label: definition.label,
      capabilities: [...definition.capabilities],
      available: definition.available,
      ...(definition.setupStatus === undefined ? {} : { setupStatus: definition.setupStatus }),
    }));
  }

  create(id: string): EventSource {
    const definition = this.definitions.get(id);
    if (definition === undefined) throw new Error(`runtime_source_unknown:${id}`);
    if (!definition.available) throw new Error(`runtime_source_unavailable:${id}`);
    return definition.create();
  }
}
