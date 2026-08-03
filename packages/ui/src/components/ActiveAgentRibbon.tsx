import type { AgentState } from '@cyber/protocol';

export type ActiveAgentRibbonProps = {
  agent: AgentState | null;
  onSelect?: (agentId: string) => void;
};

const toneFor = (agent: AgentState): 'running' | 'pending' | 'failure' => {
  const signal = `${agent.status} ${agent.currentAction ?? ''}`.toLowerCase();
  if (signal.includes('fail') || signal.includes('error')) return 'failure';
  if (signal.includes('wait') || signal.includes('block') || signal.includes('approval')) return 'pending';
  return 'running';
};

export function ActiveAgentRibbon({ agent, onSelect }: ActiveAgentRibbonProps) {
  if (!agent) return null;
  const progress = agent.progress === undefined ? undefined : Math.max(0, Math.min(100, agent.progress));
  return <button className="cyber-active-agent" type="button" data-tone={toneFor(agent)} onClick={() => onSelect?.(agent.id)}>
    <span className="cyber-agent-signal" aria-hidden="true" />
    <span className="cyber-active-agent-copy"><strong>{agent.name}</strong><small>{agent.currentAction ?? agent.status}</small></span>
    {progress !== undefined && <span className="cyber-agent-progress" role="progressbar" aria-label={`${agent.name} progress`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={progress}>
      <i style={{ inlineSize: `${progress}%` }} />
    </span>}
  </button>;
}
