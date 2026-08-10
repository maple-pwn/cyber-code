import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test, vi } from 'vitest';

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

  test('explains when no asset nodes have been committed', () => {
    render(<AssetGraphPage product={initialProductState()} t={createTranslator('zh-CN')} />);
    expect(screen.getByRole('status')).toHaveTextContent('暂无已提交资产节点');
  });

  test('exposes the report as the final result node', async () => {
    const user = userEvent.setup();
    const product = initialProductState();
    product.report = { id: 'report-result', taskId: 'task-1', version: 1, status: 'frozen', narrative: '# Assessment', recommendations: '', humanNotes: '', findings: [] };
    const onOpenReport = vi.fn();
    render(<AssetGraphPage product={product} t={createTranslator('zh-CN')} onOpenReport={onOpenReport} />);

    const resultNode = screen.getByRole('button', { name: /报告结果/ });
    resultNode.focus();
    await user.keyboard('{Enter}');
    expect(onOpenReport).toHaveBeenCalledOnce();
  });

  test('summarizes the attack surface and expands evidence on demand', async () => {
    const user = userEvent.setup();
    const product = initialProductState();
    product.assetNodes.target = { id: 'target', kind: 'target', label: '127.0.0.1', status: 'active', attributes: {}, provenance: { kind: 'evidence', evidenceIds: ['e-1'] } };
    product.assetNodes.service = { id: 'service', kind: 'service', label: 'HTTP :3000', status: 'active', attributes: {}, provenance: { kind: 'evidence', evidenceIds: ['e-1'] } };
    product.assetNodes.endpoint = { id: 'endpoint', kind: 'endpoint', label: 'GET /metrics', status: 'active', attributes: { url: 'http://127.0.0.1:3000/metrics', status_code: 200 }, provenance: { kind: 'evidence', evidenceIds: ['e-1'] } };
    product.evidence['e-1'] = { id: 'e-1', taskId: 'task-1', kind: 'http', summary: 'Metrics returned 200', data: {} };
    product.findings['f-1'] = { id: 'f-1', title: 'Metrics exposed', severity: 'medium', status: 'confirmed', confidence: 'runtime-verified', evidenceIds: ['e-1'] };

    render(<AssetGraphPage product={product} t={createTranslator('zh-CN')} />);

    expect(screen.getByRole('region', { name: '攻击面摘要' })).toHaveTextContent('1 个目标');
    expect(screen.getByRole('region', { name: '攻击面摘要' })).toHaveTextContent('1 个端点');
    expect(screen.queryByRole('button', { name: /Metrics returned 200/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole('checkbox', { name: '显示证据节点' }));
    expect(screen.getByRole('button', { name: /Metrics returned 200/ })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '图例' })).toHaveTextContent('发现');

    await user.click(screen.getByRole('button', { name: /GET \/metrics/ }));
    expect(screen.getByRole('region', { name: '节点详情' })).toHaveTextContent('http://127.0.0.1:3000/metrics');
  });
});
