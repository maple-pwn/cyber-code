import type { ImmutableEvidence } from '@cyber/protocol';

export type EvidenceBackdropProps = {
  evidence: Readonly<Record<string, ImmutableEvidence>>;
};

const displayValue = (value: unknown): string => {
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
};

export function EvidenceBackdrop({ evidence }: EvidenceBackdropProps) {
  const latest = Object.values(evidence).at(-1);
  return <div className="cyber-evidence-backdrop" data-testid="evidence-backdrop" aria-hidden="true">
    {latest && <div className="cyber-evidence-context">
      <span>{latest.kind} · {latest.id}</span>
      <strong>{latest.summary}</strong>
      {Object.entries(latest.data).map(([key, value]) => <code key={key}>{key}: {displayValue(value)}</code>)}
    </div>}
  </div>;
}
