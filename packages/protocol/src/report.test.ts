import { describe, expect, test } from 'vitest';

import type { FindingState, ImmutableEvidence, ReportState } from './index';
import { exportReport, freezeReport, validateReport } from './report';

const evidence: ImmutableEvidence = {
  id: 'evidence-1',
  taskId: 'task-1',
  kind: 'verification',
  summary: 'Bounded verification confirmed impact',
  data: { impact: 'authentication bypass' },
};
const finding: FindingState = {
  id: 'finding-1',
  title: 'Login injection',
  severity: 'high',
  status: 'confirmed',
  confidence: 'high',
  evidenceIds: [evidence.id],
};
const draft = (overrides: Partial<ReportState> = {}): ReportState => ({
  id: 'report-1',
  taskId: 'task-1',
  version: 0,
  status: 'draft',
  narrative: 'Generated assessment narrative',
  recommendations: 'Use parameterized queries.',
  humanNotes: '',
  findings: [{ finding, evidence: [evidence], included: true }],
  ...overrides,
});
const readBlob = (blob: Blob): Promise<ArrayBuffer> => new Promise((resolve, reject) => {
  const reader = new FileReader();
  reader.onerror = () => reject(reader.error);
  reader.onload = () => resolve(reader.result as ArrayBuffer);
  reader.readAsArrayBuffer(blob);
});
const readText = async (blob: Blob) => new TextDecoder().decode(await readBlob(blob));

describe('report integrity', () => {
  test('validates a same-task draft and freezes immutable snapshots without mutating it', () => {
    const report = draft();
    expect(validateReport(report, { [evidence.id]: evidence })).toEqual({ valid: true });

    const frozen = freezeReport(report, { [evidence.id]: evidence });
    expect(frozen).toMatchObject({ id: 'report-1', taskId: 'task-1', version: 1, status: 'frozen' });
    expect(report).toMatchObject({ version: 0, status: 'draft' });
    expect(Object.isFrozen(frozen)).toBe(true);
    expect(Object.isFrozen(frozen.findings[0]?.evidence[0])).toBe(true);
  });

  test('rejects cross-task Evidence and edited raw Evidence snapshots', () => {
    const crossTask = { ...evidence, taskId: 'task-2' };
    expect(validateReport(draft(), { [evidence.id]: crossTask })).toEqual({
      valid: false,
      errors: expect.arrayContaining(['evidence_wrong_task:evidence-1']),
    });
    const edited = draft({ findings: [{ finding, evidence: [{ ...evidence, data: { impact: 'edited' } }], included: true }] });
    expect(validateReport(edited, { [evidence.id]: evidence })).toEqual({
      valid: false,
      errors: expect.arrayContaining(['evidence_snapshot_mismatch:evidence-1']),
    });
  });

  test('requires an exclusion reason and refuses a mutable draft presented as frozen', () => {
    const excluded = draft({ findings: [{ finding, evidence: [evidence], included: false }] });
    expect(validateReport(excluded, { [evidence.id]: evidence })).toEqual({
      valid: false,
      errors: expect.arrayContaining(['exclusion_reason_required:finding-1']),
    });
    expect(validateReport({ ...draft(), status: 'frozen', version: 1 }, { [evidence.id]: evidence })).toEqual({
      valid: false,
      errors: expect.arrayContaining(['frozen_report_is_mutable']),
    });
  });

  test('exports deterministic Markdown, HTML, JSON, and a real PDF', async () => {
    const frozen = freezeReport(draft(), { [evidence.id]: evidence });
    const markdown = await exportReport(frozen, 'markdown');
    const html = await exportReport(frozen, 'html');
    const json = await exportReport(frozen, 'json');
    const pdf = await exportReport(frozen, 'pdf');

    expect(markdown.type).toBe('text/markdown');
    expect(await readText(markdown)).toContain('Task: task-1');
    expect(html.type).toBe('text/html');
    expect(await readText(html)).toContain('Report version 1');
    expect(json.type).toBe('application/json');
    expect(await readText(json)).toContain('"taskId": "task-1"');
    const bytes = new Uint8Array(await readBlob(pdf));
    expect(new TextDecoder('latin1').decode(bytes.slice(0, 5))).toBe('%PDF-');
  });
});
