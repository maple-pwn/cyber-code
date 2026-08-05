import { project, validateEvent, initialProductState, type RawProductEvent } from '@cyber/protocol';

import {
  LocalEventSource,
  type LocalRequest,
  type LocalTransport,
} from './index';
import { defineLocalEventSourceConformance } from './local-event-source-conformance.test-support';

class InProcessRuntimeTransport implements LocalTransport {
  private readonly runtimeId = 'runtime-in-process';
  private readonly events: RawProductEvent[] = [];

  async start(): Promise<unknown> {
    return this.handshake('in-process-start');
  }

  async request(request: LocalRequest): Promise<unknown> {
    switch (request.type) {
      case 'handshake':
        return this.handshake(request.id);
      case 'command':
        if (request.command?.command.type === 'task.create' && this.events.length === 0) {
          this.events.push(this.event(1, 'task.created', { title: request.command.command.objective }));
          this.events.push(this.event(2, 'scope.proposed', {
            scope: {
              id: 'scope-1',
              principal: 'local-user',
              workspace: request.command.command.workspace ?? '/workspace',
              validity: 'task',
              targets: [request.command.command.workspace ?? '/workspace'],
              allowedActions: ['read'],
              deniedActions: [],
              riskCeiling: 'low',
            },
          }));
        }
        return {
          id: request.id,
          type: 'command',
          receipt: { idempotencyKey: request.command?.idempotencyKey, status: 'accepted' },
        };
      case 'events':
        return {
          id: request.id,
          type: 'events',
          events: this.events.filter((item) => (item.cursor as number) > (request.afterCursor ?? 0)),
        };
      case 'snapshot': {
        const state = this.events.reduce((current, raw) => {
          const result = project(current, validateEvent(raw));
          if (result.kind === 'resync-required') throw new Error('invalid in-process fixture');
          return result.state;
        }, initialProductState());
        return { id: request.id, type: 'snapshot', snapshot: { cursor: state.committedCursor, state } };
      }
      case 'close':
        return { id: request.id, type: 'closed' };
      case 'health':
        return { id: request.id, type: 'health', ready: true };
    }
  }

  async restart(): Promise<unknown> {
    return this.handshake('in-process-restart');
  }

  async stop(): Promise<unknown> {
    return { stopped: true };
  }

  private handshake(id: string) {
    return {
      id,
      type: 'handshake',
      handshake: {
        protocolVersion: 1,
        runtimeId: this.runtimeId,
        principal: 'local-user',
        role: 'owner',
        capabilities: ['events', 'snapshot', 'commands'],
        source: {
          mode: 'local',
          runtimeId: this.runtimeId,
          principal: 'local-user',
          capabilities: ['events', 'snapshot', 'commands'],
        },
      },
    };
  }

  private event(cursor: number, type: string, payload: Record<string, unknown>): RawProductEvent {
    return {
      schemaVersion: 1,
      eventId: `event-${cursor}`,
      taskId: 'task-1',
      cursor,
      occurredAt: `2026-08-04T12:00:0${cursor}.000Z`,
      type,
      source: { runtimeId: this.runtimeId },
      payload,
    };
  }
}

defineLocalEventSourceConformance('in-process local runtime', async () => ({
  source: new LocalEventSource(new InProcessRuntimeTransport(), { pollIntervalMs: 60_000 }),
}));
