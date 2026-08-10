import { createHash } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import { runnerImport } from 'vite';

const repositoryRoot = resolve(import.meta.dirname, '..');
const fixtureRoot = resolve(repositoryRoot, 'tests/fixtures/product-events');

const canonicalJson = (value) => JSON.stringify(value, (_key, item) => (
  item && typeof item === 'object' && !Array.isArray(item)
    ? Object.fromEntries(Object.entries(item).sort(([left], [right]) => (
      left < right ? -1 : left > right ? 1 : 0
    )))
    : item
));

const sha256 = (value) => createHash('sha256').update(value).digest('hex');

const event = (cursor, type, payload, eventId = `fixture-${cursor}-${type}`) => ({
  schemaVersion: 1,
  eventId,
  taskId: 'task-1',
  cursor,
  occurredAt: new Date(Date.parse('2026-08-03T12:00:00.000Z') + cursor * 1_000).toISOString(),
  type,
  source: { runtimeId: 'scenario-local' },
  payload,
});

const summarize = (state) => ({
  committedCursor: state.committedCursor,
  taskStatus: state.task?.status ?? null,
  timelineLength: state.timeline.length,
  rawEventCount: state.rawEvents.length,
});

const projectionOutcome = (result) => (
  result.kind === 'resync-required'
    ? `resync-required:${result.expectedCursor}`
    : result.kind
);

const main = async () => {
  const [{ module: { ScenarioPlayer } }, { module: protocol }] = await Promise.all([
    runnerImport(resolve(repositoryRoot, 'packages/scenario-player/src/player.ts'), {
      configFile: false,
      root: repositoryRoot,
    }),
    runnerImport(resolve(repositoryRoot, 'packages/protocol/src/index.ts'), {
      configFile: false,
      root: repositoryRoot,
    }),
  ]);
  const { initialProductState, project, validateEvent } = protocol;

  const scenarioEvents = async (decision) => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    await player.send({
      type: 'task.create',
      objective: '评估 juice-shop.lab',
      runtimeId: 'scenario-local',
    });
    await player.send({ type: 'scope.confirm', scopeId: 'scope-1' });
    await player.send({ type: 'approval.respond', challengeId: 'approval-1', decision });
    return JSON.parse(JSON.stringify(player.events()));
  };

  const taskStarted = event(1, 'task.started', { title: 'Authorized lab assessment' });
  const gapEvent = event(3, 'agent.started', {
    agent: { id: 'agent-recon', name: 'Recon Agent', status: 'running', progress: 0 },
  });
  const recoveredEvent = event(2, 'scope.confirmed', {
    scope: {
      id: 'scope-1',
      principal: 'authorized-operator',
      workspace: '/labs/juice-shop',
      validity: 'single-task',
      targets: ['juice-shop.lab'],
      allowedActions: ['passive-recon'],
      deniedActions: ['destructive'],
      riskCeiling: 'medium',
    },
  });

  const fixtures = [
    { name: 'allow', file: 'allow.json', events: await scenarioEvents('allow_once') },
    { name: 'deny', file: 'deny.json', events: await scenarioEvents('deny') },
    {
      name: 'gap-recovery',
      file: 'gap-recovery.json',
      events: [taskStarted, gapEvent, recoveredEvent, gapEvent],
    },
    {
      name: 'unknown-event',
      file: 'unknown-event.json',
      events: [
        taskStarted,
        event(2, 'runtime.future.capability', { capability: 'future-observer', enabled: true }),
      ],
    },
    {
      name: 'duplicate-replay',
      file: 'duplicate-replay.json',
      events: [taskStarted, taskStarted],
    },
    {
      name: 'event-id-conflict',
      file: 'event-id-conflict.json',
      events: [
        taskStarted,
        event(1, 'task.started', { title: 'Conflicting assessment' }, taskStarted.eventId),
      ],
    },
  ];

  await mkdir(fixtureRoot, { recursive: true });
  const entries = [];

  for (const fixture of fixtures) {
    let state = initialProductState();
    const expectedOutcomes = [];
    for (const raw of fixture.events) {
      const validated = validateEvent(raw);
      try {
        const result = project(state, validated);
        expectedOutcomes.push(projectionOutcome(result));
        state = result.state;
      } catch (error) {
        expectedOutcomes.push(`error:${error instanceof Error ? error.message : 'unknown_error'}`);
      }
    }

    const bytes = `${JSON.stringify({ name: fixture.name, events: fixture.events }, null, 2)}\n`;
    await writeFile(resolve(fixtureRoot, fixture.file), bytes, 'utf8');
    entries.push({
      name: fixture.name,
      file: fixture.file,
      schemaVersion: 1,
      expectedOutcomes,
      expectedTerminalState: summarize(state),
      fixtureSha256: sha256(bytes),
      stateSha256: sha256(canonicalJson(state)),
    });
  }

  const manifest = {
    schemaVersion: 1,
    digestAlgorithm: 'sha256-canonical-json-v1',
    fixtures: entries,
  };
  await writeFile(resolve(fixtureRoot, 'manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`, 'utf8');
};

await main();
