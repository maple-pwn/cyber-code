import { useState } from 'react';

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
};

export function MissionControlPage({ view, t, onDispatch, onReconnect }: MissionControlPageProps) {
  const [activeTab, setActiveTab] = useState<InspectorTab>('agents');
  const disabled = ['resyncing', 'offline', 'incompatible', 'unauthorized'].includes(view.connection.status);
  return <div className="page page-mission">
    <h1>{t.t('mission.title')}</h1>
    <ConnectionBanner connection={view.connection} t={t} onReconnect={onReconnect} />
    <ControlLeaseBanner lease={view.product.controlLease} t={t} onTakeControl={(expectedRevision) => void onDispatch({ type: 'control.take', expectedRevision })} />
    <div className="mission-actions">
      <button type="button" disabled={disabled} onClick={() => void onDispatch({ type: 'task.pause' })}>{t.t('task.pause')}</button>
      <button type="button" disabled={disabled} onClick={() => void onDispatch({ type: 'task.resume' })}>{t.t('task.resume')}</button>
      <button type="button" disabled={disabled} onClick={() => void onDispatch({ type: 'task.cancel' })}>{t.t('task.cancel')}</button>
    </div>
    <div className="mission-grid">
      <main id="mission-stream"><NarrativeStream events={view.product.timeline} t={t} />
        {Object.values(view.product.approvals).filter((approval) => !approval.decision).map((approval) => <ApprovalCard
          key={approval.id}
          approval={approval}
          disabled={disabled}
          t={t}
          onApprovalDecision={(challengeId, decision) => void onDispatch({ type: 'approval.respond', challengeId, decision })}
        />)}
      </main>
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
