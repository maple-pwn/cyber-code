import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test } from 'vitest';

import { createTranslator } from '@cyber/i18n';
import { initialProductState, type ProductState } from '@cyber/protocol';

import { FindingsPage } from './FindingsPage';
import { ReportsPage } from './ReportsPage';

const t = createTranslator('en');
const product = (): ProductState => ({
  ...initialProductState(),
  task: { id: 'task-1', title: 'Authorized assessment', status: 'completed' },
  evidence: {
    'evidence-1': { id: 'evidence-1', taskId: 'task-1', kind: 'verification', summary: 'Verified authentication bypass', data: { status: 200 } },
    'evidence-other': { id: 'evidence-other', taskId: 'task-1', kind: 'header', summary: 'Server header observed', data: { server: 'Express' } },
  },
  findings: {
    'finding-1': { id: 'finding-1', title: 'Login injection', severity: 'high', status: 'confirmed', confidence: 'high', evidenceIds: ['evidence-1'] },
  },
});

describe('Finding, Evidence, and Reports pages', () => {
  test('groups supporting Evidence and renders unassociated facts as Other Evidence', () => {
    render(<FindingsPage product={product()} t={t} />);
    expect(screen.getByTestId('finding-card')).toHaveClass('cyber-glass');
    expect(screen.getByRole('heading', { name: 'Login injection' })).toBeInTheDocument();
    expect(screen.getByText('Verified authentication bypass')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Other Evidence' })).toBeInTheDocument();
    expect(screen.getByText('Server header observed')).toBeInTheDocument();
    expect(screen.getByTestId('raw-evidence')).toHaveClass('cyber-mono');
    expect(screen.getAllByText('Generated').length).toBeGreaterThan(0);
    expect(screen.queryByDisplayValue(/Express/)).not.toBeInTheDocument();
  });

  test('requires an audited exclusion reason, labels authorship, and freezes a valid report', async () => {
    render(<ReportsPage product={product()} t={t} />);
    expect(screen.getByTestId('report-editor')).toHaveClass('cyber-glass');
    expect(screen.getByText('Verified impact')).toBeInTheDocument();
    expect(screen.getByText('Generated')).toBeInTheDocument();
    expect(screen.getByText('Human note')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: /Exclude Finding/ }));
    expect(screen.getByRole('button', { name: 'Freeze report' })).toBeDisabled();
    await userEvent.type(screen.getByLabelText('Exclusion reason (required for audit)'), 'Duplicate coverage');
    expect(screen.getByRole('button', { name: 'Freeze report' })).toBeEnabled();
    await userEvent.click(screen.getByRole('button', { name: 'Freeze report' }));
    expect(screen.getByText(/Report version 1/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Analysis notes/)).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Export' })).toBeEnabled();
  });

  test('presents a denied verification as an explicit limitation', () => {
    const denied = product();
    denied.findings['finding-1'] = { ...denied.findings['finding-1']!, status: 'rejected', rejectionReason: 'Verification denied by operator' };
    render(<ReportsPage product={denied} t={t} />);
    expect(screen.getByText('Verification limitation')).toBeInTheDocument();
    expect(screen.getByText('Verification denied by operator')).toBeInTheDocument();
  });
});
