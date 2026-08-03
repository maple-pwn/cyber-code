import { useState } from 'react';

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
  const reviewKey = `${approval.id}:${approval.parameterDigest}:${approval.expiresAt}`;
  const [reviewedKey, setReviewedKey] = useState<string | null>(null);
  const reviewing = reviewedKey === reviewKey;
  const rows = [
    [t.t('approval.challengeId'), approval.id],
    [t.t('approval.agent'), approval.agentId],
    [t.t('approval.action'), approval.action],
    [t.t('approval.target'), approval.target],
    [t.t('approval.digest'), approval.parameterDigest],
    [t.t('approval.impact'), approval.risk],
    [t.t('approval.replay'), t.t('approval.oneAttempt')],
    [t.t('approval.expiry'), approval.expiresAt],
  ];
  return <article className="cyber-panel cyber-approval" tabIndex={0} data-reviewing={reviewing}>
    <header className="cyber-approval-header">
      <div><span className="cyber-approval-risk">{approval.risk} · {t.t('approval.title')}</span><h3>{approval.action}</h3></div>
      <time data-testid="approval-expiry" dateTime={approval.expiresAt}>{approval.expiresAt}</time>
    </header>
    <p className="cyber-approval-summary">{reviewing ? t.t('approval.reviewing') : `${t.t('approval.oneAttempt')} · ${t.t('approval.noPersistence')}`}</p>
    <dl className="cyber-definition-grid">
      {rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}
    </dl>
    <div className="cyber-actions">
      {reviewing
        ? <>
            <button type="button" disabled={disabled} onClick={() => setReviewedKey(null)}>{t.t('approval.back')}</button>
            <button className="cyber-primary-action" type="button" disabled={disabled} onClick={() => decide('allow_once')}>{t.t('approval.confirmAllowOnce')}</button>
          </>
        : <>
            <button type="button" disabled={disabled} onClick={() => decide('deny')}>{t.t('approval.deny')}</button>
            <button className="cyber-primary-action" type="button" disabled={disabled} onClick={() => setReviewedKey(reviewKey)}>{t.t('approval.review')}</button>
          </>}
    </div>
  </article>;
}
