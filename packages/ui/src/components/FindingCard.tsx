import type { Translator } from '@cyber/i18n';
import type { FindingState, ImmutableEvidence } from '@cyber/protocol';

export type FindingCardProps = { finding: FindingState; evidence: readonly ImmutableEvidence[]; t: Translator };

export function FindingCard({ finding, evidence, t }: FindingCardProps) {
  return <article className="cyber-panel cyber-glass cyber-finding" data-testid="finding-card">
    <header><h2>{finding.title}</h2><span className={`cyber-severity-${finding.severity}`}>{t.t('finding.severity')}: {finding.severity}</span></header>
    <p>{t.t(`finding.${finding.status}`)} · {t.t('finding.confidence')}: {finding.confidence}</p>
    <span role="img" className="cyber-confidence-dots" data-level={finding.confidence} aria-label={`${t.t('finding.confidence')}: ${finding.confidence}`}>
      <i /><i /><i />
    </span>
    <ul>{evidence.map((item) => <li key={item.id}><span className="cyber-provenance">{t.t('evidence.generated')}</span> {item.summary}</li>)}</ul>
    {finding.rejectionReason && <p>{finding.rejectionReason}</p>}
  </article>;
}
