import { describe, expect, it } from 'vitest';
import { initialProductState, project, validateEvent } from './index';

const event = (type: string, payload: Record<string, unknown>, cursor: number, eventId = `evt-${cursor}`) => validateEvent({ schemaVersion: 1, eventId, taskId: 'task-1', cursor, occurredAt: '2026-08-03T00:00:00Z', type, source: { runtimeId: 'scenario-local' }, payload });
const apply = (state: ReturnType<typeof initialProductState>, value: ReturnType<typeof event>) => project(state, value).state;

describe('project', () => {
  it('ignores an identical replay', () => {
    const value = event('task.started', { title: 'Lab review' }, 1);
    const state = apply(initialProductState(), value);
    expect(project(state, value).kind).toBe('duplicate');
  });
  it('rejects a reused event ID with a different canonical envelope', () => {
    const state = apply(initialProductState(), event('task.started', { title: 'Lab review' }, 1));
    expect(() => project(state, event('task.started', { title: 'Changed' }, 2, 'evt-1'))).toThrow('event_id_conflict');
  });
  it('rejects stale unseen cursors', () => {
    const state = apply(initialProductState(), event('task.started', { title: 'Lab review' }, 1));
    expect(() => project(state, event('task.paused', {}, 1, 'evt-2'))).toThrow('stale_cursor');
  });
  it('requires resync for a cursor gap without changing state', () => {
    const state = apply(initialProductState(), event('task.started', { title: 'Lab review' }, 1));
    const result = project(state, event('task.paused', {}, 3));
    expect(result).toMatchObject({ kind: 'resync-required', expectedCursor: 2, state });
  });
  it('permits only legal Finding transitions', () => {
    let state = apply(initialProductState(), event('finding.created', { finding: { id: 'f-1', title: 'X', severity: 'high', status: 'candidate', confidence: 'high', evidenceIds: [] } }, 1));
    state = apply(state, event('finding.verifying', { findingId: 'f-1' }, 2));
    state = apply(state, event('finding.confirmed', { findingId: 'f-1' }, 3));
    expect(state.findings['f-1'].status).toBe('confirmed');
    expect(() => project(state, event('finding.verifying', { findingId: 'f-1' }, 4))).toThrow('invalid_finding_transition');
  });
  it('resolves an approval at most once', () => {
    let state = apply(initialProductState(), event('approval.requested', { challenge: { id: 'a-1', agentId: 'agent-1', action: 'verify', target: 'lab', parameterDigest: 'abc', risk: 'high', expiresAt: '2026-08-03T01:00:00Z' } }, 1));
    state = apply(state, event('approval.resolved', { challengeId: 'a-1', decision: 'allow_once' }, 2));
    expect(() => project(state, event('approval.resolved', { challengeId: 'a-1', decision: 'deny' }, 3))).toThrow('approval_already_resolved');
  });
  it('rejects repeated approval requests that could erase a decision', () => {
    const challenge = { id: 'a-1', agentId: 'agent-1', action: 'verify', target: 'lab', parameterDigest: 'abc', risk: 'high', expiresAt: '2026-08-03T01:00:00Z' };
    let state = apply(initialProductState(), event('approval.requested', { challenge }, 1));
    state = apply(state, event('approval.resolved', { challengeId: 'a-1', decision: 'deny' }, 2));
    expect(() => project(state, event('approval.requested', { challenge: { ...challenge, target: 'changed' } }, 3))).toThrow('approval_challenge_conflict');
  });
  it('rejects every expired approval resolution, including deny', () => {
    const state = apply(initialProductState(), validateEvent({ schemaVersion: 1, eventId: 'evt-1', taskId: 'task-1', cursor: 1, occurredAt: '2026-08-03T00:00:00Z', type: 'approval.requested', source: { runtimeId: 'scenario-local' }, payload: { challenge: { id: 'a-1', agentId: 'agent-1', action: 'verify', target: 'lab', parameterDigest: 'abc', risk: 'high', expiresAt: '2026-08-03T00:01:00Z' } } }));
    expect(() => project(state, validateEvent({ schemaVersion: 1, eventId: 'evt-2', taskId: 'task-1', cursor: 2, occurredAt: '2026-08-03T00:02:00Z', type: 'approval.resolved', source: { runtimeId: 'scenario-local' }, payload: { challengeId: 'a-1', decision: 'deny' } }))).toThrow('approval_expired');
  });

  it('cannot admit non-canonical values into duplicate detection', () => {
    class Payload { value = 'x'; }
    const sparse = ['value'] as string[]; sparse.length = 2;
    for (const value of [new Date('2026-08-03T00:00:00Z'), new Payload(), sparse]) {
      expect(() => validateEvent({ schemaVersion: 1, eventId: 'evt-invalid', taskId: 'task-1', cursor: 1, occurredAt: '2026-08-03T00:00:00Z', type: 'future.event', source: { runtimeId: 'scenario-local' }, payload: { value } })).toThrow('invalid_event');
    }
  });

  it('rejects non-canonical array properties before duplicate detection', () => {
    const holeWithExtra = [] as string[] & { extra?: string }; holeWithExtra.length = 1; holeWithExtra.extra = 'x';
    const denseWithExtra = ['value'] as string[] & { extra?: string }; denseWithExtra.extra = 'x';
    const symbolValue = ['value']; Object.defineProperty(symbolValue, Symbol('hidden'), { value: 'x' });
    for (const value of [holeWithExtra, denseWithExtra, symbolValue]) {
      expect(() => validateEvent({ schemaVersion: 1, eventId: 'evt-invalid', taskId: 'task-1', cursor: 1, occurredAt: '2026-08-03T00:00:00Z', type: 'future.event', source: { runtimeId: 'scenario-local' }, payload: { value } })).toThrow('invalid_event');
    }
    const accepted = validateEvent({ schemaVersion: 1, eventId: 'evt-1', taskId: 'task-1', cursor: 1, occurredAt: '2026-08-03T00:00:00Z', type: 'future.event', source: { runtimeId: 'scenario-local' }, payload: { value: ['dense', null] } });
    expect(project(initialProductState(), accepted).kind).toBe('applied');
  });

  it('requires monotonically increasing lease revisions, including after release', () => {
    let state = apply(initialProductState(), event('control.transferred', { lease: { clientId: 'c-1', revision: 1 } }, 1));
    state = apply(state, event('control.transferred', { lease: { clientId: 'c-2', revision: 2 } }, 2));
    expect(() => project(state, event('control.transferred', { lease: { clientId: 'c-3', revision: 2 } }, 3))).toThrow('non_monotonic_lease_revision');
    state = apply(state, event('control.released', { clientId: 'c-2' }, 3));
    expect(() => project(state, event('control.acquired', { lease: { clientId: 'c-3', revision: 1 } }, 4))).toThrow('non_monotonic_lease_revision');
  });
  it('clones and freezes committed Evidence', () => {
    const evidence = { id: 'e-1', taskId: 'task-1', kind: 'http', summary: 'response', data: { status: 200 } };
    const state = apply(initialProductState(), event('evidence.committed', { evidence }, 1));
    evidence.data.status = 500;
    expect(state.evidence['e-1'].data.status).toBe(200);
    expect(Object.isFrozen(state.evidence['e-1'])).toBe(true);
    expect(Object.isFrozen(state.evidence['e-1'].data)).toBe(true);
  });
  it('does not alias mutable event payloads into projected state', () => {
    const scope = { id: 's', principal: 'p', workspace: '/lab', validity: 'task', targets: ['lab'], allowedActions: [], deniedActions: [], riskCeiling: 'high' };
    const agent = { id: 'agent-1', name: 'Scout', status: 'running' };
    const report = { id: 'r-1', taskId: 'task-1', version: 0, status: 'draft' as const, narrative: 'initial', recommendations: '', humanNotes: '', findings: [] as { finding: { title: string } }[] };
    const scopeEvent = event('scope.confirmed', { scope }, 1);
    let state = apply(initialProductState(), scopeEvent);
    const agentEvent = event('agent.started', { agent }, 2);
    state = apply(state, agentEvent);
    const reportEvent = event('report.drafted', { report }, 3);
    state = apply(state, reportEvent);
    scope.targets[0] = 'changed'; agent.name = 'changed'; report.findings.push({ finding: { title: 'changed' } });
    expect(state.scope?.targets).toEqual(['lab']);
    expect(state.agents['agent-1'].name).toBe('Scout');
    expect(state.report?.narrative).toBe('initial');
    expect(state.report?.findings).toEqual([]);
    expect((state.timeline[0].payload as { scope: { targets: string[] } }).scope.targets).toEqual(['lab']);
    expect(Object.isFrozen(state.scope)).toBe(true);
    expect(Object.isFrozen(state.agents['agent-1'])).toBe(true);
    expect(Object.isFrozen(state.report)).toBe(true);
    expect(Object.isFrozen(state.timeline[0])).toBe(true);
  });

  it('projects scope, agents, task lifecycle, and reports', () => {
    let state = apply(initialProductState(), event('task.created', { title: 'Lab' }, 1));
    state = apply(state, event('scope.confirmed', { scope: { id: 's', principal: 'p', workspace: '/lab', validity: 'task', targets: ['lab'], allowedActions: [], deniedActions: [], riskCeiling: 'high' } }, 2));
    state = apply(state, event('agent.started', { agent: { id: 'agent-1', name: 'Scout', status: 'running' } }, 3));
    state = apply(state, event('agent.progressed', { agentId: 'agent-1', progress: 50, currentAction: 'scan' }, 4));
    state = apply(state, event('task.paused', {}, 5));
    state = apply(state, event('task.resumed', {}, 6));
    state = apply(state, event('report.drafted', { report: { id: 'r-1', taskId: 'task-1', version: 1, status: 'draft', narrative: '', recommendations: '', humanNotes: '', findings: [] } }, 7));
    state = apply(state, event('report.frozen', { reportId: 'r-1', version: 2 }, 8));
    expect(state).toMatchObject({ scope: { targets: ['lab'] }, agents: { 'agent-1': { progress: 50, currentAction: 'scan' } }, task: { status: 'completed' }, report: { version: 2, status: 'frozen' } });
  });
  it('acquires and releases control', () => {
    let state = apply(initialProductState(), event('control.acquired', { lease: { clientId: 'c-1', revision: 1 } }, 1));
    state = apply(state, event('control.released', { clientId: 'c-1' }, 2));
    expect(state.controlLease).toBeNull();
  });
});
