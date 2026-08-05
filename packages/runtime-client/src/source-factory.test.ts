import { describe, expect, test, vi } from 'vitest';

import type { EventSource } from './index';
import { RuntimeSourceFactory } from './source-factory';

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
    ]);

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
});
