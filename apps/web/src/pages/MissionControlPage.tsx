import { useState, type FormEvent } from 'react';

import type { Translator } from '@cyber/i18n';
import type { RuntimeCommand, RuntimeView } from '@cyber/runtime-client';
import {
  AgentInspector,
  ApprovalCard,
  ConnectionBanner,
  ControlLeaseBanner,
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
  const [instruction, setInstruction] = useState('');
  const transportBlocked = ['resyncing', 'offline', 'incompatible', 'unauthorized'].includes(view.connection.status);
  const displaced = view.product.controlLease !== null && view.product.controlLease.clientId !== clientId;
  const writesDisabled = transportBlocked || displaced;
  const sendInstruction = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const content = instruction.trim();
    if (!content || writesDisabled) return;
    void Promise.resolve(onDispatch({ type: 'instruction.send', content })).then(() => setInstruction(''));
  };
  return <div className="page page-mission">
    <header className="mission-header">
      <h1>{t.t('mission.title')}</h1>
      {view.product.task && <div className="mission-task-summary">
        <strong>{view.product.task.title}</strong>
        <span>{view.product.task.status}</span>
      </div>}
    </header>
    <ConnectionBanner connection={view.connection} t={t} onReconnect={onReconnect} onDisconnect={onDisconnect} />
    <ControlLeaseBanner lease={view.product.controlLease} t={t} disabled={transportBlocked} onTakeControl={(expectedRevision) => void onDispatch({ type: 'control.take', expectedRevision })} />
    <div className="mission-actions">
      <button type="button" disabled={writesDisabled} onClick={() => void onDispatch({ type: 'task.pause' })}>{t.t('task.pause')}</button>
      <button type="button" disabled={writesDisabled} onClick={() => void onDispatch({ type: 'task.resume' })}>{t.t('task.resume')}</button>
      <button type="button" disabled={writesDisabled} onClick={() => void onDispatch({ type: 'task.cancel' })}>{t.t('task.cancel')}</button>
    </div>
    <form className="mission-composer" onSubmit={sendInstruction}>
      <label htmlFor="mission-instruction">{t.t('task.placeholder')}</label>
      <div>
        <textarea id="mission-instruction" value={instruction} disabled={writesDisabled} onChange={(event) => setInstruction(event.target.value)} />
        <button type="submit" disabled={writesDisabled || instruction.trim().length === 0}>{t.t('task.send')}</button>
      </div>
    </form>
    <div className="mission-grid">
      <section id="mission-stream"><NarrativeStream events={view.product.timeline} t={t} />
        {Object.values(view.product.approvals).filter((approval) => !approval.decision).map((approval) => <ApprovalCard
          key={approval.id}
          approval={approval}
          disabled={writesDisabled}
          t={t}
          onApprovalDecision={(challengeId, decision) => void onDispatch({ type: 'approval.respond', challengeId, decision })}
        />)}
      </section>
      <aside aria-label={t.t('inspector.label')}><AgentInspector
        agents={view.product.agents}
        scope={view.product.scope}
        evidence={view.product.evidence}
        activeTab={activeTab}
        t={t}
        onTabChange={setActiveTab}
      /></aside>
    </div>
  </div>;
}
