import { describe, expect, test, vi } from 'vitest';

import type { RawProductEvent } from '@cyber/protocol';

import { ScenarioPlayer } from './index';

const collect = async (player: ScenarioPlayer, afterCursor = 0) => {
  const events: RawProductEvent[] = [];
  await player.subscribe(afterCursor, (event) => events.push(event));
  return events;
};

const createAndConfirm = async (player: ScenarioPlayer) => {
  await player.send({
    type: 'task.create',
    objective: '评估 juice-shop.lab',
    runtimeId: 'scenario-local',
  });
  await player.send({ type: 'scope.confirm', scopeId: 'scope-1' });
};

describe('ScenarioPlayer', () => {
  test('produces byte-identical allow branches with bounded verification', async () => {
    const first = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    const second = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    const firstEvents = await collect(first);
    const secondEvents = await collect(second);

    for (const player of [first, second]) {
      await createAndConfirm(player);
      await player.send({
        type: 'approval.respond',
        challengeId: 'approval-1',
        decision: 'allow_once',
      });
    }

    expect(JSON.stringify(firstEvents)).toBe(JSON.stringify(secondEvents));
    expect(firstEvents.filter((event) => event.type === 'evidence.committed')).toHaveLength(9);
    expect(firstEvents.some((event) => event.type === 'tool.started'
      && (event.payload as { name?: string }).name === 'bounded-login-verification')).toBe(true);
    expect(firstEvents.map((event) => event.type)).toContain('finding.confirmed');
  });

  test('deny records a verification limitation without starting the bounded tool', async () => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    const events = await collect(player);

    await createAndConfirm(player);
    await player.send({ type: 'approval.respond', challengeId: 'approval-1', decision: 'deny' });

    expect(events.some((event) => event.type === 'tool.started'
      && (event.payload as { name?: string }).name === 'bounded-login-verification')).toBe(false);
    expect(events.map((event) => event.type)).toContain('finding.rejected');
  });

  test('supports pause, resume, cancel, and explicit control takeover', async () => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    await createAndConfirm(player);

    await player.send({ type: 'task.pause' });
    await player.send({ type: 'task.resume' });
    await player.send({ type: 'control.take', expectedRevision: 0 });
    await player.send({ type: 'control.take', expectedRevision: 1 });
    await player.send({ type: 'task.cancel' });

    const events = player.events();
    expect(events.map((event) => event.type)).toEqual(expect.arrayContaining([
      'task.paused', 'task.resumed', 'control.acquired', 'control.transferred',
      'task.cancel.requested', 'task.cancelled',
    ]));
    expect((events.filter((event) => event.type === 'control.transferred').at(-1)?.payload as {
      lease: { revision: number };
    }).lease.revision).toBe(2);
  });

  test('close suspends delivery and re-subscribe replays only later cursors', async () => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    const live = await collect(player);
    await player.send({ type: 'task.create', objective: '评估 juice-shop.lab', runtimeId: 'scenario-local' });
    const cursor = live.at(-1)?.cursor as number;

    await player.close();
    await player.send({ type: 'task.pause' });
    expect(live.at(-1)?.cursor).toBe(cursor);

    const replay = await collect(player, cursor);
    expect(replay.every((event) => (event.cursor as number) > cursor)).toBe(true);
    expect(replay.map((event) => event.type)).toContain('task.paused');
  });

  test('returns a projector-backed snapshot at the latest cursor', async () => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    await createAndConfirm(player);

    const snapshot = await player.getSnapshot();

    expect(snapshot.cursor).toBe(player.events().at(-1)?.cursor);
    expect(snapshot.state.committedCursor).toBe(snapshot.cursor);
    expect(snapshot.state.evidence).toHaveProperty('evidence-route-count');
  });

  test.each(['recon', 'verification'] as const)('injects a local-only %s failure', async (stage) => {
    const player = new ScenarioPlayer({
      runtimeId: 'scenario-local',
      speedMs: 0,
      injectFailureAt: stage,
    });
    await createAndConfirm(player);
    if (stage === 'verification') {
      await player.send({ type: 'approval.respond', challengeId: 'approval-1', decision: 'allow_once' });
    }

    expect(player.events().map((event) => event.type)).toEqual(expect.arrayContaining([
      'tool.failed', 'agent.failed', 'task.blocked',
    ]));
    expect(player.events().every((event) => (event.source as { runtimeId: string }).runtimeId === 'scenario-local'))
      .toBe(true);
  });

  test('rejects targets outside the authorized web lab', async () => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });

    await expect(player.send({
      type: 'task.create',
      objective: '评估 production.example.com',
      runtimeId: 'scenario-local',
    })).rejects.toThrow('unsupported_target');
    expect(player.events()).toHaveLength(0);
  });

  test('rejects repeated and expired approval responses', async () => {
    const repeated = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    await createAndConfirm(repeated);
    await repeated.send({ type: 'approval.respond', challengeId: 'approval-1', decision: 'deny' });
    await expect(repeated.send({
      type: 'approval.respond', challengeId: 'approval-1', decision: 'deny',
    })).rejects.toThrow('approval_already_resolved');

    const expired = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    await createAndConfirm(expired);
    for (let index = 0; index < 6; index += 1) {
      await expired.send({ type: 'task.pause' });
      await expired.send({ type: 'task.resume' });
    }
    await expect(expired.send({
      type: 'approval.respond', challengeId: 'approval-1', decision: 'deny',
    })).rejects.toThrow('approval_expired');
  });

  test('honors configured event speed', async () => {
    vi.useFakeTimers();
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 25 });
    const pending = player.send({
      type: 'task.create', objective: '评估 juice-shop.lab', runtimeId: 'scenario-local',
    });

    expect(player.events()).toHaveLength(0);
    await vi.runAllTimersAsync();
    await pending;
    expect(player.events().length).toBeGreaterThan(0);
    vi.useRealTimers();
  });
});
