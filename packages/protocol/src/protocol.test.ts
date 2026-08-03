import { describe, expect, it } from 'vitest';
import { initialProductState, project, validateEvent } from './index';

const started = { schemaVersion: 1, eventId: 'evt-1', taskId: 'task-1', cursor: 1, occurredAt: '2026-08-03T00:00:00Z', type: 'task.started', source: { runtimeId: 'scenario-local' }, payload: { title: 'Lab review' } } as const;

describe('validateEvent', () => {
  it('narrows a known envelope', () => expect(validateEvent(started).kind).toBe('known'));
  it('rejects unsupported schema versions', () => expect(() => validateEvent({ ...started, schemaVersion: 2 })).toThrow('unsupported_schema_version'));
  it('retains a well-formed unknown event without projecting it', () => {
    const unknown = validateEvent({ ...started, type: 'future.event' });
    expect(unknown.kind).toBe('unknown');
    expect(project(initialProductState(), unknown).state.rawEvents).toHaveLength(1);
  });
  it('rejects malformed envelope fields', () => expect(() => validateEvent({ ...started, cursor: '1' })).toThrow('invalid_event'));
  it('validates representative known payload families before narrowing', () => {
    const payloads = [
      ['task.failed', { reason: 'failed' }], ['scope.confirmed', { scope: { targets: [], allowedActions: [], deniedActions: [], riskCeiling: 'low' } }], ['runtime.capabilities.updated', { capabilities: [] }], ['agent.started', { agent: { id: 'a', name: 'A', status: 'running' } }], ['tool.completed', { callId: 'c', success: true, evidenceIds: [] }], ['evidence.committed', { evidence: { id: 'e', kind: 'http', summary: 'ok', data: {} } }], ['finding.created', { finding: { id: 'f', title: 'F', severity: 'low', status: 'candidate', confidence: 'low', evidenceIds: [] } }], ['approval.resolved', { challengeId: 'a', decision: 'deny' }], ['control.transferred', { lease: { clientId: 'c', revision: 1 } }], ['report.frozen', { reportId: 'r', version: 1 }],
    ] as const;
    for (const [type, payload] of payloads) expect(validateEvent({ ...started, type, payload }).kind).toBe('known');
    expect(() => validateEvent({ ...started, payload: {} })).toThrow('invalid_event');
  });
});
