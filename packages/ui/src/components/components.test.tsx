import { render, screen, within } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import 'vitest-axe/extend-expect';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { axe } from 'vitest-axe';
import { describe, expect, test, vi } from 'vitest';

import { createTranslator } from '@cyber/i18n';
import type { ApprovalState, FindingState, ImmutableEvidence, ScopeSnapshot, ValidatedProductEvent } from '@cyber/protocol';

import {
  AgentInspector,
  ActiveAgentRibbon,
  ApprovalCard,
  CommandPalette,
  ConnectionBanner,
  ControlLeaseBanner,
  EvidenceDrawer,
  EvidenceBackdrop,
  FindingCard,
  NarrativeStream,
  ReportEditor,
  ScopeReviewSheet,
} from './index';

const t = createTranslator('en');
const scope: ScopeSnapshot = {
  id: 'scope-1',
  principal: 'authorized-operator',
  workspace: '/labs/juice-shop',
  validity: 'single-task',
  targets: ['juice-shop.lab'],
  allowedActions: ['passive-recon'],
  deniedActions: ['destructive'],
  riskCeiling: 'medium',
};
const approval: ApprovalState = {
  id: 'approval-1',
  agentId: 'agent-verify',
  action: 'bounded-login-verification',
  target: 'juice-shop.lab',
  parameterDigest: 'sha256:abc',
  risk: 'medium',
  expiresAt: '2026-08-03T12:05:00.000Z',
};
const evidence: ImmutableEvidence = {
  id: 'evidence-1',
  taskId: 'task-1',
  kind: 'route',
  summary: 'Login route observed',
  data: { method: 'POST', path: '/rest/user/login' },
};
const finding: FindingState = {
  id: 'finding-1',
  title: 'Potential login injection',
  severity: 'high',
  status: 'confirmed',
  confidence: 'high',
  evidenceIds: [evidence.id],
};
const timelineEvent: ValidatedProductEvent = {
  schemaVersion: 1,
  eventId: 'event-1',
  taskId: 'task-1',
  cursor: 7,
  occurredAt: '2026-08-03T12:04:00.000Z',
  type: 'agent.started',
  source: { runtimeId: 'scenario-local', agentId: 'agent-verify' },
  payload: { agent: { id: 'agent-verify', name: 'Verification', status: 'running' } },
  kind: 'known',
};

describe('workflow components', () => {
  test('renders semantic scope review and explicit confirmation', async () => {
    const onConfirmScope = vi.fn();
    render(<ScopeReviewSheet
      scope={scope}
      scopeId="scope-1"
      principal="authorized-operator"
      workspace="/labs/juice-shop"
      validity="single task"
      t={t}
      onConfirmScope={onConfirmScope}
    />);

    expect(screen.getByRole('heading', { name: 'Scope Review' })).toBeInTheDocument();
    expect(screen.getByText('juice-shop.lab')).toBeInTheDocument();
    expect(screen.getByText('destructive')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Confirm scope' }));
    expect(onConfirmScope).toHaveBeenCalledWith('scope-1');
  });

  test('uses tabs without changing selection when inactive data updates', async () => {
    const onTabChange = vi.fn();
    const { rerender } = render(<AgentInspector
      agents={{ agent: { id: 'agent', name: 'Recon Agent', status: 'running' } }}
      scope={scope}
      evidence={{}}
      activeTab="agents"
      t={t}
      onTabChange={onTabChange}
    />);
    rerender(<AgentInspector
      agents={{ agent: { id: 'agent', name: 'Recon Agent', status: 'running' } }}
      scope={scope}
      evidence={{ [evidence.id]: evidence }}
      activeTab="agents"
      t={t}
      onTabChange={onTabChange}
    />);

    expect(screen.getByRole('tab', { name: /Agents/ })).toHaveAttribute('aria-selected', 'true');
    await userEvent.click(screen.getByRole('tab', { name: /Evidence/ }));
    expect(onTabChange).toHaveBeenCalledWith('evidence');
  });

  test('shows contextual Evidence without inventing an empty request', () => {
    const { rerender } = render(<EvidenceBackdrop evidence={{ [evidence.id]: evidence }} />);
    expect(screen.getByTestId('evidence-backdrop')).toHaveTextContent('POST');
    expect(screen.getByTestId('evidence-backdrop')).toHaveTextContent('/rest/user/login');
    rerender(<EvidenceBackdrop evidence={{}} />);
    expect(screen.getByTestId('evidence-backdrop')).not.toHaveTextContent('POST');
  });

  test('exposes active agent action, progress, and pending tone', async () => {
    const onSelect = vi.fn();
    render(<ActiveAgentRibbon agent={{ id: 'agent-verify', name: 'Verification', status: 'waiting', progress: 68, currentAction: 'Approval required' }} onSelect={onSelect} />);
    const ribbon = screen.getByRole('button', { name: /Verification/ });
    expect(ribbon).toHaveAttribute('data-tone', 'pending');
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '68');
    await userEvent.click(ribbon);
    expect(onSelect).toHaveBeenCalledWith('agent-verify');
  });

  test('renders dense agent action and causal event metadata', () => {
    render(<>
      <AgentInspector
        agents={{ agent: { id: 'agent', name: 'Recon Agent', status: 'running', progress: 42, currentAction: 'Inspect headers' } }}
        scope={scope}
        evidence={{}}
        activeTab="agents"
        t={t}
        onTabChange={vi.fn()}
      />
      <NarrativeStream events={[timelineEvent]} t={t} />
    </>);
    expect(screen.getByText('Inspect headers')).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: /Recon Agent/ })).toHaveAttribute('aria-valuenow', '42');
    expect(screen.getByText('agent.started')).toBeInTheDocument();
    expect(screen.getByText(/cursor 7/i)).toBeInTheDocument();
    expect(screen.getByText(/agent-verify/)).toBeInTheDocument();
    const expectedTime = new Intl.DateTimeFormat(t.locale, {
      dateStyle: 'short',
      timeStyle: 'medium',
    }).format(new Date(timelineEvent.occurredAt));
    const time = screen.getByText(expectedTime);
    expect(time).toHaveAttribute('datetime', timelineEvent.occurredAt);
    expect(time).not.toHaveTextContent(timelineEvent.occurredAt);
  });

  test('requires parameter review before allow and resets when the challenge changes', async () => {
    const onApprovalDecision = vi.fn();
    const { rerender } = render(<ApprovalCard approval={approval} t={t} onApprovalDecision={onApprovalDecision} />);

    expect(screen.queryByText('approval-1')).not.toBeInTheDocument();
    expect(screen.queryByText('sha256:abc')).not.toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Allow once' })).not.toBeInTheDocument();
    screen.getByRole('article').focus();
    await userEvent.keyboard('{Control>}{Enter}{/Control}');
    expect(onApprovalDecision).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Review parameters' }));
    expect(screen.getByText('approval-1')).toBeInTheDocument();
    expect(screen.getByText('sha256:abc')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Confirm allow once' })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Confirm allow once' }));
    expect(onApprovalDecision).toHaveBeenCalledWith('approval-1', 'allow_once');

    rerender(<ApprovalCard approval={{ ...approval, parameterDigest: 'sha256:changed' }} t={t} onApprovalDecision={onApprovalDecision} />);
    expect(screen.queryByRole('button', { name: 'Confirm allow once' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Deny' }));
    expect(onApprovalDecision).toHaveBeenCalledWith('approval-1', 'deny');
  });

  test('does not enter approval review while writes are disabled', async () => {
    render(<ApprovalCard approval={approval} t={t} disabled onApprovalDecision={vi.fn()} />);
    const review = screen.getByRole('button', { name: 'Review parameters' });
    expect(review).toBeDisabled();
    await userEvent.click(review);
    expect(screen.queryByRole('button', { name: 'Confirm allow once' })).not.toBeInTheDocument();
  });

  test('shows immutable raw Evidence in a focus-restoring dialog', async () => {
    const user = userEvent.setup();
    const Harness = () => {
      const [open, setOpen] = useState(false);
      return <>
        <button type="button" onClick={() => setOpen(true)}>Inspect Evidence</button>
        <EvidenceDrawer evidence={evidence} open={open} onOpenChange={setOpen} t={t} />
      </>;
    };
    render(<Harness />);
    const trigger = screen.getByRole('button', { name: 'Inspect Evidence' });
    await user.click(trigger);

    expect(screen.getByRole('dialog', { name: 'Evidence' })).toBeInTheDocument();
    expect(screen.getByText(/rest\/user\/login/)).toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    await user.keyboard('{Escape}');
    expect(trigger).toHaveFocus();
  });

  test('requires an audited exclusion reason before report freeze', async () => {
    const onFreeze = vi.fn();
    const onExcludeFinding = vi.fn();
    render(<ReportEditor
      report={{ id: 'report-1', taskId: 'task-1', version: 0, status: 'draft', narrative: 'Generated narrative', recommendations: '', humanNotes: '', findings: [{ finding, evidence: [evidence], included: true }] }}
      findings={{ [finding.id]: finding }}
      evidence={{ [evidence.id]: evidence }}
      t={t}
      onNotesChange={vi.fn()}
      onRecommendationsChange={vi.fn()}
      onExcludeFinding={onExcludeFinding}
      onFreeze={onFreeze}
      onExport={vi.fn()}
    />);

    await userEvent.click(screen.getByRole('checkbox', { name: /Exclude Finding/ }));
    expect(screen.getByRole('button', { name: 'Freeze report' })).toBeDisabled();
    await userEvent.type(screen.getByLabelText('Exclusion reason (required for audit)'), 'Out of agreed report scope');
    expect(screen.getByRole('button', { name: 'Freeze report' })).toBeEnabled();
    expect(onExcludeFinding).toHaveBeenLastCalledWith('finding-1', 'Out of agreed report scope');
    expect(screen.getByText(/"method": "POST"/)).toBeInTheDocument();
  });

  test('renders generated report narrative as structured Markdown', () => {
    render(<ReportEditor
      report={{ id: 'report-markdown', taskId: 'task-1', version: 1, status: 'frozen', narrative: '运行状态：工具调用预算已用尽。\n\n# Assessment\n\n| Field | Value |\n| --- | --- |\n| Status | Confirmed |\n\n```bash\ncurl http://127.0.0.1:18080\n```', recommendations: '', humanNotes: '', findings: [] }}
      findings={{}}
      evidence={{}}
      t={t}
      onNotesChange={vi.fn()}
      onRecommendationsChange={vi.fn()}
      onExcludeFinding={vi.fn()}
      onFreeze={vi.fn()}
      onExport={vi.fn()}
    />);

    expect(screen.getByRole('heading', { name: 'Assessment', level: 1 })).toBeInTheDocument();
    expect(screen.getByRole('table').parentElement).toHaveClass('cyber-table-scroll');
    expect(screen.getByRole('columnheader', { name: 'Field' })).toBeInTheDocument();
    expect(screen.getByRole('code')).toHaveTextContent('curl http://127.0.0.1:18080');
    expect(screen.queryByText(/工具调用预算已用尽/)).not.toBeInTheDocument();
  });

  test('shows the recorded report time and a deterministic summary', () => {
    const generatedAt = '2026-08-10T08:16:04+08:00';
    render(<ReportEditor
      report={{ id: 'report-summary', taskId: 'task-1', version: 1, status: 'frozen', narrative: '# Assessment', recommendations: '', humanNotes: '', findings: [{ finding, evidence: [evidence], included: true }] }}
      findings={{ [finding.id]: finding }}
      evidence={{ [evidence.id]: evidence }}
      generatedAt={generatedAt}
      t={t}
      onNotesChange={vi.fn()}
      onRecommendationsChange={vi.fn()}
      onExcludeFinding={vi.fn()}
      onFreeze={vi.fn()}
      onExport={vi.fn()}
    />);

    const expectedTime = new Intl.DateTimeFormat(t.locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(generatedAt));
    expect(screen.getByRole('heading', { name: `Security assessment report · ${expectedTime}`, level: 2 })).toBeInTheDocument();
    expect(screen.getByText('Confirmed findings 1 · Included findings 1 · Evidence 1')).toBeInTheDocument();
  });

  test('states when the report generation time was not recorded', () => {
    render(<ReportEditor
      report={{ id: 'report-without-time', taskId: 'task-1', version: 0, status: 'draft', narrative: '', recommendations: '', humanNotes: '', findings: [] }}
      findings={{}}
      evidence={{}}
      t={t}
      onNotesChange={vi.fn()}
      onRecommendationsChange={vi.fn()}
      onExcludeFinding={vi.fn()}
      onFreeze={vi.fn()}
      onExport={vi.fn()}
    />);

    expect(screen.getByRole('heading', { name: 'Security assessment report · Time not recorded', level: 2 })).toBeInTheDocument();
  });

  test('builds report section navigation from Markdown headings', () => {
    render(<ReportEditor
      report={{ id: 'report-sections', taskId: 'task-1', version: 0, status: 'draft', narrative: '# Assessment\n\n## Details\n\nEvidence text', recommendations: '', humanNotes: '', findings: [] }}
      findings={{}}
      evidence={{}}
      t={t}
      onNotesChange={vi.fn()}
      onRecommendationsChange={vi.fn()}
      onExcludeFinding={vi.fn()}
      onFreeze={vi.fn()}
      onExport={vi.fn()}
    />);

    const navigation = screen.getByRole('navigation', { name: 'Report sections' });
    expect(within(navigation).getByRole('link', { name: 'Assessment' })).toHaveAttribute('href', '#assessment');
    expect(within(navigation).getByRole('link', { name: 'Details' })).toHaveAttribute('href', '#details');
    expect(screen.getByRole('heading', { name: 'Assessment', level: 1 })).toHaveAttribute('id', 'assessment');
    expect(screen.getByRole('heading', { name: 'Details', level: 2 })).toHaveAttribute('id', 'details');
  });

  test('renders deterministic severity and evidence coverage summaries', () => {
    render(<ReportEditor
      report={{ id: 'report-charts', taskId: 'task-1', version: 0, status: 'draft', narrative: '', recommendations: '', humanNotes: '', findings: [{ finding, evidence: [evidence], included: true }] }}
      findings={{ [finding.id]: finding }}
      evidence={{ [evidence.id]: evidence }}
      t={t}
      onNotesChange={vi.fn()}
      onRecommendationsChange={vi.fn()}
      onExcludeFinding={vi.fn()}
      onFreeze={vi.fn()}
      onExport={vi.fn()}
    />);

    expect(screen.getByRole('img', { name: 'Finding severity' })).toBeInTheDocument();
    expect(screen.getByText('High')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'Evidence coverage 100%' })).toBeInTheDocument();
  });

  test('filters and dispatches command palette choices', async () => {
    const onCommand = vi.fn();
    const onOpenChange = vi.fn();
    render(<CommandPalette
      open
      commands={[{ id: 'reports', label: 'Reports' }, { id: 'scope', label: 'Scope Review' }]}
      t={t}
      onOpenChange={onOpenChange}
      onCommand={onCommand}
    />);

    await userEvent.type(screen.getByLabelText('Search pages or actions'), 'repo');
    expect(screen.queryByRole('button', { name: 'Scope Review' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Reports' }));
    expect(onCommand).toHaveBeenCalledWith('reports');
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  test('renders priority components without axe violations', async () => {
    const { container } = render(<main>
      <ConnectionBanner connection={{ status: 'offline', lastTrustedCursor: 7 }} t={t} onReconnect={vi.fn()} />
      <ControlLeaseBanner lease={{ clientId: 'web-1', revision: 2 }} t={t} onTakeControl={vi.fn()} />
      <NarrativeStream events={[]} t={t} />
      <FindingCard finding={finding} evidence={[evidence]} t={t} />
    </main>);

    expect(screen.getByRole('heading', { name: finding.title, level: 2 })).toBeInTheDocument();
    const results = await axe(container, { rules: { 'color-contrast': { enabled: false } } });
    expect(results.violations).toEqual([]);
  });

  test('offers the editor only for absolute file evidence with a valid line range', async () => {
    const onOpenEditor = vi.fn();
    const editable = { ...evidence, data: { path: '/workspace/app.go', startLine: 4, endLine: 8 } };
    const { rerender } = render(<FindingCard finding={finding} evidence={[editable]} t={t} onOpenEditor={onOpenEditor} />);

    await userEvent.click(screen.getByRole('button', { name: 'Evidence Editor' }));
    expect(onOpenEditor).toHaveBeenCalledWith(editable);

    for (const data of [
      { path: 'relative/app.go', startLine: 4, endLine: 8 },
      { path: '/workspace/app.go', startLine: 0, endLine: 8 },
      { path: '/workspace/app.go', startLine: 9, endLine: 8 },
      { path: '/workspace/app.go' },
    ]) {
      rerender(<FindingCard finding={finding} evidence={[{ ...evidence, data }]} t={t} onOpenEditor={onOpenEditor} />);
      expect(screen.queryByRole('button', { name: 'Evidence Editor' })).not.toBeInTheDocument();
    }
  });
});
