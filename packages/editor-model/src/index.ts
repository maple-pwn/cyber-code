import type { EditorDraftState } from '@cyber/protocol';

export type EditorSessionStatus = 'editing' | 'conflict' | 'applied' | 'discarded';

export type EditorSession = {
  draft: EditorDraftState;
  baseContent: string;
  content: string;
  dirty: boolean;
  status: EditorSessionStatus;
};

const bytes = (value: string): Uint8Array => new TextEncoder().encode(value);

async function sha256(value: string): Promise<string> {
  const encoded = bytes(value);
  const input = encoded.buffer.slice(encoded.byteOffset, encoded.byteOffset + encoded.byteLength) as ArrayBuffer;
  const digest = await globalThis.crypto.subtle.digest('SHA-256', input);
  return [...new Uint8Array(digest)].map((item) => item.toString(16).padStart(2, '0')).join('');
}

export async function createEditorSession(draft: EditorDraftState, content: string): Promise<EditorSession> {
  if (draft.encoding !== 'utf-8' || bytes(content).byteLength !== draft.baseByteLength || await sha256(content) !== draft.baseSha256) {
    throw new Error('editor_base_mismatch');
  }
  return { draft: structuredClone(draft), baseContent: content, content, dirty: false, status: 'editing' };
}

export function updateEditorContent(session: EditorSession, content: string): EditorSession {
  if (session.status === 'conflict') throw new Error('editor_conflict');
  if (session.status === 'applied' || session.status === 'discarded') throw new Error('editor_not_editable');
  return { ...session, content, dirty: content !== session.baseContent };
}

export function reconcileEditorDraft(session: EditorSession, draft: EditorDraftState): EditorSession {
  if (draft.baseSha256 !== session.draft.baseSha256 || draft.path !== session.draft.path || draft.status === 'discarded') {
    return { ...session, draft: structuredClone(draft), status: draft.status === 'discarded' ? 'discarded' : 'conflict' };
  }
  const status: EditorSessionStatus = draft.status === 'applied' || draft.status === 'verified' ? 'applied' : session.status;
  return { ...session, draft: structuredClone(draft), status };
}

export function editorByteLength(content: string): number {
  return bytes(content).byteLength;
}
