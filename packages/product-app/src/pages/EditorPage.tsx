import { useEffect, useState, type ReactNode } from 'react';

import type { Translator } from '@cyber/i18n';
import type { EditorDraftState, ProductState } from '@cyber/protocol';
import type { AppStore } from '../app-store';
import { editorByteLength } from '@cyber/editor-model';

export type CodeEditorSurfaceProps = { path: string; value: string; readOnly: boolean; onChange: (value: string) => void };
type EditorPageProps = { product: ProductState; draftId: string; store: AppStore; t: Translator; onBack: () => void; renderEditor?: (props: CodeEditorSurfaceProps) => ReactNode };

const toBytes = (value: string) => new TextEncoder().encode(value);
const toBase64 = (value: string) => {
  const encoded = toBytes(value);
  let binary = '';
  for (const byte of encoded) binary += String.fromCharCode(byte);
  return btoa(binary);
};
const fromBase64 = (value: string) => {
  const binary = atob(value);
  return new TextDecoder('utf-8', { fatal: true }).decode(Uint8Array.from(binary, (char) => char.charCodeAt(0)));
};

export function EditorPage({ product, draftId, store, t, onBack, renderEditor }: EditorPageProps) {
  const draft = product.editorDrafts[draftId] as EditorDraftState | undefined;
  const [content, setContent] = useState('');
  const [baseContent, setBaseContent] = useState('');
  const [status, setStatus] = useState<'loading' | 'ready' | 'saving' | 'complete' | 'conflict' | 'error'>('loading');
  const [message, setMessage] = useState('');
  const terminal = draft?.status === 'applied' || draft?.status === 'verified' || draft?.status === 'discarded';
  const readOnly = draft !== undefined && (!product.controlLease || product.controlLease.clientId !== draft.ownerClientId || terminal);

  useEffect(() => {
    if (!draft) { setStatus('loading'); setMessage(''); return; }
    if (draft.status === 'applied' || draft.status === 'verified') { setStatus('complete'); setMessage(t.t('editor.applied')); return; }
    if (draft.status === 'discarded') { setStatus('complete'); setMessage(t.t('editor.readOnly')); return; }
    if (readOnly) { setStatus('error'); setMessage(t.t('editor.readOnly')); return; }
    let active = true;
    setStatus('loading');
    setMessage('');
    void store.readEditorDraft(product.task?.id ?? '', draft.id, product.controlLease?.revision ?? 0).then((result) => {
      if (!active) return;
      const value = fromBase64(result.data);
      if (editorByteLength(value) !== result.byteLength || result.baseSha256 !== draft.baseSha256) throw new Error('editor_base_mismatch');
      setBaseContent(value); setContent(value); setStatus('ready');
    }).catch((error: unknown) => { if (active) { setStatus('error'); setMessage(error instanceof Error && error.message === 'editor_bridge_unavailable' ? t.t('editor.bridgeUnavailable') : t.t('editor.conflict')); } });
    return () => { active = false; };
  }, [draft, product.controlLease, product.task?.id, readOnly, store, t]);

  const dirty = content !== baseContent;
  const canMutate = status === 'ready' && dirty && !readOnly;
  const save = async () => {
    if (!draft || !canMutate) return;
    setStatus('saving');
    try {
      await store.dispatch({ type: 'editor.save', draftId: draft.id, revision: draft.nextRevision, baseSha256: draft.baseSha256, data: toBase64(content), byteLength: editorByteLength(content), expectedLeaseRevision: product.controlLease?.revision ?? 0 });
      setBaseContent(content); setStatus('ready'); setMessage(t.t('editor.saved'));
    } catch { setStatus('conflict'); setMessage(t.t('editor.conflict')); }
  };
  const apply = async () => {
    if (!draft || status !== 'ready' || draft.status !== 'saved') return;
    setStatus('saving');
    try { await store.dispatch({ type: 'editor.apply', draftId: draft.id, revision: draft.nextRevision - 1, proposedSha256: draft.proposedSha256 ?? '', expectedLeaseRevision: product.controlLease?.revision ?? 0 }); setStatus('ready'); setMessage(t.t('editor.applied')); } catch { setStatus('conflict'); setMessage(t.t('editor.conflict')); }
  };
  const discard = async () => {
    if (!draft || readOnly) return;
    setStatus('saving');
    try {
      await store.dispatch({ type: 'editor.discard', draftId: draft.id, expectedLeaseRevision: product.controlLease?.revision ?? 0 });
      onBack();
    } catch {
      setStatus('conflict');
      setMessage(t.t('editor.conflict'));
    }
  };
  return <section className="page page-editor" data-testid="editor-page">
    <header className="editor-header"><div><p className="mission-eyebrow">{t.t('editor.title')}</p><h1>{draft?.path ?? t.t('common.none')}</h1><p className="mission-meta">{draft?.id ?? ''} · {draft?.baseSha256 ?? ''}</p></div><button type="button" onClick={onBack}>{t.t('common.back')}</button></header>
    {message && status !== 'error' && status !== 'conflict' && <p role="status" className="editor-status">{message}</p>}
    {status === 'loading' && <p role="status">{t.t('editor.loading')}</p>}
    {status === 'error' || status === 'conflict' ? <p role="alert">{message || t.t('editor.conflict')}</p> : (status === 'ready' || status === 'saving') && (renderEditor ? renderEditor({ path: draft?.path ?? '', value: content, readOnly: status !== 'ready' || readOnly, onChange: setContent }) : <textarea className="editor-surface cyber-mono" value={content} onChange={(event) => setContent(event.target.value)} readOnly={status !== 'ready' || readOnly} spellCheck={false} aria-label={draft?.path ?? t.t('editor.title')} />)}
    <footer className="editor-actions"><button type="button" onClick={() => void save()} disabled={!canMutate}>{t.t('editor.save')}</button><button type="button" onClick={() => void apply()} disabled={draft?.status !== 'saved' || status !== 'ready'}>{t.t('editor.apply')}</button><button type="button" onClick={() => void discard()} disabled={!draft || readOnly || status === 'saving'}>{t.t('editor.discard')}</button></footer>
  </section>;
}
