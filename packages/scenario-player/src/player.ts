import {
  initialProductState,
  project,
  validateEvent,
  type JsonObject,
  type ProductState,
  type RawProductEvent,
} from '@cyber/protocol';
import type {
  EventSource,
  RuntimeCommand,
  RuntimeSnapshot,
  Unsubscribe,
} from '@cyber/runtime-client';

import {
  CANDIDATE_FINDING,
  LAB_SCOPE,
  RECON_EVIDENCE,
  VERIFIED_EVIDENCE,
} from './scenario';

export type ScenarioOptions = {
  runtimeId: 'scenario-local' | 'scenario-remote';
  speedMs: number;
  injectFailureAt?: 'recon' | 'verification';
};

const BASE_TIME = Date.parse('2026-08-03T12:00:00.000Z');
const APPROVAL_EXPIRES_AT = new Date(BASE_TIME + 25_000).toISOString();

export class ScenarioPlayer implements EventSource {
  private readonly eventLog: RawProductEvent[] = [];
  private state: ProductState = initialProductState();
  private listener?: (event: RawProductEvent) => void;
  private closed = false;
  private taskCreated = false;
  private scopeConfirmed = false;
  private approvalRequested = false;
  private approvalResolved = false;
  private scopeRevision = 1;
  private currentScope = LAB_SCOPE;

  constructor(private readonly options: ScenarioOptions) {
    if (!Number.isFinite(options.speedMs) || options.speedMs < 0) {
      throw new Error('invalid_scenario_speed');
    }
  }

  async subscribe(afterCursor: number, onEvent: (event: RawProductEvent) => void): Promise<Unsubscribe> {
    if (!Number.isSafeInteger(afterCursor) || afterCursor < 0) throw new Error('invalid_cursor');
    this.closed = false;
    this.listener = onEvent;
    for (const event of this.eventLog) if ((event.cursor as number) > afterCursor) onEvent(event);
    return () => {
      if (this.listener === onEvent) this.listener = undefined;
    };
  }

  async getSnapshot(): Promise<RuntimeSnapshot> {
    return { cursor: this.state.committedCursor, state: structuredClone(this.state) };
  }

  async send(command: RuntimeCommand): Promise<void> {
    switch (command.type) {
      case 'task.create':
        await this.createTask(command);
        break;
      case 'scope.confirm':
        await this.confirmScope(command.scopeId);
        break;
      case 'task.pause':
        await this.append('task.paused', {});
        break;
      case 'task.resume':
        await this.append('task.resumed', {});
        break;
      case 'task.cancel':
        await this.append('task.cancel.requested', {});
        await this.append('task.cancelled', {});
        break;
      case 'approval.respond':
        await this.resolveApproval(command.challengeId, command.decision);
        break;
      case 'control.take':
        await this.takeControl(command.expectedRevision);
        break;
      case 'instruction.send':
        if (command.content === 'request_scope_revision') {
          await this.reviseScope();
        } else {
          await this.append('question.resolved', {
            questionId: `instruction-${this.nextCursor()}`,
            answer: command.content,
          });
        }
        break;
    }
  }

  async close(): Promise<void> {
    this.closed = true;
    this.listener = undefined;
  }

  events(): readonly RawProductEvent[] {
    return this.eventLog;
  }

  private async createTask(command: Extract<RuntimeCommand, { type: 'task.create' }>): Promise<void> {
    if (this.taskCreated) throw new Error('task_already_created');
    if (command.runtimeId !== this.options.runtimeId) throw new Error('runtime_mismatch');
    if (!command.objective.includes('juice-shop.lab')) throw new Error('unsupported_target');
    this.taskCreated = true;
    await this.append('task.created', { title: command.objective });
    await this.append('scope.proposed', { scope: this.currentScope });
  }

  private async confirmScope(scopeId: string): Promise<void> {
    if (!this.taskCreated) throw new Error('task_required');
    if (scopeId !== this.currentScope.id) throw new Error('unknown_scope');
    if (this.scopeConfirmed) throw new Error('scope_already_confirmed');
    this.scopeConfirmed = true;
    await this.append('scope.confirmed', { scope: this.currentScope });
    await this.append('task.started', { title: 'Authorized juice-shop.lab assessment' });
    await this.append('agent.started', {
      agent: { id: 'agent-recon', name: 'Recon Agent', status: 'running', progress: 0 },
    });
    await this.append('tool.started', { callId: 'tool-recon', name: 'bounded-recon' });
    if (this.options.injectFailureAt === 'recon') {
      await this.failStage('agent-recon', 'tool-recon', 'recon_failed');
      return;
    }
    for (const evidence of RECON_EVIDENCE) {
      await this.append('evidence.committed', { evidence });
    }
    await this.append('tool.completed', {
      callId: 'tool-recon',
      success: true,
      evidenceIds: RECON_EVIDENCE.map((evidence) => evidence.id),
    });
    await this.append('agent.completed', { agentId: 'agent-recon' });
    this.approvalRequested = true;
    await this.append('approval.requested', {
      challenge: {
        id: 'approval-1',
        agentId: 'agent-verification',
        action: 'bounded-login-verification',
        target: 'juice-shop.lab',
        parameterDigest: 'sha256:scenario-login-v1',
        risk: 'medium',
        expiresAt: APPROVAL_EXPIRES_AT,
      },
    });
  }

  private async resolveApproval(
    challengeId: string,
    decision: Extract<RuntimeCommand, { type: 'approval.respond' }>['decision'],
  ): Promise<void> {
    if (!this.approvalRequested || challengeId !== 'approval-1') throw new Error('unknown_approval');
    if (this.approvalResolved) throw new Error('approval_already_resolved');
    if (this.timestamp(this.nextCursor()) > Date.parse(APPROVAL_EXPIRES_AT)) {
      throw new Error('approval_expired');
    }
    this.approvalResolved = true;
    await this.append('approval.resolved', { challengeId, decision });
    await this.append('finding.created', { finding: CANDIDATE_FINDING });

    if (decision === 'deny') {
      await this.append('finding.rejected', {
        findingId: CANDIDATE_FINDING.id,
        reason: 'Verification denied; candidate retained as an explicit limitation.',
      });
      await this.append('task.completed', {});
      return;
    }

    await this.append('finding.verifying', { findingId: CANDIDATE_FINDING.id });
    await this.append('agent.started', {
      agent: { id: 'agent-verification', name: 'Verification Agent', status: 'running', progress: 0 },
    });
    await this.append('tool.started', {
      callId: 'tool-verification',
      name: 'bounded-login-verification',
    });
    if (this.options.injectFailureAt === 'verification') {
      await this.failStage('agent-verification', 'tool-verification', 'verification_failed');
      return;
    }
    await this.append('evidence.committed', { evidence: VERIFIED_EVIDENCE });
    await this.append('tool.completed', {
      callId: 'tool-verification',
      success: true,
      evidenceIds: [VERIFIED_EVIDENCE.id],
    });
    await this.append('finding.confirmed', { findingId: CANDIDATE_FINDING.id });
    await this.append('agent.completed', { agentId: 'agent-verification' });
    await this.append('task.completed', {});
  }

  private async takeControl(expectedRevision: number): Promise<void> {
    if (expectedRevision !== this.state.highestCommittedLeaseRevision) {
      throw new Error('stale_control_revision');
    }
    const revision = expectedRevision + 1;
    await this.append(expectedRevision === 0 ? 'control.acquired' : 'control.transferred', {
      lease: { clientId: 'web-client', revision },
    });
  }

  private async reviseScope(): Promise<void> {
    if (!this.taskCreated) throw new Error('task_required');
    this.scopeRevision += 1;
    this.scopeConfirmed = false;
    this.currentScope = { ...LAB_SCOPE, id: `scope-${this.scopeRevision}` };
    await this.append('scope.proposed', { scope: this.currentScope });
  }

  private async failStage(agentId: string, callId: string, reason: string): Promise<void> {
    await this.append('tool.failed', { callId, reason });
    await this.append('agent.failed', { agentId, reason });
    await this.append('task.blocked', { reason });
  }

  private async append(type: string, payload: JsonObject): Promise<void> {
    if (this.options.speedMs > 0) {
      await new Promise((resolve) => setTimeout(resolve, this.options.speedMs));
    }
    const cursor = this.nextCursor();
    const event: RawProductEvent = {
      schemaVersion: 1,
      eventId: `${this.options.runtimeId}-${cursor}-${type}`,
      taskId: 'task-1',
      cursor,
      occurredAt: new Date(this.timestamp(cursor)).toISOString(),
      type,
      source: { runtimeId: this.options.runtimeId },
      payload,
    };
    const result = project(this.state, validateEvent(event));
    if (result.kind === 'resync-required') throw new Error('scenario_cursor_gap');
    this.state = result.state;
    this.eventLog.push(event);
    if (!this.closed) this.listener?.(event);
  }

  private nextCursor(): number {
    return this.eventLog.length + 1;
  }

  private timestamp(cursor: number): number {
    return BASE_TIME + cursor * 1_000;
  }
}
