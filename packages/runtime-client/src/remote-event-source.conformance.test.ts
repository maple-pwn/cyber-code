import { initialProductState, project, validateEvent, type RawProductEvent } from '@cyber/protocol';

import { RemoteEventSource, type RemoteRequest, type RemoteTransport } from './index';
import { defineRemoteEventSourceConformance } from './local-event-source-conformance.test-support';

class InProcessRemoteTransport implements RemoteTransport {
  private readonly runtimeId = 'runtime-remote-conformance';
  private readonly events: RawProductEvent[] = [];

  async request(request: RemoteRequest): Promise<unknown> {
    switch (request.type) {
      case 'handshake':
        return {
          id: request.id,
          type: 'handshake',
          handshake: {
            protocolVersion: 1,
            runtimeId: this.runtimeId,
            principal: 'remote-operator',
            role: 'operator',
            capabilities: ['events', 'snapshot', 'commands'],
            source: {
              mode: 'remote',
              runtimeId: this.runtimeId,
              principal: 'remote-operator',
              capabilities: ['events', 'snapshot', 'commands'],
            },
          },
        };
      case 'command':
        if (request.command?.command.type === 'task.create' && this.events.length === 0) {
          const workspace = request.command.command.workspace ?? '/workspace';
          this.events.push(this.event(1, 'task.created', { title: request.command.command.objective }));
          this.events.push(this.event(2, 'scope.proposed', {
            scope: {
              id: 'scope-1', principal: 'remote-operator', workspace, validity: 'task',
              targets: [workspace], allowedActions: ['read'], deniedActions: [], riskCeiling: 'low',
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
          events: this.events.filter((event) => (event.cursor as number) > (request.afterCursor ?? 0)),
        };
      case 'snapshot': {
        const state = this.events.reduce((current, raw) => {
          const result = project(current, validateEvent(raw));
          if (result.kind === 'resync-required') throw new Error('invalid_remote_fixture');
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

  private event(cursor: number, type: string, payload: Record<string, unknown>): RawProductEvent {
    return {
      schemaVersion: 1,
      eventId: `remote-event-${cursor}`,
      taskId: 'task-1',
      cursor,
      occurredAt: `2026-08-04T12:00:0${cursor}.000Z`,
      type,
      source: { runtimeId: this.runtimeId },
      payload,
    };
  }
}

defineRemoteEventSourceConformance('in-process remote runtime', async () => ({
  source: new RemoteEventSource(new InProcessRemoteTransport(), { pollIntervalMs: 60_000 }),
}));
