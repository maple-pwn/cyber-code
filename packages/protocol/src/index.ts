export const packageName = '@cyber/protocol' as const;

export type JsonObject = Record<string, unknown>;
export type EventSourceRef = { runtimeId: string; agentId?: string; toolCallId?: string };
export type ProductEvent<TType extends string, TPayload> = { schemaVersion: 1; eventId: string; taskId: string; cursor: number; occurredAt: string; type: TType; source: EventSourceRef; payload: TPayload };
export type RawProductEvent = { schemaVersion: unknown; eventId: unknown; taskId: unknown; cursor: unknown; occurredAt: unknown; type: unknown; source: unknown; payload: unknown };
export type ScopeSnapshot = { targets: string[]; allowedActions: string[]; deniedActions: string[]; riskCeiling: string };
export type AgentState = { id: string; name: string; status: string; progress?: number; currentAction?: string };
export type ImmutableEvidence = { id: string; kind: string; summary: string; data: JsonObject };
export type FindingStatus = 'candidate' | 'verifying' | 'confirmed' | 'rejected' | 'mitigated';
export type FindingState = { id: string; title: string; severity: string; status: FindingStatus; confidence: string; evidenceIds: string[]; rejectionReason?: string };
export type ApprovalDecision = 'allow_once' | 'deny';
export type ApprovalChallenge = { id: string; agentId: string; action: string; target: string; parameterDigest: string; risk: string; expiresAt: string };
export type ApprovalState = ApprovalChallenge & { decision?: ApprovalDecision };
export type ControlLease = { clientId: string; revision: number };
export type ReportState = { id: string; version: number; status?: string; [key: string]: unknown };

export interface KnownEventPayloads {
  'task.created': { title: string }; 'task.started': { title: string }; 'task.paused': JsonObject; 'task.resumed': JsonObject; 'task.cancel.requested': JsonObject; 'task.cancelled': JsonObject; 'task.completed': JsonObject; 'task.failed': { reason: string }; 'task.blocked': { reason: string };
  'scope.proposed': { scope: ScopeSnapshot }; 'scope.confirmed': { scope: ScopeSnapshot }; 'runtime.capabilities.updated': { capabilities: string[] }; 'control.acquired': { lease: ControlLease }; 'control.transferred': { lease: ControlLease }; 'control.released': { clientId: string }; 'approval.requested': { challenge: ApprovalChallenge }; 'approval.resolved': { challengeId: string; decision: ApprovalDecision }; 'question.requested': { questionId: string; prompt: string }; 'question.resolved': { questionId: string; answer: string };
  'agent.started': { agent: AgentState }; 'agent.progressed': { agentId: string; progress: number; currentAction?: string }; 'agent.completed': { agentId: string }; 'agent.failed': { agentId: string; reason: string }; 'tool.started': { callId: string; name: string }; 'tool.completed': { callId: string; success: boolean; evidenceIds: string[] }; 'tool.failed': { callId: string; reason: string }; 'evidence.committed': { evidence: ImmutableEvidence }; 'finding.created': { finding: FindingState }; 'finding.verifying': { findingId: string }; 'finding.confirmed': { findingId: string }; 'finding.rejected': { findingId: string; reason: string }; 'finding.mitigated': { findingId: string };
  'report.drafted': { report: ReportState }; 'report.edited': { report: ReportState }; 'report.validation.failed': { reportId: string; reason: string }; 'report.validated': { reportId: string }; 'report.frozen': { reportId: string; version: number }; 'report.exported': { reportId: string; format: string };
}
export type KnownEventType = keyof KnownEventPayloads;
export type KnownProductEvent = { [K in KnownEventType]: ProductEvent<K, KnownEventPayloads[K]> }[KnownEventType];
export type UnknownProductEvent = ProductEvent<string, JsonObject> & { kind: 'unknown' };
export type ValidatedProductEvent = (KnownProductEvent & { kind: 'known' }) | UnknownProductEvent;

export type ProductState = { connection: string; activeRuntime: { id: string } | null; task: { id: string; title: string; status: string } | null; scope: ScopeSnapshot | null; controlLease: ControlLease | null; agents: Record<string, AgentState>; timeline: ValidatedProductEvent[]; approvals: Record<string, ApprovalState>; findings: Record<string, FindingState>; evidence: Record<string, ImmutableEvidence>; report: ReportState | null; rawEvents: ValidatedProductEvent[]; committedCursor: number; canonicalEvents: Record<string, string> };
export type ProjectionResult = { kind: 'applied'; state: ProductState } | { kind: 'duplicate'; state: ProductState } | { kind: 'resync-required'; state: ProductState; expectedCursor: number };

export function initialProductState(): ProductState { return { connection: 'connecting', activeRuntime: null, task: null, scope: null, controlLease: null, agents: {}, timeline: [], approvals: {}, findings: {}, evidence: {}, report: null, rawEvents: [], committedCursor: 0, canonicalEvents: {} }; }
export { validateEvent } from './validation';

const canonicalize = (value: unknown): string => JSON.stringify(value, (_key, item: unknown) => item && typeof item === 'object' && !Array.isArray(item) ? Object.fromEntries(Object.entries(item as JsonObject).sort(([a], [b]) => a.localeCompare(b))) : item);
const deepFreeze = <T>(value: T): T => { if (value && typeof value === 'object') { Object.freeze(value); for (const child of Object.values(value as object)) deepFreeze(child); } return value; };
const clone = <T>(value: T): T => structuredClone(value);
const transition: Record<FindingStatus, FindingStatus[]> = { candidate: ['verifying', 'rejected'], verifying: ['confirmed', 'rejected'], confirmed: ['mitigated'], rejected: [], mitigated: [] };

export function project(previous: ProductState, event: ValidatedProductEvent): ProjectionResult {
  const canonical = canonicalize(event);
  const existing = previous.canonicalEvents[event.eventId];
  if (existing !== undefined) { if (existing === canonical) return { kind: 'duplicate', state: previous }; throw new Error('event_id_conflict'); }
  if (event.cursor <= previous.committedCursor) throw new Error('stale_cursor');
  if (event.cursor > previous.committedCursor + 1) return { kind: 'resync-required', state: previous, expectedCursor: previous.committedCursor + 1 };
  const state = clone(previous);
  for (const evidence of Object.values(state.evidence)) deepFreeze(evidence);
  state.committedCursor = event.cursor; state.canonicalEvents[event.eventId] = canonical;
  if (event.kind === 'unknown') { state.rawEvents.push(event); return { kind: 'applied', state }; }
  state.timeline.push(event);
  const payload = event.payload as JsonObject;
  switch (event.type) {
    case 'task.created': case 'task.started': state.task = { id: event.taskId, title: payload.title as string, status: event.type === 'task.started' ? 'running' : 'created' }; break;
    case 'task.paused': case 'task.resumed': case 'task.cancel.requested': case 'task.cancelled': case 'task.completed': case 'task.failed': case 'task.blocked': if (state.task) state.task.status = event.type.slice(5); break;
    case 'scope.proposed': case 'scope.confirmed': state.scope = payload.scope as ScopeSnapshot; break;
    case 'agent.started': state.agents[(payload.agent as AgentState).id] = payload.agent as AgentState; break;
    case 'agent.progressed': if (state.agents[payload.agentId as string]) Object.assign(state.agents[payload.agentId as string], payload); break;
    case 'agent.completed': case 'agent.failed': if (state.agents[payload.agentId as string]) state.agents[payload.agentId as string].status = event.type.slice(6); break;
    case 'evidence.committed': { const evidence = deepFreeze(clone(payload.evidence as ImmutableEvidence)); state.evidence[evidence.id] = evidence; break; }
    case 'finding.created': state.findings[(payload.finding as FindingState).id] = clone(payload.finding as FindingState); break;
    case 'finding.verifying': case 'finding.confirmed': case 'finding.rejected': case 'finding.mitigated': { const finding = state.findings[payload.findingId as string]; const status = event.type.slice('finding.'.length) as FindingStatus; if (!finding || !transition[finding.status].includes(status)) throw new Error('invalid_finding_transition'); finding.status = status; if (status === 'rejected') finding.rejectionReason = payload.reason as string; break; }
    case 'approval.requested': { const challenge = payload.challenge as ApprovalChallenge; state.approvals[challenge.id] = challenge; break; }
    case 'approval.resolved': { const approval = state.approvals[payload.challengeId as string]; if (!approval || approval.decision) throw new Error('approval_already_resolved'); if (payload.decision === 'allow_once' && Date.parse(event.occurredAt) > Date.parse(approval.expiresAt)) throw new Error('approval_expired'); approval.decision = payload.decision as ApprovalDecision; break; }
    case 'control.acquired': case 'control.transferred': { const lease = payload.lease as ControlLease; if (state.controlLease && lease.revision <= state.controlLease.revision) throw new Error('non_monotonic_lease_revision'); state.controlLease = lease; break; }
    case 'control.released': if (state.controlLease?.clientId === payload.clientId) state.controlLease = null; break;
    case 'report.drafted': case 'report.edited': state.report = payload.report as ReportState; break;
    case 'report.frozen': if (state.report) state.report = { ...state.report, version: payload.version as number, status: 'frozen' }; break;
  }
  return { kind: 'applied', state };
}
