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

export type AssetNeighbor = { edgeId: string; nodeId: string; unresolved: boolean };
export type AssetNeighborSummary = { nodeId: string; incoming: AssetNeighbor[]; outgoing: AssetNeighbor[] };
export type AssetPosition = { nodeId: string; x: number; y: number };
export type AssetLayout = { positions: AssetPosition[]; truncated: boolean };

export function createAssetGraph(product: Pick<ProductState, 'assetNodes' | 'assetEdges'>): AssetGraph {
  const nodes = Object.values(product.assetNodes).sort((left, right) => left.id.localeCompare(right.id));
  const edges = Object.values(product.assetEdges).sort((left, right) => left.id.localeCompare(right.id));
  const nodeIds = new Set(nodes.map((node) => node.id));
  const unresolvedNodeIds = [...new Set(edges.flatMap((edge) => [edge.sourceId, edge.targetId]).filter((id) => !nodeIds.has(id)))].sort((left, right) => left.localeCompare(right));
  return { nodes, edges, unresolvedNodeIds };
}

export function filterAssetGraph(graph: AssetGraph, filter: AssetGraphFilter = {}): AssetGraph {
  const query = filter.query?.trim().toLocaleLowerCase();
  const kinds = filter.kinds && new Set(filter.kinds);
  const statuses = filter.statuses && new Set(filter.statuses);
  const nodes = graph.nodes.filter((node) => {
    if (kinds && !kinds.has(node.kind)) return false;
    if (statuses && !statuses.has(node.status)) return false;
    if (query && ![node.id, node.kind, node.label].some((value) => value.toLocaleLowerCase().includes(query))) return false;
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
  const maxNodes = Math.max(1, Math.min(options.maxNodes ?? 500, 5_000));
  const ordered = [...graph.nodes].sort((left, right) => hash(`${options.seed}:${left.id}`) - hash(`${options.seed}:${right.id}`) || left.id.localeCompare(right.id));
  const visible = ordered.slice(0, maxNodes);
  const columns = Math.max(1, Math.ceil(Math.sqrt(visible.length)));
  return {
    positions: visible.map((node, index) => ({ nodeId: node.id, x: 96 + (index % columns) * 224, y: 80 + Math.floor(index / columns) * 132 })),
    truncated: ordered.length > visible.length,
  };
}

function hash(value: string): number {
  let result = 2166136261;
  for (let index = 0; index < value.length; index += 1) result = Math.imul(result ^ value.charCodeAt(index), 16777619);
  return result >>> 0;
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
