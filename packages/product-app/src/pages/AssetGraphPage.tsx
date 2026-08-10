import { useMemo, useState } from 'react';
import { Download, Eye, Network } from 'lucide-react';

import type { TranslationKey, Translator } from '@cyber/i18n';
import type { AssetNodeState, ProductState } from '@cyber/protocol';
import { createAssessmentGraph, filterAssetGraph, layoutAssetGraph, neighborSummary } from '@cyber/asset-graph';

const visibleKinds = ['target', 'service', 'endpoint', 'finding', 'evidence', 'report'] as const;
const kindTranslationKeys: Record<(typeof visibleKinds)[number], TranslationKey> = {
  target: 'assetGraph.kind.target', service: 'assetGraph.kind.service', endpoint: 'assetGraph.kind.endpoint',
  finding: 'assetGraph.kind.finding', evidence: 'assetGraph.kind.evidence', report: 'assetGraph.kind.report',
};

export function AssetGraphPage({ product, t, onOpenReport }: { product: ProductState; t: Translator; onOpenReport?: () => void }) {
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('all');
  const [kind, setKind] = useState('all');
  const [showEvidence, setShowEvidence] = useState(false);
  const [selectedNodeId, setSelectedNodeId] = useState('');
  const assessmentGraph = useMemo(() => createAssessmentGraph(product, { includeEvidence: showEvidence }), [product, showEvidence]);
  const graph = useMemo(() => filterAssetGraph(assessmentGraph, {
    query,
    kinds: kind === 'all' ? undefined : [kind],
    statuses: status === 'all' ? undefined : [status as AssetNodeState['status']],
  }), [assessmentGraph, kind, query, status]);
  const layout = useMemo(() => layoutAssetGraph(graph, { seed: product.task?.id ?? 'asset-graph', maxNodes: 500 }), [graph, product.task?.id]);
  const positions = new Map(layout.positions.map((position) => [position.nodeId, position]));
  const selectedNode = assessmentGraph.nodes.find((node) => node.id === selectedNodeId) ?? null;
  const reportNodeId = product.report ? `report-result:${product.report.id}` : '';
  const activateNode = (nodeId: string) => nodeId === reportNodeId ? onOpenReport?.() : setSelectedNodeId(nodeId);
  const width = Math.max(420, ...layout.positions.map((position) => position.x + 130));
  const height = Math.max(280, ...layout.positions.map((position) => position.y + 90));
  const counts = Object.fromEntries(visibleKinds.map((item) => [item, assessmentGraph.nodes.filter((node) => node.kind === item).length]));
  const download = () => {
    const blob = new Blob([JSON.stringify({ nodes: graph.nodes, edges: graph.edges, unresolvedNodeIds: graph.unresolvedNodeIds }, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a'); link.href = url; link.download = 'cyber-attack-surface.json'; link.click(); URL.revokeObjectURL(url);
  };

  return <section className="page page-asset-graph">
    <header className="asset-graph-header">
      <div><span className="mission-eyebrow">CYBER · ATTACK SURFACE</span><h1>{t.t('nav.assetGraph')}</h1><p>{graph.nodes.length} nodes · {graph.edges.length} edges</p></div>
      <button type="button" onClick={download} aria-label={t.t('assetGraph.export')} title={t.t('assetGraph.export')}><Download aria-hidden="true" size={17} /></button>
    </header>

    <section className="asset-graph-summary" role="region" aria-label={t.t('assetGraph.summary')}>
      <span><strong>{counts.target}</strong>{t.t('assetGraph.targets')}</span>
      <span><strong>{counts.service}</strong>{t.t('assetGraph.services')}</span>
      <span><strong>{counts.endpoint}</strong>{t.t('assetGraph.endpoints')}</span>
      <span><strong>{counts.finding}</strong>{t.t('assetGraph.findings')}</span>
      <span><strong>{Object.keys(product.evidence).length}</strong>{t.t('assetGraph.evidence')}</span>
    </section>

    <div className="asset-graph-controls">
      <label htmlFor="asset-graph-search">{t.t('assetGraph.search')}</label>
      <input id="asset-graph-search" aria-label={t.t('assetGraph.search')} value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t.t('assetGraph.searchPlaceholder')} />
      <label htmlFor="asset-graph-kind">{t.t('assetGraph.type')}</label>
      <select id="asset-graph-kind" value={kind} onChange={(event) => setKind(event.target.value)}><option value="all">{t.t('common.all')}</option>{visibleKinds.map((item) => <option key={item} value={item}>{item}</option>)}</select>
      <label htmlFor="asset-graph-status">{t.t('common.status')}</label>
      <select id="asset-graph-status" value={status} onChange={(event) => setStatus(event.target.value)}><option value="all">{t.t('common.all')}</option><option value="active">active</option><option value="revoked">revoked</option><option value="deleted">deleted</option><option value="unknown">unknown</option></select>
      <label className="asset-graph-toggle"><Eye aria-hidden="true" size={15} /><input type="checkbox" checked={showEvidence} onChange={(event) => setShowEvidence(event.target.checked)} />{t.t('assetGraph.showEvidence')}</label>
    </div>

    <section className="asset-graph-legend" role="region" aria-label={t.t('assetGraph.legend')}>
      {visibleKinds.map((item) => <span key={item} data-kind={item}><i aria-hidden="true" />{t.t(kindTranslationKeys[item])}</span>)}
    </section>

    <div className="asset-graph-workspace">
      <div className="asset-graph-canvas">{graph.nodes.length === 0
        ? <p className="asset-graph-empty" role="status">{t.t('assetGraph.empty')}</p>
        : <svg aria-label={t.t('assetGraph.canvas')} role="img" viewBox={`0 0 ${width} ${height}`}>
          <defs><marker id="asset-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" /></marker></defs>
          {graph.edges.map((edge) => { const source = positions.get(edge.sourceId); const target = positions.get(edge.targetId); return source && target ? <g className="asset-edge" key={edge.id}><line x1={source.x} y1={source.y} x2={target.x} y2={target.y} markerEnd={edge.directed ? 'url(#asset-arrow)' : undefined}><title>{edge.kind}</title></line><text x={(source.x + target.x) / 2} y={(source.y + target.y) / 2 - 7}>{edge.kind.replaceAll('_', ' ')}</text></g> : null; })}
          {graph.nodes.map((node) => { const position = positions.get(node.id); return position ? <GraphNode key={node.id} node={node} label={node.kind === 'report' ? t.t('assetGraph.reportResult') : node.label} selected={selectedNodeId === node.id} x={position.x} y={position.y} onActivate={() => activateNode(node.id)} /> : null; })}
        </svg>}
        {layout.truncated && <p role="status">{t.t('assetGraph.truncated')}</p>}
      </div>

      <aside className="asset-graph-inspector" role="region" aria-label={t.t('assetGraph.nodeDetails')}>
        {selectedNode ? <>
          <header><Network aria-hidden="true" size={17} /><div><small>{selectedNode.kind}</small><h2>{selectedNode.label}</h2></div></header>
          <dl>{Object.entries(selectedNode.attributes).map(([name, value]) => <div key={name}><dt>{name}</dt><dd>{renderAttribute(value)}</dd></div>)}</dl>
          <section role="region" aria-label={t.t('assetGraph.provenance')}>
            <h3>{t.t('assetGraph.provenance')}</h3>
            {selectedNode.provenance.kind === 'evidence' ? <ul>{selectedNode.provenance.evidenceIds.map((id) => <li key={id}><strong>{product.evidence[id]?.summary ?? id}</strong><code>{id}</code></li>)}</ul> : <p>{selectedNode.provenance.author} · {selectedNode.provenance.annotationId}</p>}
          </section>
        </> : <p>{t.t('assetGraph.selectNode')}</p>}
      </aside>
    </div>

    <section className="asset-graph-text" aria-label={t.t('assetGraph.textView')} role="region"><h2>{t.t('assetGraph.textView')}</h2><ul>{graph.nodes.map((node) => { const neighbors = neighborSummary(graph, node.id); return <li key={node.id}><strong>{node.label}</strong> <code>{node.kind}</code> <span>[{node.status}]</span><small>{t.t('assetGraph.neighbors')}: {neighbors.incoming.concat(neighbors.outgoing).map((item) => item.nodeId).join(', ') || t.t('common.none')}</small></li>; })}{graph.unresolvedNodeIds.map((id) => <li key={`unknown-${id}`}><strong>{id}</strong> <span>[unresolved endpoint]</span></li>)}</ul></section>
  </section>;
}

function GraphNode({ node, label, selected, x, y, onActivate }: { node: AssetNodeState; label: string; selected: boolean; x: number; y: number; onActivate: () => void }) {
  const common = { 'data-status': node.status, 'data-severity': String(node.attributes.severity ?? '') };
  const shape = node.kind === 'target' || node.kind === 'host'
    ? <polygon {...common} points="-48,-29 48,-29 62,0 48,29 -48,29 -62,0" />
    : node.kind === 'finding'
      ? <polygon {...common} points="0,-43 55,0 0,43 -55,0" />
      : node.kind === 'evidence'
        ? <circle {...common} r="31" />
        : node.kind === 'report'
          ? <path {...common} d="M-55-35h78l32 28v42h-110z M23-35v28h32" />
          : <rect {...common} x={node.kind === 'endpoint' ? -70 : -62} y="-29" width={node.kind === 'endpoint' ? 140 : 124} height="58" rx={node.kind === 'endpoint' ? 18 : 7} />;
  return <g role="button" tabIndex={0} aria-label={`${label} ${node.kind} ${node.status}`} aria-pressed={selected} data-node-kind={node.kind} transform={`translate(${x} ${y})`} onClick={onActivate} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onActivate(); } }}>
    <title>{label} · {node.kind} · {node.status}</title>{shape}<text textAnchor="middle" y="3">{label.length > 21 ? `${label.slice(0, 20)}…` : label}</text><text className="asset-node-kind" textAnchor="middle" y="47">{node.kind}</text>
  </g>;
}

function renderAttribute(value: unknown): string {
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
}
