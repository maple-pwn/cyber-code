import type { Translator } from '@cyber/i18n';
import type { ScopeSnapshot } from '@cyber/protocol';

export type ScopeReviewSheetProps = {
  scope: ScopeSnapshot;
  scopeId: string;
  principal?: string;
  workspace?: string;
  validity?: string;
  confirmed?: boolean;
  primaryHeading?: boolean;
  t: Translator;
  onConfirmScope: (scopeId: string) => void;
  onEdit?: () => void;
};

export function ScopeReviewSheet({
  scope,
  scopeId,
  principal,
  workspace,
  validity,
  confirmed = false,
  primaryHeading = false,
  t,
  onConfirmScope,
  onEdit,
}: ScopeReviewSheetProps) {
  const Heading = primaryHeading ? 'h1' : 'h2';
  const rows = [
    [t.t('scope.principal'), principal ?? t.t('common.none')],
    [t.t('scope.targets'), scope.targets.join(', ')],
    [t.t('scope.workspace'), workspace ?? t.t('common.none')],
    [t.t('scope.allowed'), scope.allowedActions.join(', ')],
    [t.t('scope.denied'), scope.deniedActions.join(', ')],
    [t.t('scope.risk'), scope.riskCeiling],
    [t.t('scope.validity'), validity ?? t.t('common.none')],
  ];

  return <section className="cyber-panel cyber-scope-review" aria-labelledby="scope-review-title">
    <header className="cyber-panel-header">
      <div>
        <Heading id="scope-review-title">{t.t('scope.title')}</Heading>
        {confirmed && <p>{t.t('scope.immutable')}</p>}
      </div>
      <span className="cyber-status-warning">{t.t('scope.destructiveDisabled')}</span>
    </header>
    <dl className="cyber-definition-grid">
      {rows.map(([label, value]) => <div key={label}>
        <dt>{label}</dt>
        <dd className="cyber-label">{value}</dd>
      </div>)}
    </dl>
    <footer className="cyber-actions">
      {!confirmed && <button type="button" onClick={() => onConfirmScope(scopeId)}>{t.t('scope.confirm')}</button>}
      {onEdit && <button type="button" onClick={onEdit}>{t.t('scope.edit')}</button>}
    </footer>
  </section>;
}
