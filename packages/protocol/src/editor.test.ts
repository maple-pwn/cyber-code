import { describe, expect, test } from 'vitest';

import { initialProductState, project, validateEvent, type RawProductEvent } from './index';

const digest = (character: string) => character.repeat(64);
const rawEditorEvent = (cursor: number, type: string, payload: Record<string, unknown>): RawProductEvent => ({
  schemaVersion: 1,
  eventId: `editor-event-${cursor}`,
  taskId: 'task-1',
  cursor,
  occurredAt: '2026-08-06T12:00:00Z',
  type,
  source: { runtimeId: 'runtime-1' },
  payload,
});

const reference = { findingId: 'finding-1', evidenceId: 'evidence-1', startLine: 4, endLine: 8 };
const openedDraft = {
  id: 'draft-1', path: '/lab/app.go', scopeId: 'scope-1', ownerClientId: 'client-1',
  leaseRevision: 2, baseSha256: digest('a'), baseByteLength: 128, encoding: 'utf-8',
  evidenceReferences: [reference],
};

describe('evidence-aware editor protocol', () => {
  test('projects an audited draft, patch, apply, and verification lifecycle', () => {
    const events = [
      rawEditorEvent(1, 'evidence.committed', { evidence: { id: 'evidence-1', taskId: 'task-1', kind: 'source', summary: 'Affected handler', data: { path: '/lab/app.go' } } }),
      rawEditorEvent(2, 'finding.created', { finding: { id: 'finding-1', title: 'Unsafe handler', severity: 'high', status: 'candidate', confidence: 'high', evidenceIds: ['evidence-1'] } }),
      rawEditorEvent(3, 'editor.draft.opened', { draft: openedDraft }),
      rawEditorEvent(4, 'editor.draft.saved', { draftId: 'draft-1', revision: 1, baseSha256: digest('a'), proposedSha256: digest('b'), proposedByteLength: 144 }),
      rawEditorEvent(5, 'editor.patch.applied', { draftId: 'draft-1', revision: 1, baseSha256: digest('a'), proposedSha256: digest('b'), resultSha256: digest('b'), reviewer: 'client-1' }),
      rawEditorEvent(6, 'editor.patch.verified', { draftId: 'draft-1', revision: 1, verificationId: 'verification-1', success: true, evidenceIds: ['evidence-1'] }),
    ];

    let state = initialProductState();
    for (const raw of events) state = project(state, validateEvent(raw)).state;
    const draft = (state as unknown as { editorDrafts: Record<string, Record<string, unknown>> }).editorDrafts['draft-1'];

    expect(draft).toMatchObject({
      status: 'verified', nextRevision: 2, proposedSha256: digest('b'), resultSha256: digest('b'), reviewer: 'client-1',
      verification: { id: 'verification-1', revision: 1, success: true, evidenceIds: ['evidence-1'] },
      evidenceReferences: [reference],
    });
    expect(state.evidence['evidence-1']).toMatchObject({ summary: 'Affected handler', data: { path: '/lab/app.go' } });
  });

  test('rejects malformed editor audit metadata', () => {
    expect(() => validateEvent(rawEditorEvent(1, 'editor.draft.opened', { draft: { ...openedDraft, baseSha256: 'short' } }))).toThrow('invalid_event');
    expect(() => validateEvent(rawEditorEvent(1, 'editor.draft.opened', { draft: { ...openedDraft, evidenceReferences: [{ ...reference, startLine: 9, endLine: 8 }] } }))).toThrow('invalid_event');
    expect(() => validateEvent(rawEditorEvent(1, 'editor.draft.saved', { draftId: 'draft-1', revision: 0, baseSha256: digest('a'), proposedSha256: digest('b'), proposedByteLength: 144 }))).toThrow('invalid_event');
  });

  test('rejects a draft whose provenance is not committed', () => {
    const opened = validateEvent(rawEditorEvent(1, 'editor.draft.opened', { draft: openedDraft }));
    expect(() => project(initialProductState(), opened)).toThrow('editor_provenance_missing');
  });
});
