import { useEffect, useState } from 'react';

import type { Translator } from '@cyber/i18n';
import { exportReport, freezeReport, validateReport, type FrozenReport, type ProductState, type ReportFormat, type ReportState } from '@cyber/protocol';
import type { RuntimeSourceMetadata } from '@cyber/runtime-client';
import { ReportEditor } from '@cyber/ui';

const createDraft = (product: ProductState): ReportState => ({
  id: product.report?.id ?? 'report-1',
  taskId: product.task?.id ?? '',
  version: product.report?.version ?? 0,
  status: 'draft',
  narrative: Object.values(product.findings).some((finding) => finding.status === 'confirmed')
    ? 'Confirmed Findings are supported by bounded verification.'
    : 'Active verification was not performed; conclusions retain explicit limitations.',
  recommendations: '',
  humanNotes: '',
  findings: Object.values(product.findings).map((finding) => ({
    finding: structuredClone(finding),
    evidence: finding.evidenceIds.flatMap((id) => product.evidence[id] ? [structuredClone(product.evidence[id])] : []),
    included: true,
  })),
});

const reportGeneratedAt = (product: ProductState, reportId: string): string | undefined => {
  for (let index = product.timeline.length - 1; index >= 0; index -= 1) {
    const event = product.timeline[index];
    if (event?.kind === 'known' && event.type === 'report.drafted' && event.payload.report.id === reportId) return event.occurredAt;
  }
  return undefined;
};

export function ReportsPage({ product, source = null, t }: { product: ProductState; source?: RuntimeSourceMetadata | null; t: Translator }) {
  const [report, setReport] = useState<ReportState>(() => product.report ?? createDraft(product));
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    if (!dirty && report.status === 'draft') setReport(product.report ?? createDraft(product));
  }, [dirty, product, report.status]);
  const validation = validateReport(report, product.evidence);
  const setExclusion = (findingId: string, reason: string) => {
    setDirty(true);
    setReport((current) => ({
      ...current,
      findings: current.findings.map((entry) => entry.finding.id === findingId
        ? { ...entry, included: reason === '', exclusionReason: reason || undefined }
        : entry),
    }));
  };
  const freeze = () => { setDirty(true); setReport(freezeReport(report, product.evidence)); };
  const download = async (format: ReportFormat) => {
    if (report.status !== 'frozen') return;
    const blob = await exportReport(report as FrozenReport, format, { source });
    if (typeof URL.createObjectURL !== 'function') return;
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `${report.id}-v${report.version}.${format === 'markdown' ? 'md' : format}`;
    anchor.click();
    URL.revokeObjectURL(url);
  };

  const confirmed = report.findings.some((entry) => entry.finding.status === 'confirmed');
  const generatedAt = reportGeneratedAt(product, report.id);
  const sourceLabel = source ? `${source.mode.charAt(0).toUpperCase()}${source.mode.slice(1)} · ${source.runtimeId}` : t.t('common.none');
  return <section className="page page-reports"><h1>{t.t('nav.reports')}</h1>
    <p className="report-source-context">{t.t('runtime.label')}: <strong>{sourceLabel}</strong></p>
    <section className={`report-integrity ${confirmed ? 'cyber-status-success' : 'cyber-status-warning'}`} aria-label={confirmed ? t.t('report.verifiedImpact') : t.t('report.verificationLimitation')}>
      <strong>{confirmed ? t.t('report.verifiedImpact') : t.t('report.verificationLimitation')}</strong>
      {!confirmed && report.findings.map((entry) => entry.finding.rejectionReason && <p key={entry.finding.id}>{entry.finding.rejectionReason}</p>)}
    </section>
    {report.status === 'frozen' && <p role="status">Report version {report.version} · {report.taskId}</p>}
    <p className="phone-only">{t.t('phone.desktopRequired')}</p>
    <div className="desktop-report-editor"><ReportEditor
        report={report}
        findings={product.findings}
        evidence={product.evidence}
        generatedAt={generatedAt}
        t={t}
        freezeDisabled={!validation.valid || report.status === 'frozen'}
        onNotesChange={(humanNotes) => { setDirty(true); setReport((current) => ({ ...current, humanNotes })); }}
        onRecommendationsChange={(recommendations) => { setDirty(true); setReport((current) => ({ ...current, recommendations })); }}
        onExcludeFinding={setExclusion}
        onFreeze={freeze}
        onExport={(format) => void download(format)}
      /></div>
    {!validation.valid && <p role="alert">{t.t('report.validationFailed')}</p>}
  </section>;
}
