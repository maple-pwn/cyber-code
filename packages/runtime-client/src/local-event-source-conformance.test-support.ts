import { describe, expect, test } from 'vitest';

import type { RawProductEvent } from '@cyber/protocol';

import type { EventSource } from './index';

type LocalSourceFixture = {
  source: EventSource;
  verifyClosed?: () => void | Promise<void>;
};

export function defineLocalEventSourceConformance(
  name: string,
  createFixture: () => Promise<LocalSourceFixture>,
  timeout = 10_000,
): void {
  defineEventSourceConformance(name, 'local', createFixture, timeout);
}

export function defineRemoteEventSourceConformance(
  name: string,
  createFixture: () => Promise<LocalSourceFixture>,
  timeout = 10_000,
): void {
  defineEventSourceConformance(name, 'remote', createFixture, timeout);
}

function defineEventSourceConformance(
  name: string,
  mode: 'local' | 'remote',
  createFixture: () => Promise<LocalSourceFixture>,
  timeout: number,
): void {
  describe(name, () => {
    test(`completes the authenticated ${mode} runtime golden path`, async () => {
      const fixture = await createFixture();
      const { source } = fixture;
      const events: RawProductEvent[] = [];
      let closed = false;

      try {
        const handshake = await source.handshake({
          supportedProtocolVersions: [1],
          afterCursor: 0,
        });
        expect(handshake.source).toMatchObject({
          mode,
          runtimeId: handshake.runtimeId,
          principal: handshake.principal,
        });

        await expect(source.send({
          idempotencyKey: 'conformance-task-create',
          command: {
            type: 'task.create',
            objective: 'Inspect the authorized workspace',
            runtimeId: handshake.runtimeId,
          },
        })).resolves.toEqual({
          idempotencyKey: 'conformance-task-create',
          status: 'accepted',
        });

        const unsubscribe = await source.subscribe(0, (event) => events.push(event));
        unsubscribe();
        expect(events.map((event) => event.cursor)).toEqual([1, 2]);
        expect(events.map((event) => event.type)).toEqual(['task.created', 'scope.proposed']);

        const snapshot = await source.getSnapshot();
        expect(snapshot.cursor).toBe(2);
        expect(snapshot.state.committedCursor).toBe(2);
        expect(snapshot.state.task).toMatchObject({
          title: 'Inspect the authorized workspace',
          status: 'created',
        });

        await source.close();
        closed = true;
        await fixture.verifyClosed?.();
      } finally {
        if (!closed) await source.close().catch(() => undefined);
      }
    }, timeout);
  });
}
