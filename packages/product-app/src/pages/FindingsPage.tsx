import { useState } from 'react';

import type { Translator } from '@cyber/i18n';
import type { ProductState } from '@cyber/protocol';
import { FindingCard } from '@cyber/ui';
import type { AppStore } from '../app-store';

export function FindingsPage({ product, t, store, onOpenEditor, editorAvailable = false }: { product: ProductState; t: Translator; store?: AppStore; onOpenEditor?: (draftId: string) => void; editorAvailable?: boolean }) {
  const [editorError, setEditorError] = useState('');
  const associated = new Set(Object.values(product.findings).flatMap((finding) => finding.evidenceIds));
  const lease = product.controlLease;
  const otherEvidence = Object.values(product.evidence).filter((item) => !associated.has(item.id));
  return <section className="page page-findings"><h1>{t.t('nav.findings')}</h1>
    {editorError && <p role="alert">{editorError}</p>}
    <div className="finding-list">{Object.values(product.findings).map((finding) => <FindingCard
      key={finding.id}
      finding={finding}
      evidence={finding.evidenceIds.flatMap((id) => product.evidence[id] ? [product.evidence[id]] : [])}
      t={t}
      onOpenEditor={editorAvailable && store && onOpenEditor && lease && product.scope && product.task ? (evidence) => {
        if (typeof evidence.data.path !== 'string' || !product.scope?.id || typeof evidence.data.startLine !== 'number' || typeof evidence.data.endLine !== 'number') return;
        const draftId = `draft-${crypto.randomUUID()}`;
        setEditorError('');
        void store.dispatch({ type: 'editor.open', draftId, path: evidence.data.path, scopeId: product.scope.id, evidenceReferences: [{ findingId: finding.id, evidenceId: evidence.id, startLine: evidence.data.startLine, endLine: evidence.data.endLine }], expectedLeaseRevision: lease.revision })
          .then(() => onOpenEditor(draftId))
          .catch(() => setEditorError(t.t('editor.openFailed')));
      } : undefined}
    />)}</div>
    <section className="other-evidence" aria-labelledby="other-evidence-title">
      <h2 id="other-evidence-title">{t.t('finding.otherEvidence')}</h2>
      {otherEvidence.length === 0 ? <p>{t.t('common.none')}</p> : <ul>{otherEvidence.map((item) => <li key={item.id}>
        <span className="cyber-provenance">{t.t('evidence.generated')}</span>
        <strong>{item.summary}</strong>
        <pre className="cyber-mono" data-testid="raw-evidence" tabIndex={0}>{JSON.stringify(item.data, null, 2)}</pre>
      </li>)}</ul>}
    </section>
  </section>;
}
