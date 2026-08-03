import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import 'vitest-axe/extend-expect';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { axe } from 'vitest-axe';
import { describe, expect, test, vi } from 'vitest';

import { createTranslator } from '@cyber/i18n';
import type { ApprovalState, FindingState, ImmutableEvidence, ScopeSnapshot } from '@cyber/protocol';

import {
  AgentInspector,
  ApprovalCard,
  CommandPalette,
  ConnectionBanner,
  ControlLeaseBanner,
  EvidenceDrawer,
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

  test('shows normalized approval fields and requires a deliberate keyboard chord', async () => {
    const onApprovalDecision = vi.fn();
    render(<ApprovalCard approval={approval} t={t} onApprovalDecision={onApprovalDecision} />);

    expect(screen.getByText('approval-1')).toBeInTheDocument();
    expect(screen.getByText('sha256:abc')).toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    screen.getByRole('article').focus();
    await userEvent.keyboard('{Control>}{Enter}{/Control}');
    expect(onApprovalDecision).toHaveBeenCalledWith('approval-1', 'allow_once');
    await userEvent.click(screen.getByRole('button', { name: 'Deny' }));
    expect(onApprovalDecision).toHaveBeenCalledWith('approval-1', 'deny');
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
      report={{ id: 'report-1', version: 1 }}
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

    const results = await axe(container, { rules: { 'color-contrast': { enabled: false } } });
    expect(results.violations).toEqual([]);
  });
});
