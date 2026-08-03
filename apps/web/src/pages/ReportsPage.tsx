import type { Translator } from '@cyber/i18n';
import type { ProductState } from '@cyber/protocol';

export function ReportsPage({ product, t }: { product: ProductState; t: Translator }) {
  return <section className="page"><h1>{t.t('nav.reports')}</h1><p>{product.report?.id ?? t.t('common.none')}</p></section>;
}
