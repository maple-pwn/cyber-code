import { describe, expect, test } from 'vitest';

import { initialProductState, project, validateEvent, type ProductState, type RawProductEvent } from './index';

const raw = (cursor: number, type: string, payload: Record<string, unknown>): RawProductEvent => ({
  schemaVersion: 1,
  eventId: `event-${cursor}`,
  taskId: 'task-1',
  cursor,
  occurredAt: '2026-08-06T00:00:00Z',
  type,
  source: { runtimeId: 'runtime-local' },
  payload,
});

const apply = (state: ProductState, cursor: number, type: string, payload: Record<string, unknown>) => project(state, validateEvent(raw(cursor, type, payload))).state;

const target = {
  id: 'asset:target:juice-shop.lab',
  kind: 'target',
  label: 'juice-shop.lab',
  status: 'active',
  attributes: { hostname: 'juice-shop.lab' },
  provenance: { kind: 'evidence', evidenceIds: ['evidence-1'] },
};
const service = {
  id: 'asset:service:juice-shop.lab:443',
  kind: 'service',
  label: 'HTTPS 443',
  status: 'active',
  attributes: { port: 443, transport: 'tcp' },
  provenance: { kind: 'evidence', evidenceIds: ['evidence-1'] },
};
const edge = {
  id: 'edge:target-service:juice-shop.lab:443',
  kind: 'exposes',
  sourceId: target.id,
  targetId: service.id,
  directed: true,
  provenance: { kind: 'evidence', evidenceIds: ['evidence-1'] },
};

describe('asset graph protocol', () => {
  test('projects evidence-backed nodes and typed causal edges', () => {
    let state = apply(initialProductState(), 1, 'evidence.committed', { evidence: { id: 'evidence-1', taskId: 'task-1', kind: 'network', summary: '443 open', data: { port: 443 } } });
    state = apply(state, 2, 'asset.node.committed', { node: target });
    state = apply(state, 3, 'asset.node.committed', { node: service });
    state = apply(state, 4, 'asset.edge.committed', { edge });

    expect(state.assetNodes).toEqual({ [target.id]: target, [service.id]: service });
    expect(state.assetEdges).toEqual({ [edge.id]: edge });
    expect(Object.isFrozen(state.assetEdges[edge.id])).toBe(true);
  });

  test('retains an edge with an unresolved endpoint without fabricating a node', () => {
    let state = apply(initialProductState(), 1, 'evidence.committed', { evidence: { id: 'evidence-1', taskId: 'task-1', kind: 'network', summary: 'redirect observed', data: {} } });
    state = apply(state, 2, 'asset.node.committed', { node: target });
    state = apply(state, 3, 'asset.edge.committed', { edge: { ...edge, id: 'edge-unresolved', targetId: 'asset:route:unknown', kind: 'routes_to' } });

    expect(state.assetEdges['edge-unresolved'].targetId).toBe('asset:route:unknown');
    expect(state.assetNodes['asset:route:unknown']).toBeUndefined();
  });

  test('deduplicates identical asset identities and rejects conflicting reuse', () => {
    let state = apply(initialProductState(), 1, 'evidence.committed', { evidence: { id: 'evidence-1', taskId: 'task-1', kind: 'network', summary: '443 open', data: {} } });
    state = apply(state, 2, 'asset.node.committed', { node: target });
    const same = apply(state, 3, 'asset.node.committed', { node: target });
    expect(same.assetNodes).toEqual(state.assetNodes);
    expect(() => apply(same, 4, 'asset.node.committed', { node: { ...target, label: 'conflict' } })).toThrow('asset_node_conflict');

    state = apply(same, 4, 'asset.edge.committed', { edge });
    const sameEdge = apply(state, 5, 'asset.edge.committed', { edge });
    expect(sameEdge.assetEdges).toEqual(state.assetEdges);
    expect(() => apply(sameEdge, 6, 'asset.edge.committed', { edge: { ...edge, kind: 'routes_to' } })).toThrow('asset_edge_conflict');
  });

  test('keeps revoked and deleted nodes inspectable', () => {
    let state = apply(initialProductState(), 1, 'evidence.committed', { evidence: { id: 'evidence-1', taskId: 'task-1', kind: 'credential', summary: 'credential observed', data: {} } });
    state = apply(state, 2, 'asset.node.committed', { node: { ...target, kind: 'credential' } });
    state = apply(state, 3, 'asset.node.status.changed', { nodeId: target.id, status: 'revoked', reason: 'rotated' });
    expect(state.assetNodes[target.id]).toMatchObject({ status: 'revoked', statusReason: 'rotated' });
    state = apply(state, 4, 'asset.node.status.changed', { nodeId: target.id, status: 'deleted', reason: 'removed_from_scope' });
    expect(state.assetNodes[target.id]).toMatchObject({ status: 'deleted', statusReason: 'removed_from_scope' });
  });

  test('requires auditable provenance and valid graph envelopes', () => {
    for (const event of [
      raw(1, 'asset.node.committed', { node: { ...target, provenance: { kind: 'evidence', evidenceIds: [] } } }),
      raw(1, 'asset.edge.committed', { edge: { ...edge, provenance: { kind: 'human', annotationId: '', author: 'operator' } } }),
      raw(1, 'asset.edge.committed', { edge: { ...edge, sourceId: edge.targetId } }),
      raw(1, 'asset.node.status.changed', { nodeId: target.id, status: 'active', reason: 'rollback' }),
    ]) expect(() => validateEvent(event)).toThrow('invalid_event');
  });

  test('rejects evidence provenance that does not reference committed Evidence', () => {
    expect(() => apply(initialProductState(), 1, 'asset.node.committed', { node: target })).toThrow('asset_evidence_missing');
  });
});
