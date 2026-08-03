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
  freezeDisabled?: boolean;
};

export function ReportEditor({ report, findings, evidence, t, onNotesChange, onRecommendationsChange, onExcludeFinding, onFreeze, onExport, freezeDisabled = false }: ReportEditorProps) {
  const frozen = report.status === 'frozen';
  const initiallyExcluded = report.findings.find((entry) => !entry.included);
  const [excluded, setExcluded] = useState<string | null>(initiallyExcluded?.finding.id ?? null);
  const [reason, setReason] = useState(initiallyExcluded?.exclusionReason ?? '');
  const [format, setFormat] = useState<'markdown' | 'html' | 'pdf' | 'json'>('json');
  const updateReason = (value: string) => {
    setReason(value);
    if (excluded) onExcludeFinding(excluded, value);
  };
  return <section className="cyber-report-editor" aria-labelledby="report-editor-title">
    <h2 id="report-editor-title">{t.t('report.title')} {report.id}</h2>
    <p><span className="cyber-provenance">{t.t('evidence.generated')}</span> {report.narrative}</p>
    <label>{t.t('report.notes')} <span className="cyber-provenance">{t.t('evidence.human')}</span><textarea value={report.humanNotes} disabled={frozen} onChange={(event) => onNotesChange(event.currentTarget.value)} /></label>
    <label>{t.t('report.recommendations')}<textarea value={report.recommendations} disabled={frozen} onChange={(event) => onRecommendationsChange(event.currentTarget.value)} /></label>
    <fieldset>
      <legend>{t.t('finding.title')}</legend>
      {Object.values(findings).map((finding) => <label key={finding.id}>
        <input type="checkbox" checked={excluded === finding.id} disabled={frozen} onChange={(event) => {
          setExcluded(event.currentTarget.checked ? finding.id : null);
          if (!event.currentTarget.checked) { setReason(''); onExcludeFinding(finding.id, ''); }
        }} />
        {t.t('report.exclude')}: {finding.title}
      </label>)}
    </fieldset>
    {excluded && <label>{t.t('report.exclusionReason')}<input value={reason} disabled={frozen} onChange={(event) => updateReason(event.currentTarget.value)} /></label>}
    <section aria-label={t.t('evidence.raw')}>
      <h3>{t.t('evidence.raw')}</h3>
      <p>{t.t('evidence.immutable')}</p>
      {Object.values(evidence).map((item) => <pre key={item.id} tabIndex={0}>{JSON.stringify(item.data, null, 2)}</pre>)}
    </section>
    <div className="cyber-actions">
      <button type="button" disabled={freezeDisabled || (Boolean(excluded) && reason.trim() === '')} onClick={onFreeze}>{t.t('report.freeze')}</button>
      <select aria-label={t.t('report.export')} value={format} onChange={(event) => setFormat(event.currentTarget.value as 'markdown' | 'html' | 'pdf' | 'json')}>
        <option value="json">JSON</option><option value="markdown">Markdown</option><option value="html">HTML</option><option value="pdf">PDF</option>
      </select>
      <button type="button" disabled={report.status !== 'frozen'} onClick={() => onExport(format)}>{t.t('report.export')}</button>
    </div>
  </section>;
}
