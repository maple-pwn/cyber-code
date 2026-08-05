import { PDFDocument, StandardFonts, rgb } from 'pdf-lib';

import type { FrozenReport, ImmutableEvidence, ReportState } from './index';

export type ReportFormat = 'markdown' | 'html' | 'pdf' | 'json';
export type ReportValidation = { valid: true } | { valid: false; errors: string[] };
export type ReportAuditMetadata = {
  source: {
    mode: 'demo' | 'local' | 'remote';
    runtimeId: string;
    principal: string;
    capabilities: readonly string[];
  } | null;
};

const canonical = (value: unknown): string => JSON.stringify(value, (_key, item: unknown) =>
  item && typeof item === 'object' && !Array.isArray(item)
    ? Object.fromEntries(Object.entries(item as Record<string, unknown>).sort(([left], [right]) => left.localeCompare(right)))
    : item);

const deepFreeze = <T>(value: T): T => {
  if (value && typeof value === 'object' && !Object.isFrozen(value)) {
    Object.freeze(value);
    for (const child of Object.values(value as object)) deepFreeze(child);
  }
  return value;
};

export function validateReport(
  report: ReportState,
  evidenceById: Readonly<Record<string, ImmutableEvidence>>,
): ReportValidation {
  const errors: string[] = [];
  if (!report.id.trim()) errors.push('report_id_required');
  if (!report.taskId.trim()) errors.push('task_id_required');
  if (report.status === 'frozen' && !Object.isFrozen(report)) errors.push('frozen_report_is_mutable');

  const findingIds = new Set<string>();
  for (const entry of report.findings) {
    if (findingIds.has(entry.finding.id)) errors.push(`duplicate_finding:${entry.finding.id}`);
    findingIds.add(entry.finding.id);
    if (!entry.included && !entry.exclusionReason?.trim()) {
      errors.push(`exclusion_reason_required:${entry.finding.id}`);
    }
    if (entry.included && entry.evidence.length === 0) {
      errors.push(`evidence_required:${entry.finding.id}`);
    }
    for (const snapshot of entry.evidence) {
      const trusted = evidenceById[snapshot.id];
      if (!trusted) {
        errors.push(`evidence_missing:${snapshot.id}`);
        continue;
      }
      if (trusted.taskId !== report.taskId) errors.push(`evidence_wrong_task:${snapshot.id}`);
      if (!entry.finding.evidenceIds.includes(snapshot.id)) errors.push(`evidence_not_linked:${snapshot.id}`);
      if (canonical(snapshot) !== canonical(trusted)) errors.push(`evidence_snapshot_mismatch:${snapshot.id}`);
    }
    if (entry.included) {
      for (const evidenceId of entry.finding.evidenceIds) {
        if (!entry.evidence.some((snapshot) => snapshot.id === evidenceId)) errors.push(`evidence_reference_missing:${evidenceId}`);
      }
    }
  }
  return errors.length === 0 ? { valid: true } : { valid: false, errors };
}

export function freezeReport(
  report: ReportState,
  evidenceById: Readonly<Record<string, ImmutableEvidence>>,
): FrozenReport {
  if (report.status !== 'draft') throw new Error('report_not_draft');
  const validation = validateReport(report, evidenceById);
  if (!validation.valid) throw new Error(`report_invalid:${validation.errors.join(',')}`);
  return deepFreeze(structuredClone({ ...report, version: report.version + 1, status: 'frozen' as const }));
}

const includedFindings = (report: FrozenReport) => report.findings.filter((entry) => entry.included);
const sourceLabel = (audit: ReportAuditMetadata): string | null => audit.source === null
  ? null
  : `${audit.source.mode.charAt(0).toUpperCase()}${audit.source.mode.slice(1)} (${audit.source.runtimeId})`;

const sourceAuditLines = (audit: ReportAuditMetadata): string[] => {
  const label = sourceLabel(audit);
  if (label === null || audit.source === null) return [];
  return [
    `Runtime source: ${label}`,
    `Runtime principal: ${audit.source.principal}`,
    `Runtime capabilities: ${audit.source.capabilities.join(', ') || 'none'}`,
  ];
};

const markdownFor = (report: FrozenReport, audit: ReportAuditMetadata): string => [
  `# ${report.id}`,
  '',
  `Task: ${report.taskId}`,
  `Report version: ${report.version}`,
  ...sourceAuditLines(audit),
  '',
  report.narrative,
  '',
  '## Findings',
  ...includedFindings(report).flatMap((entry) => [
    `### ${entry.finding.title}`,
    `Severity: ${entry.finding.severity}; Status: ${entry.finding.status}`,
    ...entry.evidence.map((item) => `- Evidence ${item.id}: ${item.summary}`),
  ]),
  '',
  '## Recommendations',
  report.recommendations,
  '',
  '## Human notes',
  report.humanNotes,
].join('\n');

const escapeHtml = (value: string) => value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;');

export async function exportReport(
  report: FrozenReport,
  format: ReportFormat,
  audit: ReportAuditMetadata = { source: null },
): Promise<Blob> {
  if (report.status !== 'frozen' || !Object.isFrozen(report)) throw new Error('report_not_frozen');
  if (format === 'json') return new Blob([`${JSON.stringify({ report, audit }, null, 2)}\n`], { type: 'application/json' });
  if (format === 'markdown') return new Blob([markdownFor(report, audit)], { type: 'text/markdown' });
  if (format === 'html') {
    const findings = includedFindings(report).map((entry) => `<article><h2>${escapeHtml(entry.finding.title)}</h2><p>${escapeHtml(entry.finding.severity)} / ${escapeHtml(entry.finding.status)}</p></article>`).join('');
    const source = sourceAuditLines(audit).map((line) => `<p>${escapeHtml(line)}</p>`).join('');
    const html = `<!doctype html><html><body><h1>${escapeHtml(report.id)}</h1><p>Task ${escapeHtml(report.taskId)}</p><p>Report version ${report.version}</p>${source}${findings}</body></html>`;
    return new Blob([html], { type: 'text/html' });
  }

  const document = await PDFDocument.create();
  const label = sourceLabel(audit);
  if (label !== null && audit.source !== null) {
    document.setSubject(`${label}; principal ${audit.source.principal}; capabilities ${audit.source.capabilities.join(', ') || 'none'}`);
  }
  const page = document.addPage([612, 792]);
  const font = await document.embedFont(StandardFonts.Helvetica);
  const lines = [report.id, `Task: ${report.taskId}`, `Report version: ${report.version}`, ...sourceAuditLines(audit), ...includedFindings(report).map((entry) => `${entry.finding.severity.toUpperCase()}: ${entry.finding.title}`)];
  lines.forEach((line, index) => page.drawText(line.slice(0, 90), { x: 48, y: 740 - index * 24, size: index === 0 ? 18 : 11, font, color: rgb(0.1, 0.12, 0.14) }));
  const bytes = await document.save();
  return new Blob([Uint8Array.from(bytes).buffer], { type: 'application/pdf' });
}
