import type { Translator } from '@cyber/i18n';
import type { FindingState, ImmutableEvidence } from '@cyber/protocol';

export type FindingCardProps = { finding: FindingState; evidence: readonly ImmutableEvidence[]; t: Translator; onOpenEditor?: (evidence: ImmutableEvidence) => void };

export function FindingCard({ finding, evidence, t, onOpenEditor }: FindingCardProps) {
  const editableEvidence = evidence.find((item) => typeof item.data.path === 'string'
    && (item.data.path.startsWith('/') || /^[A-Za-z]:[\\/]/.test(item.data.path))
    && Number.isSafeInteger(item.data.startLine) && (item.data.startLine as number) > 0
    && Number.isSafeInteger(item.data.endLine) && (item.data.endLine as number) >= (item.data.startLine as number));
  return <article className="cyber-panel cyber-glass cyber-finding" data-testid="finding-card">
    <header><h2>{finding.title}</h2><span className={`cyber-severity-${finding.severity}`}>{t.t('finding.severity')}: {finding.severity}</span></header>
    <p>{t.t(`finding.${finding.status}`)} · {t.t('finding.confidence')}: {finding.confidence}</p>
    <span role="img" className="cyber-confidence-dots" data-level={finding.confidence} aria-label={`${t.t('finding.confidence')}: ${finding.confidence}`}>
      <i /><i /><i />
    </span>
    <ul>{evidence.map((item) => <li key={item.id}><span className="cyber-provenance">{t.t('evidence.generated')}</span> {item.summary}</li>)}</ul>
    {onOpenEditor && editableEvidence && <button type="button" onClick={() => onOpenEditor(editableEvidence)}>{t.t('editor.title')}</button>}
    {finding.rejectionReason && <p>{finding.rejectionReason}</p>}
  </article>;
}
