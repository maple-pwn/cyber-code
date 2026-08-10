import type { AssetEdgeState, AssetNodeState, ProductState } from '@cyber/protocol';

export type AssetGraph = {
  nodes: AssetNodeState[];
  edges: AssetEdgeState[];
  unresolvedNodeIds: string[];
};

export type AssetGraphFilter = {
  query?: string;
  kinds?: readonly string[];
  statuses?: readonly AssetNodeState['status'][];
};

export type AssessmentGraphOptions = {
  includeEvidence?: boolean;
  includeReceipts?: boolean;
};

export type AssetNeighbor = { edgeId: string; nodeId: string; unresolved: boolean };
export type AssetNeighborSummary = { nodeId: string; incoming: AssetNeighbor[]; outgoing: AssetNeighbor[] };
export type AssetPosition = { nodeId: string; x: number; y: number; layer: number };
export type AssetLayout = { positions: AssetPosition[]; truncated: boolean };

export function createAssetGraph(product: Pick<ProductState, 'assetNodes' | 'assetEdges'>): AssetGraph {
  const nodes = Object.values(product.assetNodes).sort((left, right) => left.id.localeCompare(right.id));
  const edges = Object.values(product.assetEdges).sort((left, right) => left.id.localeCompare(right.id));
  const nodeIds = new Set(nodes.map((node) => node.id));
  const unresolvedNodeIds = [...new Set(edges.flatMap((edge) => [edge.sourceId, edge.targetId]).filter((id) => !nodeIds.has(id)))].sort((left, right) => left.localeCompare(right));
  return { nodes, edges, unresolvedNodeIds };
}

export function createAssessmentGraph(product: ProductState, options: AssessmentGraphOptions = {}): AssetGraph {
  const nodes = new Map(createAssetGraph(product).nodes.map((node) => [node.id, node]));
  const edges = new Map(createAssetGraph(product).edges.map((edge) => [edge.id, edge]));

  for (const finding of Object.values(product.findings)) {
    const id = `finding-result:${finding.id}`;
    nodes.set(id, {
      id,
      kind: 'finding',
      label: finding.title,
      status: finding.status === 'rejected' ? 'revoked' : 'active',
      attributes: { findingId: finding.id, severity: finding.severity, confidence: finding.confidence, findingStatus: finding.status },
      provenance: finding.evidenceIds.length > 0
        ? { kind: 'evidence', evidenceIds: [...finding.evidenceIds] }
        : { kind: 'human', annotationId: finding.id, author: 'cyber-agent' },
    });
  }

  if (options.includeEvidence) {
    for (const evidence of Object.values(product.evidence)) {
      const evidenceNodeId = `evidence-result:${evidence.id}`;
      nodes.set(evidenceNodeId, {
        id: evidenceNodeId,
        kind: 'evidence',
        label: evidence.summary,
        status: 'active',
        attributes: { evidenceId: evidence.id, evidenceKind: evidence.kind, ...evidence.data },
        provenance: { kind: 'evidence', evidenceIds: [evidence.id] },
      });
      for (const assetNode of Object.values(product.assetNodes)) {
        if (assetNode.kind !== 'endpoint' || assetNode.provenance.kind !== 'evidence' || !assetNode.provenance.evidenceIds.includes(evidence.id)) continue;
        const edgeId = `edge:observed-at:${evidence.id}:${assetNode.id}`;
        edges.set(edgeId, derivedEdge(edgeId, 'observed_at', evidenceNodeId, assetNode.id, [evidence.id]));
      }
      for (const finding of Object.values(product.findings)) {
        if (!finding.evidenceIds.includes(evidence.id)) continue;
        const edgeId = `edge:supports:${evidence.id}:${finding.id}`;
        edges.set(edgeId, derivedEdge(edgeId, 'supports', evidenceNodeId, `finding-result:${finding.id}`, [evidence.id]));
      }
    }
  }

  if (product.report) {
    const reportNodeId = `report-result:${product.report.id}`;
    nodes.set(reportNodeId, {
      id: reportNodeId,
      kind: 'report',
      label: product.report.status === 'frozen' ? 'Final report' : 'Draft report',
      status: 'active',
      attributes: { reportId: product.report.id, version: product.report.version, reportStatus: product.report.status },
      provenance: { kind: 'human', annotationId: product.report.id, author: 'cyber-agent' },
    });
    for (const item of product.report.findings) {
      if (!item.included) continue;
      const findingNodeId = `finding-result:${item.finding.id}`;
      if (!nodes.has(findingNodeId)) continue;
      const edgeId = `edge:included-in:${item.finding.id}:${product.report.id}`;
      const evidenceIds = item.finding.evidenceIds;
      edges.set(edgeId, evidenceIds.length > 0
        ? derivedEdge(edgeId, 'included_in', findingNodeId, reportNodeId, evidenceIds)
        : { id: edgeId, kind: 'included_in', sourceId: findingNodeId, targetId: reportNodeId, directed: true, provenance: { kind: 'human', annotationId: product.report.id, author: 'cyber-agent' } });
    }
  }

  const orderedNodes = [...nodes.values()].sort((left, right) => left.id.localeCompare(right.id));
  const orderedEdges = [...edges.values()].sort((left, right) => left.id.localeCompare(right.id));
  const nodeIds = new Set(orderedNodes.map((node) => node.id));
  const unresolvedNodeIds = [...new Set(orderedEdges.flatMap((edge) => [edge.sourceId, edge.targetId]).filter((id) => !nodeIds.has(id)))].sort((left, right) => left.localeCompare(right));
  return { nodes: orderedNodes, edges: orderedEdges, unresolvedNodeIds };
}

function derivedEdge(id: string, kind: string, sourceId: string, targetId: string, evidenceIds: string[]): AssetEdgeState {
  return { id, kind, sourceId, targetId, directed: true, provenance: { kind: 'evidence', evidenceIds: [...evidenceIds] } };
}

export function filterAssetGraph(graph: AssetGraph, filter: AssetGraphFilter = {}): AssetGraph {
  const query = filter.query?.trim().toLocaleLowerCase();
  const kinds = filter.kinds && new Set(filter.kinds);
  const statuses = filter.statuses && new Set(filter.statuses);
  const nodes = graph.nodes.filter((node) => {
    if (kinds && !kinds.has(node.kind)) return false;
    if (statuses && !statuses.has(node.status)) return false;
    if (query && ![node.id, node.kind, node.label, JSON.stringify(node.attributes)].some((value) => value.toLocaleLowerCase().includes(query))) return false;
    return true;
  });
  const nodeIds = new Set(nodes.map((node) => node.id));
  const originalNodeIds = new Set(graph.nodes.map((node) => node.id));
  const edges = graph.edges.filter((edge) => {
    const sourceKnown = originalNodeIds.has(edge.sourceId);
    const targetKnown = originalNodeIds.has(edge.targetId);
    return (!sourceKnown || nodeIds.has(edge.sourceId)) && (!targetKnown || nodeIds.has(edge.targetId));
  });
  const unresolvedNodeIds = [...new Set(edges.flatMap((edge) => [edge.sourceId, edge.targetId]).filter((id) => !nodeIds.has(id)))].sort((left, right) => left.localeCompare(right));
  return { nodes, edges, unresolvedNodeIds };
}

export function neighborSummary(graph: AssetGraph, nodeId: string): AssetNeighborSummary {
  const known = new Set(graph.nodes.map((node) => node.id));
  const incoming: AssetNeighbor[] = [];
  const outgoing: AssetNeighbor[] = [];
  for (const edge of graph.edges) {
    if (edge.sourceId === nodeId) outgoing.push({ edgeId: edge.id, nodeId: edge.targetId, unresolved: !known.has(edge.targetId) });
    if (edge.targetId === nodeId) incoming.push({ edgeId: edge.id, nodeId: edge.sourceId, unresolved: !known.has(edge.sourceId) });
    if (!edge.directed && edge.sourceId === nodeId) incoming.push({ edgeId: edge.id, nodeId: edge.targetId, unresolved: !known.has(edge.targetId) });
    if (!edge.directed && edge.targetId === nodeId) outgoing.push({ edgeId: edge.id, nodeId: edge.sourceId, unresolved: !known.has(edge.sourceId) });
  }
  return { nodeId, incoming: incoming.sort(compareNeighbor), outgoing: outgoing.sort(compareNeighbor) };
}

function compareNeighbor(left: AssetNeighbor, right: AssetNeighbor): number {
  return left.edgeId.localeCompare(right.edgeId) || left.nodeId.localeCompare(right.nodeId);
}

export function layoutAssetGraph(graph: AssetGraph, options: { seed: string; maxNodes?: number }): AssetLayout {
  void options.seed;
  const maxNodes = Math.max(1, Math.min(options.maxNodes ?? 500, 5_000));
  const ordered = [...graph.nodes].sort(compareLayeredNode);
  const selectedIds = new Set(
    [...graph.nodes]
      .sort((left, right) => retentionPriority(left) - retentionPriority(right) || compareLayeredNode(left, right))
      .slice(0, maxNodes)
      .map((node) => node.id),
  );
  const visible = ordered.filter((node) => selectedIds.has(node.id));
  const layerOffsets = new Map<number, number>();
  return {
    positions: visible.map((node) => {
      const layer = semanticLayer(node.kind);
      const offset = layerOffsets.get(layer) ?? 0;
      layerOffsets.set(layer, offset + 1);
      return { nodeId: node.id, x: 110 + layer * 250, y: 86 + offset * 116, layer };
    }),
    truncated: ordered.length > visible.length,
  };
}

function semanticLayer(kind: string): number {
  return ({ target: 0, host: 0, service: 1, endpoint: 2, finding: 3, evidence: 4, receipt: 4, report: 5 } as Record<string, number>)[kind] ?? 2;
}

function retentionPriority(node: AssetNodeState): number {
  return ({ report: 0, target: 1, host: 1, service: 2, endpoint: 3, finding: 4, evidence: 6, receipt: 7 } as Record<string, number>)[node.kind] ?? 5;
}

function compareLayeredNode(left: AssetNodeState, right: AssetNodeState): number {
  const layerDelta = semanticLayer(left.kind) - semanticLayer(right.kind);
  if (layerDelta !== 0) return layerDelta;
  if (left.kind === 'finding' && right.kind === 'finding') {
    const severityRank: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3, info: 4 };
    const severityDelta = (severityRank[String(left.attributes.severity).toLowerCase()] ?? 5) - (severityRank[String(right.attributes.severity).toLowerCase()] ?? 5);
    if (severityDelta !== 0) return severityDelta;
  }
  return left.label.localeCompare(right.label) || left.id.localeCompare(right.id);
}

export async function assetGraphDigest(graph: AssetGraph): Promise<string> {
  const encoded = new TextEncoder().encode(stableJson(graph));
  const digest = await globalThis.crypto.subtle.digest('SHA-256', encoded);
  return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, '0')).join('');
}

function stableJson(value: unknown): string {
  return JSON.stringify(value, (_key, item: unknown) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return item;
    return Object.fromEntries(Object.entries(item as Record<string, unknown>).sort(([left], [right]) => left.localeCompare(right)));
  });
}
