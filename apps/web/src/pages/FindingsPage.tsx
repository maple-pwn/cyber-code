import type { Translator } from '@cyber/i18n';
import type { ProductState } from '@cyber/protocol';
import { FindingCard } from '@cyber/ui';

export function FindingsPage({ product, t }: { product: ProductState; t: Translator }) {
  const associated = new Set(Object.values(product.findings).flatMap((finding) => finding.evidenceIds));
  const otherEvidence = Object.values(product.evidence).filter((item) => !associated.has(item.id));
  return <section className="page"><h1>{t.t('nav.findings')}</h1>
    <div className="finding-list">{Object.values(product.findings).map((finding) => <FindingCard
      key={finding.id}
      finding={finding}
      evidence={finding.evidenceIds.flatMap((id) => product.evidence[id] ? [product.evidence[id]] : [])}
      t={t}
    />)}</div>
    <section className="other-evidence" aria-labelledby="other-evidence-title">
      <h2 id="other-evidence-title">{t.t('finding.otherEvidence')}</h2>
      {otherEvidence.length === 0 ? <p>{t.t('common.none')}</p> : <ul>{otherEvidence.map((item) => <li key={item.id}>
        <span className="cyber-provenance">{t.t('evidence.generated')}</span>
        <strong>{item.summary}</strong>
        <pre tabIndex={0}>{JSON.stringify(item.data, null, 2)}</pre>
      </li>)}</ul>}
    </section>
  </section>;
}
