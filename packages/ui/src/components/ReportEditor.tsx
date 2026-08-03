import { useState } from 'react';

import type { Translator } from '@cyber/i18n';
import type { FindingState, ImmutableEvidence, ReportState } from '@cyber/protocol';

export type ReportEditorProps = {
  report: ReportState;
  findings: Readonly<Record<string, FindingState>>;
  evidence: Readonly<Record<string, ImmutableEvidence>>;
  t: Translator;
  onNotesChange: (notes: string) => void;
  onRecommendationsChange: (recommendations: string) => void;
  onExcludeFinding: (findingId: string, reason: string) => void;
  onFreeze: () => void;
  onExport: (format: 'markdown' | 'html' | 'pdf' | 'json') => void;
};

export function ReportEditor({ report, findings, evidence, t, onNotesChange, onRecommendationsChange, onExcludeFinding, onFreeze, onExport }: ReportEditorProps) {
  const [excluded, setExcluded] = useState<string | null>(null);
  const [reason, setReason] = useState('');
  const updateReason = (value: string) => {
    setReason(value);
    if (excluded) onExcludeFinding(excluded, value);
  };
  return <section className="cyber-report-editor" aria-labelledby="report-editor-title">
    <h2 id="report-editor-title">{t.t('report.title')} {report.id}</h2>
    <label>{t.t('report.notes')}<textarea onChange={(event) => onNotesChange(event.currentTarget.value)} /></label>
    <label>{t.t('report.recommendations')}<textarea onChange={(event) => onRecommendationsChange(event.currentTarget.value)} /></label>
    <fieldset>
      <legend>{t.t('finding.title')}</legend>
      {Object.values(findings).map((finding) => <label key={finding.id}>
        <input type="checkbox" checked={excluded === finding.id} onChange={(event) => {
          setExcluded(event.currentTarget.checked ? finding.id : null);
          if (!event.currentTarget.checked) setReason('');
        }} />
        {t.t('report.exclude')}: {finding.title}
      </label>)}
    </fieldset>
    {excluded && <label>{t.t('report.exclusionReason')}<input value={reason} onChange={(event) => updateReason(event.currentTarget.value)} /></label>}
    <section aria-label={t.t('evidence.raw')}>
      <h3>{t.t('evidence.raw')}</h3>
      <p>{t.t('evidence.immutable')}</p>
      {Object.values(evidence).map((item) => <pre key={item.id} tabIndex={0}>{JSON.stringify(item.data, null, 2)}</pre>)}
    </section>
    <div className="cyber-actions">
      <button type="button" disabled={Boolean(excluded) && reason.trim() === ''} onClick={onFreeze}>{t.t('report.freeze')}</button>
      <select aria-label={t.t('report.export')} defaultValue="json" onChange={(event) => onExport(event.currentTarget.value as 'markdown' | 'html' | 'pdf' | 'json')}>
        <option value="json">JSON</option><option value="markdown">Markdown</option><option value="html">HTML</option><option value="pdf">PDF</option>
      </select>
    </div>
  </section>;
}
