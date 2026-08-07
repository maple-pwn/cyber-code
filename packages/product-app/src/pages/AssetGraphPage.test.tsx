import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test } from 'vitest';

import { initialProductState } from '@cyber/protocol';
import { createTranslator } from '@cyber/i18n';

import { AssetGraphPage } from './AssetGraphPage';

describe('AssetGraphPage', () => {
  test('exposes filters, text alternative, unresolved endpoints and snapshot export', () => {
    const product = initialProductState();
    product.assetNodes['host-1'] = { id: 'host-1', kind: 'host', label: 'Gateway', status: 'active', attributes: {}, provenance: { kind: 'evidence', evidenceIds: ['e-1'] } };
    product.assetEdges['edge-1'] = { id: 'edge-1', kind: 'connects', sourceId: 'host-1', targetId: 'unknown-1', directed: true, provenance: { kind: 'evidence', evidenceIds: ['e-1'] } };
    render(<AssetGraphPage product={product} t={createTranslator('zh-CN')} />);
    expect(screen.getByRole('heading', { name: '资产关系图' })).toBeInTheDocument();
    expect(screen.getByLabelText('搜索资产')).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '资产关系图文本视图' })).toHaveTextContent('unknown-1');
    expect(screen.getByRole('button', { name: '导出关系图快照' })).toBeInTheDocument();
  });

  test('renders deterministic nodes and drills into committed evidence by keyboard', async () => {
	const user = userEvent.setup();
	const product = initialProductState();
	product.evidence['e-1'] = { id: 'e-1', taskId: 'task-1', kind: 'network', summary: 'Observed gateway', data: {} };
	product.assetNodes['host-1'] = { id: 'host-1', kind: 'host', label: 'Gateway', status: 'active', attributes: {}, provenance: { kind: 'evidence', evidenceIds: ['e-1'] } };
	render(<AssetGraphPage product={product} t={createTranslator('zh-CN')} />);
	const node = screen.getByRole('button', { name: /Gateway/ });
	node.focus(); await user.keyboard('{Enter}');
	expect(screen.getByRole('region', { name: '资产证据来源' })).toHaveTextContent('Observed gateway');
  });
});
