import { useMemo, useState } from 'react';
import { Download, Network } from 'lucide-react';

import type { Translator } from '@cyber/i18n';
import type { ProductState } from '@cyber/protocol';
import { createAssetGraph, filterAssetGraph, neighborSummary } from '@cyber/asset-graph';

export function AssetGraphPage({ product, t }: { product: ProductState; t: Translator }) {
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('all');
  const graph = useMemo(() => filterAssetGraph(createAssetGraph(product), { query, statuses: status === 'all' ? undefined : [status as 'active' | 'revoked' | 'deleted' | 'unknown'] }), [product, query, status]);
  const download = () => {
    const blob = new Blob([JSON.stringify({ nodes: graph.nodes, edges: graph.edges, unresolvedNodeIds: graph.unresolvedNodeIds }, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a'); link.href = url; link.download = 'cyber-asset-graph.json'; link.click(); URL.revokeObjectURL(url);
  };
  return <section className="page page-asset-graph">
    <header className="asset-graph-header"><div><span className="mission-eyebrow">CYBER · EVIDENCE GRAPH</span><h1>{t.t('nav.assetGraph')}</h1><p>{graph.nodes.length} nodes · {graph.edges.length} edges</p></div><button type="button" onClick={download} aria-label={t.t('assetGraph.export')} title={t.t('assetGraph.export')}><Download aria-hidden="true" size={17} /></button></header>
    <div className="asset-graph-controls"><label htmlFor="asset-graph-search">{t.t('assetGraph.search')}</label><input id="asset-graph-search" aria-label={t.t('assetGraph.search')} value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t.t('assetGraph.searchPlaceholder')} /><label htmlFor="asset-graph-status">{t.t('common.status')}</label><select id="asset-graph-status" value={status} onChange={(event) => setStatus(event.target.value)}><option value="all">{t.t('common.all')}</option><option value="active">active</option><option value="revoked">revoked</option><option value="deleted">deleted</option><option value="unknown">unknown</option></select></div>
    <div className="asset-graph-canvas" aria-label={t.t('assetGraph.canvas')} role="img"><Network aria-hidden="true" size={34} /><span>{t.t('assetGraph.canvasHint')}</span></div>
    <section className="asset-graph-text" aria-label={t.t('assetGraph.textView')} role="region"><h2>{t.t('assetGraph.textView')}</h2><ul>{graph.nodes.map((node) => { const neighbors = neighborSummary(graph, node.id); return <li key={node.id}><strong>{node.label}</strong> <code>{node.id}</code> <span>[{node.status}]</span><small>{t.t('assetGraph.neighbors')}: {neighbors.incoming.concat(neighbors.outgoing).map((item) => item.nodeId).join(', ') || t.t('common.none')}</small></li>; })}{graph.unresolvedNodeIds.map((id) => <li key={`unknown-${id}`}><strong>{id}</strong> <span>[unresolved endpoint]</span></li>)}</ul></section>
  </section>;
}
