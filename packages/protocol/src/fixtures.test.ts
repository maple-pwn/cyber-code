import { createHash } from 'node:crypto';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, test } from 'vitest';

import {
  initialProductState,
  project,
  validateEvent,
  type JsonObject,
  type ProductState,
  type ProjectionResult,
  type RawProductEvent,
} from './index';

type Fixture = {
  name: string;
  events: RawProductEvent[];
};

type ExpectedTerminalState = {
  committedCursor: number;
  taskStatus: string | null;
  timelineLength: number;
  rawEventCount: number;
};

type ManifestEntry = {
  name: string;
  file: string;
  schemaVersion: 1;
  expectedOutcomes: string[];
  expectedTerminalState: ExpectedTerminalState;
  fixtureSha256: string;
  stateSha256: string;
};

type FixtureManifest = {
  schemaVersion: 1;
  digestAlgorithm: 'sha256-canonical-json-v1';
  fixtures: ManifestEntry[];
};

const fixtureRoot = resolve(process.cwd(), 'tests/fixtures/product-events');

const canonicalJson = (value: unknown): string => JSON.stringify(value, (_key, item: unknown) => (
  item && typeof item === 'object' && !Array.isArray(item)
    ? Object.fromEntries(Object.entries(item as JsonObject).sort(([left], [right]) => (
      left < right ? -1 : left > right ? 1 : 0
    )))
    : item
));

const sha256 = (value: string): string => createHash('sha256').update(value).digest('hex');

const summarize = (state: ProductState): ExpectedTerminalState => ({
  committedCursor: state.committedCursor,
  taskStatus: state.task?.status ?? null,
  timelineLength: state.timeline.length,
  rawEventCount: state.rawEvents.length,
});

const outcome = (result: ProjectionResult): string => {
  if (result.kind === 'resync-required') return `resync-required:${result.expectedCursor}`;
  return result.kind;
};

const projectFixture = (events: RawProductEvent[]) => {
  let state = initialProductState();
  const outcomes: string[] = [];

  for (const raw of events) {
    const event = validateEvent(raw);
    try {
      const result = project(state, event);
      outcomes.push(outcome(result));
      state = result.state;
    } catch (error) {
      outcomes.push(`error:${error instanceof Error ? error.message : 'unknown_error'}`);
    }
  }

  return { state, outcomes };
};

describe('cross-language product event fixtures', () => {
  const manifest = JSON.parse(readFileSync(resolve(fixtureRoot, 'manifest.json'), 'utf8')) as FixtureManifest;

  test('publishes the complete schema-v1 fixture catalog', () => {
    expect(manifest.schemaVersion).toBe(1);
    expect(manifest.digestAlgorithm).toBe('sha256-canonical-json-v1');
    expect(manifest.fixtures.map(({ name }) => name)).toEqual([
      'allow',
      'deny',
      'gap-recovery',
      'unknown-event',
      'duplicate-replay',
      'event-id-conflict',
    ]);

    const files = readdirSync(fixtureRoot)
      .filter((file) => file.endsWith('.json') && file !== 'manifest.json')
      .sort();
    expect(files).toEqual(manifest.fixtures.map(({ file }) => file).sort());
  });

  test.each(manifest.fixtures)('$name validates and reaches its recorded terminal state', (entry) => {
    const bytes = readFileSync(resolve(fixtureRoot, entry.file), 'utf8');
    const fixture = JSON.parse(bytes) as Fixture;

    expect(entry.schemaVersion).toBe(1);
    expect(fixture.name).toBe(entry.name);
    expect(sha256(bytes)).toBe(entry.fixtureSha256);

    const { state, outcomes } = projectFixture(fixture.events);
    expect(outcomes).toEqual(entry.expectedOutcomes);
    expect(summarize(state)).toEqual(entry.expectedTerminalState);
    expect(sha256(canonicalJson(state))).toBe(entry.stateSha256);
  });
});
