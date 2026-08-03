import type { Translator } from '@cyber/i18n';
import type { AgentState, ImmutableEvidence, ScopeSnapshot } from '@cyber/protocol';

export type InspectorTab = 'agents' | 'scope' | 'evidence';
export type AgentInspectorProps = {
  agents: Readonly<Record<string, AgentState>>;
  scope: ScopeSnapshot | null;
  evidence: Readonly<Record<string, ImmutableEvidence>>;
  activeTab: InspectorTab;
  t: Translator;
  onTabChange: (tab: InspectorTab) => void;
};

export function AgentInspector({ agents, scope, evidence, activeTab, t, onTabChange }: AgentInspectorProps) {
  const tabs: { id: InspectorTab; label: string; count: number }[] = [
    { id: 'agents', label: t.t('inspector.agents'), count: Object.keys(agents).length },
    { id: 'scope', label: t.t('inspector.scope'), count: scope ? 1 : 0 },
    { id: 'evidence', label: t.t('inspector.evidence'), count: Object.keys(evidence).length },
  ];
  return <section className="cyber-inspector">
    <div role="tablist" aria-label={t.t('inspector.label')}>
      {tabs.map((tab) => <button
        key={tab.id}
        type="button"
        role="tab"
        id={`inspector-tab-${tab.id}`}
        aria-controls={`inspector-panel-${tab.id}`}
        aria-selected={activeTab === tab.id}
        tabIndex={activeTab === tab.id ? 0 : -1}
        onClick={() => onTabChange(tab.id)}
      >{tab.label} <span aria-label={`${tab.count}`}>{tab.count}</span></button>)}
    </div>
    <div role="tabpanel" id={`inspector-panel-${activeTab}`} aria-labelledby={`inspector-tab-${activeTab}`}>
      {activeTab === 'agents' && <ul>{Object.values(agents).map((agent) => <li key={agent.id}>{agent.name}: {agent.status}</li>)}</ul>}
      {activeTab === 'scope' && <p>{scope?.targets.join(', ') ?? t.t('common.none')}</p>}
      {activeTab === 'evidence' && <ul>{Object.values(evidence).map((item) => <li key={item.id}>{item.summary}</li>)}</ul>}
    </div>
  </section>;
}
