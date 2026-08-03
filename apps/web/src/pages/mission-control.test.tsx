import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test, vi } from 'vitest';

import { createTranslator } from '@cyber/i18n';
import { initialProductState, type ApprovalState, type ProductState } from '@cyber/protocol';
import type { RuntimeView } from '@cyber/runtime-client';

import { MissionControlPage } from './MissionControlPage';

const t = createTranslator('en');
const approval: ApprovalState = {
  id: 'approval-1', agentId: 'agent-1', action: 'bounded-login-verification',
  target: 'juice-shop.lab', parameterDigest: 'sha256:abc', risk: 'medium',
  expiresAt: '2026-08-03T12:05:00.000Z',
};
const product = (leaseClient = 'web-client'): ProductState => ({
  ...initialProductState(),
  task: { id: 'task-1', title: 'Authorized assessment', status: 'running' },
  scope: {
    id: 'scope-1', principal: 'operator', workspace: '/labs/juice-shop', validity: 'single-task',
    targets: ['juice-shop.lab'], allowedActions: ['passive-recon'], deniedActions: ['destructive'], riskCeiling: 'medium',
  },
  controlLease: { clientId: leaseClient, revision: 3 },
  highestCommittedLeaseRevision: 3,
  agents: { 'agent-1': { id: 'agent-1', name: 'Recon Agent', status: 'running' } },
  approvals: { [approval.id]: approval },
});
const view = (status: RuntimeView['connection']['status'], leaseClient = 'web-client'): RuntimeView => ({
  connection: { status, lastTrustedCursor: 12 },
  product: product(leaseClient),
});

describe('MissionControlPage', () => {
  test('renders operational landmarks and dispatches exact task, instruction, and approval commands', async () => {
    const onDispatch = vi.fn().mockResolvedValue(undefined);
    render(<MissionControlPage view={view('healthy')} t={t} onDispatch={onDispatch} onReconnect={vi.fn()} />);

    expect(screen.getByRole('banner')).toBeInTheDocument();
    expect(screen.getByRole('complementary', { name: 'Mission inspector' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Agents/ })).toHaveAttribute('aria-selected', 'true');
    await userEvent.click(screen.getByRole('button', { name: 'Pause' }));
    await userEvent.click(screen.getByRole('button', { name: 'Resume' }));
    await userEvent.click(screen.getByRole('button', { name: 'Cancel task' }));
    await userEvent.type(screen.getByLabelText('Send an instruction to active agents'), 'Check headers');
    await userEvent.click(screen.getByRole('button', { name: 'Send instruction' }));
    const approvalCard = screen.getByRole('article');
    await userEvent.click(within(approvalCard).getByRole('button', { name: 'Allow once' }));

    expect(onDispatch.mock.calls.map(([command]) => command)).toEqual(expect.arrayContaining([
      { type: 'task.pause' },
      { type: 'task.resume' },
      { type: 'task.cancel' },
      { type: 'instruction.send', content: 'Check headers' },
      { type: 'approval.respond', challengeId: 'approval-1', decision: 'allow_once' },
    ]));
  });

  test('keeps cached state read-only offline and supports explicit reconnect', async () => {
    const onDispatch = vi.fn();
    const onReconnect = vi.fn();
    render(<MissionControlPage view={view('offline')} t={t} onDispatch={onDispatch} onReconnect={onReconnect} />);

    expect(screen.getByText('Authorized assessment')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Pause' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Allow once' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Take control' })).toBeDisabled();
    expect(screen.getByLabelText('Send an instruction to active agents')).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Reconnect' }));
    expect(onReconnect).toHaveBeenCalledOnce();
    expect(onDispatch).not.toHaveBeenCalled();
  });

  test('makes a displaced controller read-only until explicit takeover', async () => {
    const onDispatch = vi.fn();
    render(<MissionControlPage view={view('healthy', 'desktop-client')} t={t} onDispatch={onDispatch} onReconnect={vi.fn()} clientId="web-client" />);

    expect(screen.getByRole('button', { name: 'Pause' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Allow once' })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Take control' }));
    expect(onDispatch).toHaveBeenCalledWith({ type: 'control.take', expectedRevision: 3 });
  });
});
