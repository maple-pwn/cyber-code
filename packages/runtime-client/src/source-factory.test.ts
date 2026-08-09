import { describe, expect, test, vi } from 'vitest';

import type { EventSource } from './index';
import { RuntimeSourceFactory } from './source-factory';
import { cyberAgentSourceDefinition } from './source-factory';

const source = {} as EventSource;

describe('RuntimeSourceFactory', () => {
  test('lists honest availability and rejects unavailable sources before creation', () => {
    const createDemo = vi.fn(() => source);
    const factory = new RuntimeSourceFactory([
      {
        id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic'],
        available: true, create: createDemo,
      },
      {
        id: 'local', mode: 'local', label: 'Local', capabilities: ['real-runtime'],
        available: false, setupStatus: 'Install or configure the cyber-code runtime.',
        create: vi.fn(() => source),
      },
    ], { realSourcesEnabled: true });

    expect(factory.options()).toEqual([
      expect.objectContaining({ id: 'demo', mode: 'demo', available: true }),
      expect.objectContaining({
        id: 'local', mode: 'local', available: false,
        setupStatus: 'Install or configure the cyber-code runtime.',
      }),
    ]);
    expect(factory.create('demo')).toBe(source);
    expect(createDemo).toHaveBeenCalledOnce();
    expect(() => factory.create('local')).toThrow('runtime_source_unavailable:local');
  });

  test('requires unique definitions and an available default source', () => {
    expect(() => new RuntimeSourceFactory([])).toThrow('runtime_source_default_unavailable');
    expect(() => new RuntimeSourceFactory([
      { id: 'demo', mode: 'demo', label: 'Demo', capabilities: [], available: true, create: () => source },
      { id: 'demo', mode: 'local', label: 'Local', capabilities: [], available: true, create: () => source },
    ])).toThrow('duplicate_runtime_source:demo');
  });

  test('gates real sources behind an explicit capability flag while keeping Demo available', () => {
    const definitions = [
      { id: 'demo', mode: 'demo' as const, label: 'Demo', capabilities: [], available: true, create: () => source },
      { id: 'local', mode: 'local' as const, label: 'Local', capabilities: ['real-runtime'], available: true, create: () => source },
    ];

    const gated = new RuntimeSourceFactory(definitions, { realSourcesEnabled: false });
    expect(gated.options()).toEqual([
      expect.objectContaining({ id: 'demo', available: true }),
      expect.objectContaining({ id: 'local', available: false, setupStatus: expect.stringContaining('capability') }),
    ]);
    expect(() => gated.create('local')).toThrow('runtime_source_capability_disabled:local');

    const enabled = new RuntimeSourceFactory(definitions, { realSourcesEnabled: true });
    expect(enabled.options()[1]).toMatchObject({ id: 'local', available: true });
  });

  test('registers cyber-agent as an explicit remote source without a Demo fallback', () => {
    const transport = { request: vi.fn(), events: async function* () { yield undefined; } };
    const definition = cyberAgentSourceDefinition(transport);
    const factory = new RuntimeSourceFactory([
      { id: 'demo', mode: 'demo', label: 'Demo', capabilities: [], available: true, create: () => source },
      definition,
    ], { realSourcesEnabled: true });
    expect(factory.options()[1]).toMatchObject({ id: 'cyber-agent', mode: 'remote', available: true });
    expect(factory.create('cyber-agent')).not.toBe(source);
  });
});
