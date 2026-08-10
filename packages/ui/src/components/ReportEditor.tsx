import { useState, type CSSProperties } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

import type { Translator } from '@cyber/i18n';
import type { FindingState, ImmutableEvidence, ReportState } from '@cyber/protocol';

export type ReportSection = { id: string; title: string; level: 1 | 2 | 3 };

const slugify = (title: string, index: number): string => {
  const slug = title.trim().toLocaleLowerCase().replace(/\s+/gu, '-').replace(/[^\p{L}\p{N}_-]/gu, '');
  return slug || `section-${index + 1}`;
};

export function extractReportSections(narrative: string): ReportSection[] {
  const seen = new Map<string, number>();
  const sections: ReportSection[] = [];
  const headingPattern = /^(#{1,3})\s+(.+?)\s*#*\s*$/gmu;
  let match: RegExpExecArray | null;
  while ((match = headingPattern.exec(narrative)) !== null) {
    const level = match[1].length as 1 | 2 | 3;
    const title = match[2].trim();
    const base = slugify(title, sections.length);
    const count = seen.get(base) ?? 0;
    seen.set(base, count + 1);
    sections.push({ id: count === 0 ? base : `${base}-${count + 1}`, title, level });
  }
  return sections;
}

export type ReportEditorProps = {
  report: ReportState;
  findings: Readonly<Record<string, FindingState>>;
  evidence: Readonly<Record<string, ImmutableEvidence>>;
  generatedAt?: string;
  t: Translator;
  onNotesChange: (notes: string) => void;
  onRecommendationsChange: (recommendations: string) => void;
  onExcludeFinding: (findingId: string, reason: string) => void;
  onFreeze: () => void;
  onExport: (format: 'markdown' | 'html' | 'pdf' | 'json') => void;
  freezeDisabled?: boolean;
};

function reportNarrative(narrative: string): string {
  const heading = narrative.search(/^#{1,6}\s+/m);
  if (heading <= 0) return narrative;
  const preamble = narrative.slice(0, heading);
  return /工具调用预算已用尽/u.test(preamble) ? narrative.slice(heading).trim() : narrative;
}

export function ReportEditor({ report, findings, evidence, generatedAt, t, onNotesChange, onRecommendationsChange, onExcludeFinding, onFreeze, onExport, freezeDisabled = false }: ReportEditorProps) {
  const frozen = report.status === 'frozen';
  const sections = extractReportSections(reportNarrative(report.narrative));
  const includedFindings = report.findings.filter((entry) => entry.included);
  const confirmedFindings = includedFindings.filter((entry) => entry.finding.status === 'confirmed');
  const evidenceCoverage = includedFindings.length === 0 ? 0 : Math.round((includedFindings.filter((entry) => entry.evidence.length > 0).length / includedFindings.length) * 100);
  const severityCounts = ['critical', 'high', 'medium', 'low', 'info'].map((severity) => ({ severity, count: includedFindings.filter((entry) => entry.finding.severity === severity).length })).filter((entry) => entry.count > 0);
  const generatedDate = generatedAt ? new Date(generatedAt) : null;
  const hasGeneratedTime = generatedDate !== null && !Number.isNaN(generatedDate.getTime());
  const generatedTime = hasGeneratedTime
    ? new Intl.DateTimeFormat(t.locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(generatedDate)
    : t.t('report.timeUnknown');
  const initiallyExcluded = report.findings.find((entry) => !entry.included);
  const [excluded, setExcluded] = useState<string | null>(initiallyExcluded?.finding.id ?? null);
  const [reason, setReason] = useState(initiallyExcluded?.exclusionReason ?? '');
  const [format, setFormat] = useState<'markdown' | 'html' | 'pdf' | 'json'>('json');
  const updateReason = (value: string) => {
    setReason(value);
    if (excluded) onExcludeFinding(excluded, value);
  };
  let renderedHeading = 0;
  const headingId = () => sections[renderedHeading++]?.id;
  return <section className="cyber-report-editor cyber-glass" data-testid="report-editor" aria-labelledby="report-editor-title">
    <header className="cyber-report-header">
      <h2 id="report-editor-title">{t.t('report.assessmentTitle')} · {hasGeneratedTime ? <time dateTime={generatedAt}>{generatedTime}</time> : generatedTime}</h2>
      <p>{t.t('report.summary', { confirmed: confirmedFindings.length, findings: includedFindings.length, evidence: Object.keys(evidence).length })}</p>
    </header>
    <div className="cyber-report-layout">
      <aside className="cyber-report-sidebar">
        {sections.length > 0 && <nav className="cyber-report-sections" aria-label={t.t('report.sections')}><strong>{t.t('report.sections')}</strong><ul>{sections.map((section) => <li key={section.id} data-level={section.level}><a href={`#${section.id}`}>{section.title}</a></li>)}</ul></nav>}
        <div className="cyber-report-charts">
          <section className="cyber-report-chart" aria-labelledby="report-severity-title"><h3 id="report-severity-title">{t.t('report.severity')}</h3><div role="img" aria-label={t.t('report.severity')} className="cyber-severity-bars">{severityCounts.length > 0 ? severityCounts.map((entry) => <div className="cyber-severity-row" key={entry.severity}><span>{entry.severity[0].toUpperCase() + entry.severity.slice(1)}</span><i style={{ '--cyber-bar-size': `${Math.max(12, (entry.count / Math.max(1, includedFindings.length)) * 100)}%` } as CSSProperties} /><b>{entry.count}</b></div>) : <span>{t.t('common.none')}</span>}</div></section>
          <section className="cyber-report-chart" aria-labelledby="report-coverage-title"><h3 id="report-coverage-title">{t.t('report.evidenceCoverage', { percent: evidenceCoverage })}</h3><div role="img" aria-label={t.t('report.evidenceCoverage', { percent: evidenceCoverage })} className="cyber-coverage-meter"><i style={{ '--cyber-bar-size': `${evidenceCoverage}%` } as CSSProperties} /></div></section>
        </div>
      </aside>
      <div className="cyber-report-body">
        <div className="cyber-report-narrative">
          <span className="cyber-provenance">{t.t('evidence.generated')}</span>
          <div className="cyber-report-markdown">
            <Markdown
              remarkPlugins={[remarkGfm]}
              skipHtml
              components={{
                table: ({ children, ...props }) => <div className="cyber-table-scroll"><table {...props}>{children}</table></div>,
                h1: ({ children, ...props }) => <h1 id={headingId()} {...props}>{children}</h1>,
                h2: ({ children, ...props }) => <h2 id={headingId()} {...props}>{children}</h2>,
                h3: ({ children, ...props }) => <h3 id={headingId()} {...props}>{children}</h3>,
              }}
            >{reportNarrative(report.narrative)}</Markdown>
          </div>
        </div>
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
          {Object.values(evidence).map((item) => <pre className="cyber-mono" key={item.id} tabIndex={0}>{JSON.stringify(item.data, null, 2)}</pre>)}
        </section>
      </div>
    </div>
    <div className="cyber-actions">
      <button type="button" disabled={freezeDisabled || (Boolean(excluded) && reason.trim() === '')} onClick={onFreeze}>{t.t('report.freeze')}</button>
      <select aria-label={t.t('report.export')} value={format} onChange={(event) => setFormat(event.currentTarget.value as 'markdown' | 'html' | 'pdf' | 'json')}>
        <option value="json">JSON</option><option value="markdown">Markdown</option><option value="html">HTML</option><option value="pdf">PDF</option>
      </select>
      <button type="button" disabled={report.status !== 'frozen'} onClick={() => onExport(format)}>{t.t('report.export')}</button>
    </div>
  </section>;
}
