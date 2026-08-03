import type { Translator } from '@cyber/i18n';
import type { ProductState } from '@cyber/protocol';
import { FindingCard } from '@cyber/ui';

export function FindingsPage({ product, t }: { product: ProductState; t: Translator }) {
  return <section className="page"><h1>{t.t('nav.findings')}</h1>
    <div className="finding-list">{Object.values(product.findings).map((finding) => <FindingCard
      key={finding.id}
      finding={finding}
      evidence={finding.evidenceIds.flatMap((id) => product.evidence[id] ? [product.evidence[id]] : [])}
      t={t}
    />)}</div>
  </section>;
}
