import { createTranslator, type Translator } from '@cyber/i18n';
import type { ScopeSnapshot } from '@cyber/protocol';
import { ScopeReviewSheet } from '@cyber/ui';

export type ScopeReviewPageProps = {
  scope: ScopeSnapshot;
  runtime: { id: string; label: string };
  t?: Translator;
  onConfirm: (scopeId: string) => void;
  onEdit: () => void;
};

export function ScopeReviewPage({ scope, runtime, t = createTranslator(), onConfirm, onEdit }: ScopeReviewPageProps) {
  return <div className="page page-scope-review">
    <p className="scope-runtime-context">{t.t('runtime.label')}: <strong>{runtime.label}</strong> <code className="cyber-mono">{runtime.id}</code></p>
    <section className="derived-scope-targets" aria-label={t.t('scope.derivedTargets')}>
      <strong>{t.t('scope.derivedTargets')}</strong>
      <span className="cyber-mono">{scope.targets.join(', ')}</span>
    </section>
    <ScopeReviewSheet
      scope={scope}
      scopeId={scope.id}
      primaryHeading
      t={t}
      onConfirmScope={onConfirm}
      onEdit={onEdit}
    />
  </div>;
}
