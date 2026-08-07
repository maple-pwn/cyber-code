import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, test, vi } from 'vitest';

import { createTranslator } from '@cyber/i18n';
import { initialProductState, type ProductState } from '@cyber/protocol';

import type { AppStore } from '../app-store';
import { FindingsPage } from './FindingsPage';

const product: ProductState = {
  ...initialProductState(),
  task: { id: 'task-1', title: 'Review', status: 'running' },
  scope: { id: 'scope-1', principal: 'operator', workspace: '/workspace', validity: 'single-task', targets: ['/workspace'], allowedActions: ['editor.open', 'editor.write'], deniedActions: [], riskCeiling: 'low' },
  controlLease: { clientId: 'client-1', revision: 1 },
  findings: { 'finding-1': { id: 'finding-1', title: 'Unsafe call', severity: 'high', status: 'confirmed', confidence: 'high', evidenceIds: ['evidence-1'] } },
  evidence: { 'evidence-1': { id: 'evidence-1', taskId: 'task-1', kind: 'file', summary: 'Unsafe call at line 4', data: { path: '/workspace/main.go', startLine: 4, endLine: 8 } } },
};

test('reports editor open failures without navigating to an unprojected draft', async () => {
  const dispatch = vi.fn().mockRejectedValue(new Error('stale lease'));
  const onOpenEditor = vi.fn();
  render(<FindingsPage product={product} t={createTranslator('zh-CN')} store={{ dispatch } as unknown as AppStore} editorAvailable onOpenEditor={onOpenEditor} />);

  await userEvent.click(screen.getByRole('button', { name: '证据编辑器' }));

  expect(await screen.findByRole('alert')).toHaveTextContent('无法打开证据编辑器');
  expect(onOpenEditor).not.toHaveBeenCalled();
});
