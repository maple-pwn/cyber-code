export const packageName = '@cyber/protocol' as const;

export type JsonObject = Record<string, unknown>;
export type EventSourceRef = { runtimeId: string; agentId?: string; toolCallId?: string };
export type ProductEvent<TType extends string, TPayload> = { schemaVersion: 1; eventId: string; taskId: string; cursor: number; occurredAt: string; type: TType; source: EventSourceRef; payload: TPayload };
export type RawProductEvent = { schemaVersion: unknown; eventId: unknown; taskId: unknown; cursor: unknown; occurredAt: unknown; type: unknown; source: unknown; payload: unknown };
export type ScopeSnapshot = { id: string; principal: string; workspace: string; validity: string; targets: string[]; allowedActions: string[]; deniedActions: string[]; riskCeiling: string };
export type AgentState = { id: string; name: string; status: string; progress?: number; currentAction?: string };
export type ImmutableEvidence = { id: string; taskId: string; kind: string; summary: string; data: JsonObject };
export type FindingStatus = 'candidate' | 'verifying' | 'confirmed' | 'rejected' | 'mitigated';
export type FindingState = { id: string; title: string; severity: string; status: FindingStatus; confidence: string; evidenceIds: string[]; rejectionReason?: string };
export type ApprovalDecision = 'allow_once' | 'deny';
export type ApprovalChallenge = { id: string; agentId: string; action: string; target: string; parameterDigest: string; risk: string; expiresAt: string };
export type ApprovalState = ApprovalChallenge & { decision?: ApprovalDecision };
export type ControlLease = { clientId: string; revision: number };
export type TerminalOutputChunk = { sequence: number; data: string; byteLength: number };
export type TerminalSessionState = {
  id: string;
  profileId: string;
  processId: string;
  workingDirectory: string;
  scopeId: string;
  ownerClientId: string;
  leaseRevision: number;
  columns: number;
  rows: number;
  outputLimitBytes: number;
  outputBytes: number;
  nextInputSequence: number;
  nextOutputSequence: number;
  status: 'open' | 'exited';
  output: TerminalOutputChunk[];
  exitCode?: number;
  exitReason?: string;
};
export type EditorEvidenceReference = { findingId: string; evidenceId: string; startLine: number; endLine: number };
export type EditorVerificationState = { id: string; revision: number; success: boolean; evidenceIds: string[] };
export type EditorDraftState = {
  id: string;
  path: string;
  scopeId: string;
  ownerClientId: string;
  leaseRevision: number;
  baseSha256: string;
  baseByteLength: number;
  encoding: 'utf-8';
  evidenceReferences: EditorEvidenceReference[];
  status: 'open' | 'saved' | 'applied' | 'verified' | 'discarded';
  nextRevision: number;
  proposedSha256?: string;
  proposedByteLength?: number;
  resultSha256?: string;
  reviewer?: string;
  verification?: EditorVerificationState;
  discardReason?: string;
};
export type ReportFinding = {
  finding: FindingState;
  evidence: ImmutableEvidence[];
  included: boolean;
  exclusionReason?: string;
};
export type ReportState = {
  id: string;
  taskId: string;
  version: number;
  status: 'draft' | 'frozen';
  narrative: string;
  recommendations: string;
  humanNotes: string;
  findings: ReportFinding[];
};
export type FrozenReport = ReportState & { status: 'frozen' };

export interface KnownEventPayloads {
  'task.created': { title: string }; 'task.started': { title: string }; 'task.paused': JsonObject; 'task.resumed': JsonObject; 'task.cancel.requested': JsonObject; 'task.cancelled': JsonObject; 'task.completed': JsonObject; 'task.failed': { reason: string }; 'task.blocked': { reason: string };
  'scope.proposed': { scope: ScopeSnapshot }; 'scope.confirmed': { scope: ScopeSnapshot }; 'runtime.capabilities.updated': { capabilities: string[] }; 'control.acquired': { lease: ControlLease }; 'control.transferred': { lease: ControlLease }; 'control.released': { clientId: string }; 'approval.requested': { challenge: ApprovalChallenge }; 'approval.resolved': { challengeId: string; decision: ApprovalDecision }; 'question.requested': { questionId: string; prompt: string }; 'question.resolved': { questionId: string; answer: string };
  'agent.started': { agent: AgentState }; 'agent.progressed': { agentId: string; progress: number; currentAction?: string }; 'agent.completed': { agentId: string }; 'agent.failed': { agentId: string; reason: string }; 'tool.started': { callId: string; name: string }; 'tool.completed': { callId: string; success: boolean; evidenceIds: string[] }; 'tool.failed': { callId: string; reason: string }; 'evidence.committed': { evidence: ImmutableEvidence }; 'finding.created': { finding: FindingState }; 'finding.verifying': { findingId: string }; 'finding.confirmed': { findingId: string }; 'finding.rejected': { findingId: string; reason: string }; 'finding.mitigated': { findingId: string };
  'report.drafted': { report: ReportState }; 'report.edited': { report: ReportState }; 'report.validation.failed': { reportId: string; reason: string }; 'report.validated': { reportId: string }; 'report.frozen': { reportId: string; version: number }; 'report.exported': { reportId: string; format: string };
  'terminal.opened': { session: Omit<TerminalSessionState, 'outputBytes' | 'nextInputSequence' | 'nextOutputSequence' | 'status' | 'output' | 'exitCode' | 'exitReason'> };
  'terminal.output': { sessionId: string; sequence: number; data: string; byteLength: number };
  'terminal.input.accepted': { sessionId: string; sequence: number; byteLength: number; sha256: string };
  'terminal.resized': { sessionId: string; columns: number; rows: number };
  'terminal.exited': { sessionId: string; exitCode: number; reason: string };
  'editor.draft.opened': { draft: Omit<EditorDraftState, 'status' | 'nextRevision' | 'proposedSha256' | 'proposedByteLength' | 'resultSha256' | 'reviewer' | 'verification' | 'discardReason'> };
  'editor.draft.saved': { draftId: string; revision: number; baseSha256: string; proposedSha256: string; proposedByteLength: number };
  'editor.patch.applied': { draftId: string; revision: number; baseSha256: string; proposedSha256: string; resultSha256: string; reviewer: string };
  'editor.patch.verified': { draftId: string; revision: number; verificationId: string; success: boolean; evidenceIds: string[] };
  'editor.draft.discarded': { draftId: string; reason: string };
}
export type KnownEventType = keyof KnownEventPayloads;
export type KnownProductEvent = { [K in KnownEventType]: ProductEvent<K, KnownEventPayloads[K]> }[KnownEventType];
export type UnknownProductEvent = ProductEvent<string, JsonObject> & { kind: 'unknown' };
export type ValidatedProductEvent = (KnownProductEvent & { kind: 'known' }) | UnknownProductEvent;
export type ProductState = { activeRuntime: { id: string } | null; task: { id: string; title: string; status: string } | null; scope: ScopeSnapshot | null; controlLease: ControlLease | null; highestCommittedLeaseRevision: number; agents: Record<string, AgentState>; timeline: ValidatedProductEvent[]; approvals: Record<string, ApprovalState>; findings: Record<string, FindingState>; evidence: Record<string, ImmutableEvidence>; terminals: Record<string, TerminalSessionState>; editorDrafts: Record<string, EditorDraftState>; report: ReportState | null; rawEvents: ValidatedProductEvent[]; committedCursor: number; canonicalEvents: Record<string, string> };
export type ProjectionResult = { kind: 'applied'; state: ProductState } | { kind: 'duplicate'; state: ProductState } | { kind: 'resync-required'; state: ProductState; expectedCursor: number };

export function initialProductState(): ProductState { return { activeRuntime: null, task: null, scope: null, controlLease: null, highestCommittedLeaseRevision: 0, agents: {}, timeline: [], approvals: {}, findings: {}, evidence: {}, terminals: {}, editorDrafts: {}, report: null, rawEvents: [], committedCursor: 0, canonicalEvents: {} }; }
export { validateEvent } from './validation';
export { exportReport, freezeReport, validateReport, type ReportAuditMetadata, type ReportFormat, type ReportValidation } from './report';

const canonicalize = (value: unknown): string => JSON.stringify(value, (_key, item: unknown) => item && typeof item === 'object' && !Array.isArray(item) ? Object.fromEntries(Object.entries(item as JsonObject).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)) : item);
const deepFreeze = <T>(value: T): T => { if (value && typeof value === 'object' && !Object.isFrozen(value)) { Object.freeze(value); for (const child of Object.values(value as object)) deepFreeze(child); } return value; };
const clone = <T>(value: T): T => structuredClone(value);
const retained = <T>(value: T): T => deepFreeze(clone(value));
const transition: Record<FindingStatus, FindingStatus[]> = { candidate: ['verifying', 'rejected'], verifying: ['confirmed', 'rejected'], confirmed: ['mitigated'], rejected: [], mitigated: [] };

export function project(previous: ProductState, event: ValidatedProductEvent): ProjectionResult {
  const canonical = canonicalize(event);
  const existing = previous.canonicalEvents[event.eventId];
  if (existing !== undefined) { if (existing === canonical) return { kind: 'duplicate', state: previous }; throw new Error('event_id_conflict'); }
  if (event.cursor <= previous.committedCursor) throw new Error('stale_cursor');
  if (event.cursor > previous.committedCursor + 1) return { kind: 'resync-required', state: previous, expectedCursor: previous.committedCursor + 1 };
  const state = clone(previous);
  for (const value of [state.task, state.scope, state.controlLease, state.report, ...Object.values(state.agents), ...Object.values(state.approvals), ...Object.values(state.findings), ...Object.values(state.evidence), ...Object.values(state.terminals), ...Object.values(state.editorDrafts), ...state.timeline, ...state.rawEvents]) deepFreeze(value);
  state.committedCursor = event.cursor; state.canonicalEvents[event.eventId] = canonical;
  const storedEvent = retained(event);
  if (event.kind === 'unknown') { state.rawEvents.push(storedEvent); return { kind: 'applied', state }; }
  state.timeline.push(storedEvent);
  const payload = event.payload as JsonObject;
  switch (event.type) {
    case 'task.created': case 'task.started': state.task = retained({ id: event.taskId, title: payload.title as string, status: event.type === 'task.started' ? 'running' : 'created' }); break;
    case 'task.paused': case 'task.resumed': case 'task.cancel.requested': case 'task.cancelled': case 'task.completed': case 'task.failed': case 'task.blocked': if (state.task) state.task = retained({ ...state.task, status: event.type.slice(5) }); break;
    case 'scope.proposed': case 'scope.confirmed': state.scope = retained(payload.scope as ScopeSnapshot); break;
    case 'agent.started': { const agent = payload.agent as AgentState; state.agents[agent.id] = retained(agent); break; }
    case 'agent.progressed': if (state.agents[payload.agentId as string]) state.agents[payload.agentId as string] = retained({ ...state.agents[payload.agentId as string], ...payload }); break;
    case 'agent.completed': case 'agent.failed': if (state.agents[payload.agentId as string]) state.agents[payload.agentId as string] = retained({ ...state.agents[payload.agentId as string], status: event.type.slice(6) }); break;
    case 'evidence.committed': { const evidence = retained(payload.evidence as ImmutableEvidence); state.evidence[evidence.id] = evidence; break; }
    case 'finding.created': { const finding = payload.finding as FindingState; state.findings[finding.id] = retained(finding); break; }
    case 'finding.verifying': case 'finding.confirmed': case 'finding.rejected': case 'finding.mitigated': { const finding = state.findings[payload.findingId as string]; const status = event.type.slice('finding.'.length) as FindingStatus; if (!finding || !transition[finding.status].includes(status)) throw new Error('invalid_finding_transition'); state.findings[finding.id] = retained({ ...finding, status, ...(status === 'rejected' ? { rejectionReason: payload.reason as string } : {}) }); break; }
    case 'approval.requested': { const challenge = payload.challenge as ApprovalChallenge; const existingApproval = state.approvals[challenge.id]; if (existingApproval && canonicalize(existingApproval) !== canonicalize(challenge)) throw new Error('approval_challenge_conflict'); if (!existingApproval) state.approvals[challenge.id] = retained(challenge); break; }
    case 'approval.resolved': { const approval = state.approvals[payload.challengeId as string]; if (!approval || approval.decision) throw new Error('approval_already_resolved'); if (Date.parse(event.occurredAt) > Date.parse(approval.expiresAt)) throw new Error('approval_expired'); state.approvals[approval.id] = retained({ ...approval, decision: payload.decision as ApprovalDecision }); break; }
    case 'control.acquired': case 'control.transferred': { const lease = payload.lease as ControlLease; if (lease.revision <= state.highestCommittedLeaseRevision) throw new Error('non_monotonic_lease_revision'); state.controlLease = retained(lease); state.highestCommittedLeaseRevision = lease.revision; break; }
    case 'control.released': if (state.controlLease?.clientId === payload.clientId) state.controlLease = null; break;
    case 'report.drafted': case 'report.edited': state.report = retained(payload.report as ReportState); break;
    case 'report.frozen': if (state.report) state.report = retained({ ...state.report, version: payload.version as number, status: 'frozen' }); break;
    case 'terminal.opened': {
      const session = payload.session as KnownEventPayloads['terminal.opened']['session'];
      if (state.terminals[session.id]) throw new Error('terminal_session_conflict');
      state.terminals[session.id] = retained({ ...session, outputBytes: 0, nextInputSequence: 1, nextOutputSequence: 1, status: 'open', output: [] });
      break;
    }
    case 'terminal.output': {
      const session = state.terminals[payload.sessionId as string];
      if (!session || session.status !== 'open') throw new Error('terminal_session_not_open');
      if (payload.sequence !== session.nextOutputSequence) throw new Error('terminal_output_sequence');
      const outputBytes = session.outputBytes + (payload.byteLength as number);
      if (outputBytes > session.outputLimitBytes) throw new Error('terminal_output_limit');
      const chunk = { sequence: payload.sequence as number, data: payload.data as string, byteLength: payload.byteLength as number };
      state.terminals[session.id] = retained({ ...session, outputBytes, nextOutputSequence: session.nextOutputSequence + 1, output: [...session.output, chunk] });
      break;
    }
    case 'terminal.input.accepted': {
      const session = state.terminals[payload.sessionId as string];
      if (!session || session.status !== 'open') throw new Error('terminal_session_not_open');
      if (payload.sequence !== session.nextInputSequence) throw new Error('terminal_input_sequence');
      state.terminals[session.id] = retained({ ...session, nextInputSequence: session.nextInputSequence + 1 });
      break;
    }
    case 'terminal.resized': {
      const session = state.terminals[payload.sessionId as string];
      if (!session || session.status !== 'open') throw new Error('terminal_session_not_open');
      state.terminals[session.id] = retained({ ...session, columns: payload.columns as number, rows: payload.rows as number });
      break;
    }
    case 'terminal.exited': {
      const session = state.terminals[payload.sessionId as string];
      if (!session || session.status !== 'open') throw new Error('terminal_session_not_open');
      state.terminals[session.id] = retained({ ...session, status: 'exited', exitCode: payload.exitCode as number, exitReason: payload.reason as string });
      break;
    }
    case 'editor.draft.opened': {
      const draft = payload.draft as KnownEventPayloads['editor.draft.opened']['draft'];
      if (state.editorDrafts[draft.id]) throw new Error('editor_draft_conflict');
      for (const reference of draft.evidenceReferences) {
        const finding = state.findings[reference.findingId];
        const evidence = state.evidence[reference.evidenceId];
        if (!finding || !evidence || evidence.taskId !== event.taskId || !finding.evidenceIds.includes(reference.evidenceId)) throw new Error('editor_provenance_missing');
      }
      state.editorDrafts[draft.id] = retained({ ...draft, status: 'open', nextRevision: 1 });
      break;
    }
    case 'editor.draft.saved': {
      const draft = state.editorDrafts[payload.draftId as string];
      if (!draft || !['open', 'saved'].includes(draft.status)) throw new Error('editor_draft_not_editable');
      if (payload.revision !== draft.nextRevision || payload.baseSha256 !== draft.baseSha256) throw new Error('editor_draft_revision');
      state.editorDrafts[draft.id] = retained({ ...draft, status: 'saved', nextRevision: draft.nextRevision + 1, proposedSha256: payload.proposedSha256 as string, proposedByteLength: payload.proposedByteLength as number });
      break;
    }
    case 'editor.patch.applied': {
      const draft = state.editorDrafts[payload.draftId as string];
      if (!draft || draft.status !== 'saved' || payload.revision !== draft.nextRevision - 1 || payload.baseSha256 !== draft.baseSha256 || payload.proposedSha256 !== draft.proposedSha256) throw new Error('editor_patch_stale');
      state.editorDrafts[draft.id] = retained({ ...draft, status: 'applied', resultSha256: payload.resultSha256 as string, reviewer: payload.reviewer as string });
      break;
    }
    case 'editor.patch.verified': {
      const draft = state.editorDrafts[payload.draftId as string];
      if (!draft || draft.status !== 'applied' || payload.revision !== draft.nextRevision - 1) throw new Error('editor_patch_not_applied');
      for (const evidenceId of payload.evidenceIds as string[]) if (!state.evidence[evidenceId]) throw new Error('editor_provenance_missing');
      state.editorDrafts[draft.id] = retained({ ...draft, status: 'verified', verification: { id: payload.verificationId as string, revision: payload.revision as number, success: payload.success as boolean, evidenceIds: payload.evidenceIds as string[] } });
      break;
    }
    case 'editor.draft.discarded': {
      const draft = state.editorDrafts[payload.draftId as string];
      if (!draft || !['open', 'saved'].includes(draft.status)) throw new Error('editor_draft_not_editable');
      state.editorDrafts[draft.id] = retained({ ...draft, status: 'discarded', discardReason: payload.reason as string });
      break;
    }
  }
  return { kind: 'applied', state };
}
