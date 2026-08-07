import { describe, expect, test } from 'vitest';

import { initialProductState, type AssetEdgeState, type AssetNodeState } from '@cyber/protocol';

import { assetGraphDigest, createAssetGraph, filterAssetGraph, layoutAssetGraph, neighborSummary } from './index';

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

  test('uses a deterministic seed and caps layout work for huge graphs', () => {
    const nodes = Object.fromEntries(Array.from({ length: 1_000 }, (_, index) => [`node-${index}`, node(`node-${index}`)]));
    const graph = createAssetGraph({ ...initialProductState(), assetNodes: nodes });

    const first = layoutAssetGraph(graph, { seed: 'task-1', maxNodes: 200 });
    const replay = layoutAssetGraph(graph, { seed: 'task-1', maxNodes: 200 });
    const otherSeed = layoutAssetGraph(graph, { seed: 'task-2', maxNodes: 200 });

    expect(first.positions).toHaveLength(200);
    expect(first.truncated).toBe(true);
    expect(first).toEqual(replay);
    expect(first.positions).not.toEqual(otherSeed.positions);
  });
});
