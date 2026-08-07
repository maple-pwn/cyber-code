import { useMemo, useState } from 'react';
import { Download } from 'lucide-react';

import type { Translator } from '@cyber/i18n';
import type { ProductState } from '@cyber/protocol';
import { createAssetGraph, filterAssetGraph, layoutAssetGraph, neighborSummary } from '@cyber/asset-graph';

export function AssetGraphPage({ product, t }: { product: ProductState; t: Translator }) {
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('all');
  const [selectedNodeId, setSelectedNodeId] = useState('');
  const graph = useMemo(() => filterAssetGraph(createAssetGraph(product), { query, statuses: status === 'all' ? undefined : [status as 'active' | 'revoked' | 'deleted' | 'unknown'] }), [product, query, status]);
  const layout = useMemo(() => layoutAssetGraph(graph, { seed: product.task?.id ?? 'asset-graph', maxNodes: 500 }), [graph, product.task?.id]);
  const positions = new Map(layout.positions.map((position) => [position.nodeId, position]));
  const selectedNode = graph.nodes.find((node) => node.id === selectedNodeId) ?? null;
  const width = Math.max(320, ...layout.positions.map((position) => position.x + 120));
  const height = Math.max(220, ...layout.positions.map((position) => position.y + 80));
  const download = () => {
    const blob = new Blob([JSON.stringify({ nodes: graph.nodes, edges: graph.edges, unresolvedNodeIds: graph.unresolvedNodeIds }, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a'); link.href = url; link.download = 'cyber-asset-graph.json'; link.click(); URL.revokeObjectURL(url);
  };
  return <section className="page page-asset-graph">
    <header className="asset-graph-header"><div><span className="mission-eyebrow">CYBER · EVIDENCE GRAPH</span><h1>{t.t('nav.assetGraph')}</h1><p>{graph.nodes.length} nodes · {graph.edges.length} edges</p></div><button type="button" onClick={download} aria-label={t.t('assetGraph.export')} title={t.t('assetGraph.export')}><Download aria-hidden="true" size={17} /></button></header>
    <div className="asset-graph-controls"><label htmlFor="asset-graph-search">{t.t('assetGraph.search')}</label><input id="asset-graph-search" aria-label={t.t('assetGraph.search')} value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t.t('assetGraph.searchPlaceholder')} /><label htmlFor="asset-graph-status">{t.t('common.status')}</label><select id="asset-graph-status" value={status} onChange={(event) => setStatus(event.target.value)}><option value="all">{t.t('common.all')}</option><option value="active">active</option><option value="revoked">revoked</option><option value="deleted">deleted</option><option value="unknown">unknown</option></select></div>
    <div className="asset-graph-canvas"><svg aria-label={t.t('assetGraph.canvas')} role="img" viewBox={`0 0 ${width} ${height}`}><defs><marker id="asset-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker></defs>{graph.edges.map((edge) => { const source = positions.get(edge.sourceId); const target = positions.get(edge.targetId); return source && target ? <line key={edge.id} x1={source.x} y1={source.y} x2={target.x} y2={target.y} markerEnd={edge.directed ? 'url(#asset-arrow)' : undefined}><title>{edge.kind}</title></line> : null; })}{graph.nodes.map((node) => { const position = positions.get(node.id); if (!position) return null; return <g key={node.id} role="button" tabIndex={0} aria-label={`${node.label} ${node.kind} ${node.status}`} aria-pressed={selectedNodeId === node.id} transform={`translate(${position.x} ${position.y})`} onClick={() => setSelectedNodeId(node.id)} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); setSelectedNodeId(node.id); } }}><circle r="34" data-status={node.status} /><text textAnchor="middle" y="3">{node.label.slice(0, 18)}</text><text className="asset-node-kind" textAnchor="middle" y="50">{node.kind}</text></g>; })}</svg>{layout.truncated && <p role="status">{t.t('assetGraph.truncated')}</p>}</div>
    {selectedNode && <section className="asset-graph-provenance" role="region" aria-label={t.t('assetGraph.provenance')}><h2>{selectedNode.label}</h2>{selectedNode.provenance.kind === 'evidence' ? <ul>{selectedNode.provenance.evidenceIds.map((id) => <li key={id}><strong>{product.evidence[id]?.summary ?? id}</strong><code>{id}</code></li>)}</ul> : <p>{selectedNode.provenance.author} · {selectedNode.provenance.annotationId}</p>}</section>}
    <section className="asset-graph-text" aria-label={t.t('assetGraph.textView')} role="region"><h2>{t.t('assetGraph.textView')}</h2><ul>{graph.nodes.map((node) => { const neighbors = neighborSummary(graph, node.id); return <li key={node.id}><strong>{node.label}</strong> <code>{node.id}</code> <span>[{node.status}]</span><small>{t.t('assetGraph.neighbors')}: {neighbors.incoming.concat(neighbors.outgoing).map((item) => item.nodeId).join(', ') || t.t('common.none')}</small></li>; })}{graph.unresolvedNodeIds.map((id) => <li key={`unknown-${id}`}><strong>{id}</strong> <span>[unresolved endpoint]</span></li>)}</ul></section>
  </section>;
}
