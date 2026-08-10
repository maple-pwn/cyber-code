import { describe, expect, test } from 'vitest';

import type { EditorDraftState } from '@cyber/protocol';

import { createEditorSession, updateEditorContent, reconcileEditorDraft } from './index';

const digest = (value: string) => value === 'hello' ? '2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824' : 'a'.repeat(64);
const draft = (overrides: Partial<EditorDraftState> = {}): EditorDraftState => ({
  id: 'draft-1', path: '/workspace/app.go', scopeId: 'scope-1', ownerClientId: 'client-1', leaseRevision: 1,
  baseSha256: digest('base'), baseByteLength: 5, encoding: 'utf-8', evidenceReferences: [], status: 'open', nextRevision: 1,
  ...overrides,
});

describe('editor model', () => {
  test('creates a clean session and tracks edits without changing protocol state', async () => {
    const session = await createEditorSession(draft({ baseSha256: digest('hello') }), 'hello');
    expect(session.dirty).toBe(false);
    const edited = updateEditorContent(session, 'hello world');
    expect(edited.dirty).toBe(true);
    expect(edited.content).toBe('hello world');
    expect(edited.draft).toEqual(session.draft);
  });

  test('marks a remote base change as a conflict and blocks editing', async () => {
    const session = await createEditorSession(draft({ baseSha256: digest('hello') }), 'hello');
    const conflict = reconcileEditorDraft(session, draft({ baseSha256: 'b'.repeat(64) }));
    expect(conflict.status).toBe('conflict');
    expect(() => updateEditorContent(conflict, 'new')).toThrow('editor_conflict');
  });

  test('rejects content whose digest or byte length does not match the draft', async () => {
    await expect(createEditorSession(draft(), 'wrong')).rejects.toThrow('editor_base_mismatch');
  });
});
