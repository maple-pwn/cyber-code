import { describe, expect, test } from 'vitest';

import { initialProductState, type AssetEdgeState, type AssetNodeState } from '@cyber/protocol';

import { assetGraphDigest, createAssessmentGraph, createAssetGraph, filterAssetGraph, layoutAssetGraph, neighborSummary } from './index';

const provenance = { kind: 'human' as const, annotationId: 'annotation-1', author: 'operator' };
const node = (id: string, kind = 'target', status: AssetNodeState['status'] = 'active'): AssetNodeState => ({ id, kind, label: id, status, attributes: {}, provenance });
const edge = (id: string, sourceId: string, targetId: string): AssetEdgeState => ({ id, kind: 'related_to', sourceId, targetId, directed: true, provenance });

describe('asset graph model', () => {
  test('produces the same ordered snapshot and digest for equivalent projections', async () => {
    const left = { ...initialProductState(), assetNodes: { b: node('b', 'service'), a: node('a') }, assetEdges: { z: edge('z', 'a', 'b') } };
    const right = { ...initialProductState(), assetNodes: { a: node('a'), b: node('b', 'service') }, assetEdges: { z: edge('z', 'a', 'b') } };

    const leftGraph = createAssetGraph(left);
    const rightGraph = createAssetGraph(right);

    expect(leftGraph.nodes.map((item) => item.id)).toEqual(['a', 'b']);
    expect(leftGraph).toEqual(rightGraph);
    expect(await assetGraphDigest(leftGraph)).toBe(await assetGraphDigest(rightGraph));
  });

  test('keeps unresolved endpoints explicit in neighbor summaries', () => {
    const graph = createAssetGraph({ ...initialProductState(), assetNodes: { a: node('a') }, assetEdges: { edge: edge('edge', 'a', 'missing') } });

    expect(graph.unresolvedNodeIds).toEqual(['missing']);
    expect(neighborSummary(graph, 'a')).toEqual({ nodeId: 'a', incoming: [], outgoing: [{ edgeId: 'edge', nodeId: 'missing', unresolved: true }] });
  });

  test('filters by query, kind, and status without inventing connected nodes', () => {
    const graph = createAssetGraph({
      ...initialProductState(),
      assetNodes: { api: { ...node('api', 'service'), label: 'Public API' }, credential: node('credential', 'credential', 'revoked') },
      assetEdges: { uses: edge('uses', 'api', 'credential') },
    });

    const filtered = filterAssetGraph(graph, { query: 'public', kinds: ['service'], statuses: ['active'] });
    expect(filtered.nodes.map((item) => item.id)).toEqual(['api']);
    expect(filtered.edges).toEqual([]);
    expect(filtered.unresolvedNodeIds).toEqual([]);
  });

  test('composes findings, evidence, and the frozen report onto durable assets', () => {
    const product = initialProductState();
    product.assetNodes.target = node('target', 'target');
    product.assetNodes.service = node('service', 'service');
    product.assetNodes.endpoint = { ...node('endpoint', 'endpoint'), provenance: { kind: 'evidence', evidenceIds: ['e-1'] } };
    product.assetEdges.exposes = edge('exposes', 'target', 'service');
    product.assetEdges.hosts = edge('hosts', 'service', 'endpoint');
    product.evidence['e-1'] = { id: 'e-1', taskId: 'task-1', kind: 'http', summary: 'GET /metrics returned 200', data: { target: 'http://127.0.0.1:3000/metrics' } };
    product.findings['f-1'] = { id: 'f-1', title: 'Metrics exposed', severity: 'medium', status: 'confirmed', confidence: 'runtime-verified', evidenceIds: ['e-1'] };
    product.report = { id: 'r-1', taskId: 'task-1', version: 1, status: 'frozen', narrative: '# Report', recommendations: '', humanNotes: '', findings: [{ finding: product.findings['f-1'], evidence: [product.evidence['e-1']], included: true }] };

    const graph = createAssessmentGraph(product, { includeEvidence: true });

    expect(graph.nodes.map((item) => item.id)).toEqual(expect.arrayContaining(['target', 'service', 'endpoint', 'finding-result:f-1', 'evidence-result:e-1', 'report-result:r-1']));
    expect(graph.edges).toEqual(expect.arrayContaining([
      expect.objectContaining({ kind: 'observed_at', sourceId: 'evidence-result:e-1', targetId: 'endpoint' }),
      expect.objectContaining({ kind: 'supports', sourceId: 'evidence-result:e-1', targetId: 'finding-result:f-1' }),
      expect.objectContaining({ kind: 'included_in', sourceId: 'finding-result:f-1', targetId: 'report-result:r-1' }),
    ]));
  });

  test('collapses evidence details without fabricating endpoint relationships', () => {
    const product = initialProductState();
    product.evidence['e-1'] = { id: 'e-1', taskId: 'task-1', kind: 'http', summary: 'Unbound observation', data: {} };
    product.findings['f-1'] = { id: 'f-1', title: 'Observed issue', severity: 'low', status: 'confirmed', confidence: 'runtime-verified', evidenceIds: ['e-1'] };

    const collapsed = createAssessmentGraph(product);
    const expanded = createAssessmentGraph(product, { includeEvidence: true });

    expect(collapsed.nodes.map((item) => item.id)).toEqual(['finding-result:f-1']);
    expect(expanded.edges.some((item) => item.kind === 'observed_at')).toBe(false);
  });

  test('uses a deterministic seed and caps layout work for huge graphs', () => {
    const nodes = Object.fromEntries(Array.from({ length: 1_000 }, (_, index) => [`node-${index}`, node(`node-${index}`)]));
    const graph = createAssetGraph({ ...initialProductState(), assetNodes: nodes });

    const first = layoutAssetGraph(graph, { seed: 'task-1', maxNodes: 200 });
    const replay = layoutAssetGraph(graph, { seed: 'task-1', maxNodes: 200 });
    const otherSeed = layoutAssetGraph(graph, { seed: 'task-2', maxNodes: 200 });

    expect(first.positions).toHaveLength(200);
    expect(first.truncated).toBe(true);
    expect(first).toEqual(replay);
    expect(first.positions).toEqual(otherSeed.positions);
  });

  test('places assessment nodes in semantic horizontal layers', () => {
    const graph = createAssetGraph({
      ...initialProductState(),
      assetNodes: {
        report: node('report', 'report'),
        finding: node('finding', 'finding'),
        endpoint: node('endpoint', 'endpoint'),
        target: node('target', 'target'),
        service: node('service', 'service'),
        evidence: node('evidence', 'evidence'),
      },
    });

    const layout = layoutAssetGraph(graph, { seed: 'task-1' });
    const layers = new Map(layout.positions.map((item) => [item.nodeId, item.layer]));

    expect(layers.get('target')).toBeLessThan(layers.get('service')!);
    expect(layers.get('service')).toBeLessThan(layers.get('endpoint')!);
    expect(layers.get('endpoint')).toBeLessThan(layers.get('finding')!);
    expect(layers.get('finding')).toBeLessThan(layers.get('evidence')!);
    expect(layers.get('evidence')).toBeLessThan(layers.get('report')!);
    expect(layout.positions.find((item) => item.nodeId === 'target')?.x).toBeLessThan(layout.positions.find((item) => item.nodeId === 'service')?.x ?? 0);
  });
});
