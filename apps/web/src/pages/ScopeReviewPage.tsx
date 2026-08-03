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
    <p>{t.t('runtime.label')}: <strong>{runtime.label}</strong> <code>{runtime.id}</code></p>
    <ScopeReviewSheet
      scope={scope}
      scopeId="scope-1"
      principal="authorized-operator"
      workspace="/labs/juice-shop"
      validity="single task"
      primaryHeading
      t={t}
      onConfirmScope={onConfirm}
      onEdit={onEdit}
    />
  </div>;
}
