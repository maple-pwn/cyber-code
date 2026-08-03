import type { KeyboardEvent } from 'react';

import type { Translator } from '@cyber/i18n';
import type { ApprovalDecision, ApprovalState } from '@cyber/protocol';

export type ApprovalCardProps = {
  approval: ApprovalState;
  t: Translator;
  disabled?: boolean;
  onApprovalDecision: (challengeId: string, decision: ApprovalDecision) => void;
};

export function ApprovalCard({ approval, t, disabled = false, onApprovalDecision }: ApprovalCardProps) {
  const decide = (decision: ApprovalDecision) => onApprovalDecision(approval.id, decision);
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter' && !disabled) {
      event.preventDefault();
      decide('allow_once');
    }
  };
  const rows = [
    [t.t('approval.challengeId'), approval.id],
    [t.t('approval.agent'), approval.agentId],
    [t.t('approval.action'), approval.action],
    [t.t('approval.target'), approval.target],
    [t.t('approval.digest'), approval.parameterDigest],
    [t.t('approval.impact'), approval.risk],
    [t.t('approval.replay'), 'one-shot'],
    [t.t('approval.expiry'), approval.expiresAt],
  ];
  return <article className="cyber-panel cyber-approval" tabIndex={0} onKeyDown={onKeyDown}>
    <h3>{t.t('approval.title')}</h3>
    <dl className="cyber-definition-grid">
      {rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}
    </dl>
    <div className="cyber-actions">
      <button type="button" disabled={disabled} onClick={() => decide('allow_once')}>{t.t('approval.allowOnce')}</button>
      <button type="button" disabled={disabled} onClick={() => decide('deny')}>{t.t('approval.deny')}</button>
    </div>
  </article>;
}
