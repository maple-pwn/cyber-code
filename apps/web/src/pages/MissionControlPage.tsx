import { useEffect, useRef, useState, type FormEvent } from 'react';
import { OctagonX, PanelRightOpen, Pause, Play, Send } from 'lucide-react';

import type { Translator } from '@cyber/i18n';
import type { RuntimeCommand, RuntimeView } from '@cyber/runtime-client';
import {
  AgentInspector,
  ActiveAgentRibbon,
  ApprovalCard,
  ConnectionBanner,
  ControlLeaseBanner,
  EvidenceBackdrop,
  NarrativeStream,
  type InspectorTab,
} from '@cyber/ui';

export type MissionControlPageProps = {
  view: RuntimeView;
  t: Translator;
  onDispatch: (command: RuntimeCommand) => void | Promise<void>;
  onReconnect: () => void;
  onDisconnect?: () => void;
  clientId?: string;
};

export function MissionControlPage({ view, t, onDispatch, onReconnect, onDisconnect, clientId = 'web-client' }: MissionControlPageProps) {
  const [activeTab, setActiveTab] = useState<InspectorTab>('agents');
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const inspectorTrigger = useRef<HTMLButtonElement>(null);
  const [instruction, setInstruction] = useState('');
  const transportBlocked = ['resyncing', 'offline', 'incompatible', 'unauthorized'].includes(view.connection.status);
  const displaced = view.product.controlLease !== null && view.product.controlLease.clientId !== clientId;
  const writesDisabled = transportBlocked || displaced;
  const pendingApproval = Object.values(view.product.approvals).find((approval) => !approval.decision);
  const agents = Object.values(view.product.agents);
  const selectedAgent = agents.find((agent) => agent.id === pendingApproval?.agentId)
    ?? (pendingApproval ? {
      id: pendingApproval.agentId,
      name: pendingApproval.agentId.replace(/^agent-/, '').split('-').map((part) => `${part.charAt(0).toUpperCase()}${part.slice(1)}`).join(' '),
      status: 'waiting',
      currentAction: t.t('approval.title'),
    } : null)
    ?? agents.find((agent) => /waiting|blocked/i.test(agent.status))
    ?? agents.find((agent) => /running/i.test(agent.status))
    ?? agents[0]
    ?? null;
  const activeAgent = selectedAgent && pendingApproval?.agentId === selectedAgent.id
    ? { ...selectedAgent, status: 'waiting', currentAction: t.t('approval.title') }
    : selectedAgent;
  useEffect(() => {
    if (!inspectorOpen) return;
    document.querySelector<HTMLElement>('#mission-inspector [role="tab"][aria-selected="true"]')?.focus();
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      setInspectorOpen(false);
      window.requestAnimationFrame(() => inspectorTrigger.current?.focus());
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [inspectorOpen]);
  const sendInstruction = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const content = instruction.trim();
    if (!content || writesDisabled) return;
    void Promise.resolve(onDispatch({ type: 'instruction.send', content })).then(() => setInstruction(''));
  };
  const focusAgent = () => { setActiveTab('agents'); setInspectorOpen(true); };
  return <div className="page page-mission">
    <div className="mission-canvas">
      <EvidenceBackdrop evidence={view.product.evidence} />
      <div className="mission-command-deck" data-testid="mission-command-deck">
        <header className="mission-header cyber-glass">
          <div className="mission-heading"><span className="mission-eyebrow">CYBER · COMMAND DECK</span><h1>{t.t('mission.title')}</h1>
            <span className="mission-title-detail">{view.product.task?.title ?? t.t('common.none')}</span>
            <span className="mission-meta">{view.product.scope?.targets.join(', ') ?? t.t('common.none')} · cursor {view.product.committedCursor}</span>
          </div>
          {view.product.task && <div className="mission-task-summary"><strong>{view.product.task.status}</strong><span>{view.product.activeRuntime?.id ?? 'scenario-local'}</span></div>}
          <div className="mission-actions">
            <button type="button" title={t.t('task.pause')} aria-label={t.t('task.pause')} disabled={writesDisabled} onClick={() => void onDispatch({ type: 'task.pause' })}><Pause aria-hidden="true" size={17} /></button>
            <button type="button" title={t.t('task.resume')} aria-label={t.t('task.resume')} disabled={writesDisabled} onClick={() => void onDispatch({ type: 'task.resume' })}><Play aria-hidden="true" size={17} /></button>
            <button type="button" title={t.t('task.cancel')} aria-label={t.t('task.cancel')} disabled={writesDisabled} onClick={() => void onDispatch({ type: 'task.cancel' })}><OctagonX aria-hidden="true" size={17} /></button>
            <button ref={inspectorTrigger} className="mission-inspector-trigger" type="button" title={inspectorOpen ? t.t('inspector.close') : t.t('inspector.open')} aria-label={inspectorOpen ? t.t('inspector.close') : t.t('inspector.open')} aria-expanded={inspectorOpen} aria-controls="mission-inspector" onClick={() => setInspectorOpen((open) => !open)}><PanelRightOpen aria-hidden="true" size={17} /></button>
          </div>
        </header>
        <section className="mission-stream-surface cyber-glass" id="mission-stream">
          <div className="mission-system-banners">
            <ConnectionBanner connection={view.connection} t={t} onReconnect={onReconnect} onDisconnect={onDisconnect} />
            <ControlLeaseBanner lease={view.product.controlLease} t={t} disabled={transportBlocked} onTakeControl={(expectedRevision) => void onDispatch({ type: 'control.take', expectedRevision })} />
          </div>
          <NarrativeStream events={view.product.timeline} t={t} />
        {Object.values(view.product.approvals).filter((approval) => !approval.decision).map((approval) => <ApprovalCard
          key={approval.id}
          approval={approval}
          disabled={writesDisabled}
          t={t}
          onApprovalDecision={(challengeId, decision) => void onDispatch({ type: 'approval.respond', challengeId, decision })}
        />)}
          <form className="mission-composer" onSubmit={sendInstruction}>
            <label className="cyber-visually-hidden" htmlFor="mission-instruction">{t.t('task.placeholder')}</label>
            <div><textarea id="mission-instruction" aria-label={t.t('task.placeholder')} placeholder={t.t('task.placeholder')} value={instruction} disabled={writesDisabled} onChange={(event) => setInstruction(event.target.value)} />
              <button type="submit" aria-label={t.t('task.send')} title={t.t('task.send')} disabled={writesDisabled || instruction.trim().length === 0}><Send aria-hidden="true" size={18} /></button></div>
          </form>
        </section>
        <aside id="mission-inspector" className="mission-inspector-surface cyber-glass" data-open={inspectorOpen} aria-label={t.t('inspector.label')}><AgentInspector
          agents={view.product.agents}
          scope={view.product.scope}
          evidence={view.product.evidence}
          activeTab={activeTab}
          t={t}
          onTabChange={setActiveTab}
        /></aside>
      </div>
      <ActiveAgentRibbon agent={activeAgent} onSelect={focusAgent} />
    </div>
  </div>;
}
