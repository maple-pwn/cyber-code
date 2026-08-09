import type {
  ApprovalDecision,
  ProductState,
  RawProductEvent,
} from '@cyber/protocol';
import type {
  RuntimeCommandEnvelope,
  RuntimeCommandReceipt,
  RuntimeHandshakeRequest,
  RuntimeHandshakeResponse,
  RuntimeSourceMetadata,
} from './conformance';

export type ConnectionStatus =
  | 'connecting'
  | 'healthy'
  | 'degraded'
  | 'reconnecting'
  | 'resyncing'
  | 'offline'
  | 'incompatible'
  | 'unauthorized';

export type ConnectionState = {
  status: ConnectionStatus;
  lastTrustedCursor: number;
  errorCode?: string;
};

export type RuntimeCommand =
  | { type: 'task.create'; objective: string; runtimeId: string; workspace?: string }
  | { type: 'scope.confirm'; scopeId: string }
  | { type: 'task.pause' }
  | { type: 'task.resume' }
  | { type: 'task.cancel' }
  | { type: 'approval.respond'; challengeId: string; decision: ApprovalDecision }
  | { type: 'control.take'; expectedRevision: number }
  | { type: 'instruction.send'; content: string }
  | { type: 'terminal.open'; sessionId: string; profileId: string; workingDirectory: string; scopeId: string; columns: number; rows: number; outputLimitBytes: number; expectedLeaseRevision: number }
  | { type: 'terminal.input'; sessionId: string; sequence: number; data: string; byteLength: number; expectedLeaseRevision: number }
  | { type: 'terminal.resize'; sessionId: string; columns: number; rows: number; expectedLeaseRevision: number }
  | { type: 'terminal.cancel'; sessionId: string; expectedLeaseRevision: number }
  | { type: 'editor.open'; draftId: string; path: string; scopeId: string; evidenceReferences: { findingId: string; evidenceId: string; startLine: number; endLine: number }[]; expectedLeaseRevision: number }
  | { type: 'editor.save'; draftId: string; revision: number; baseSha256: string; data: string; byteLength: number; expectedLeaseRevision: number }
  | { type: 'editor.apply'; draftId: string; revision: number; proposedSha256: string; expectedLeaseRevision: number }
  | { type: 'editor.discard'; draftId: string; expectedLeaseRevision: number };

export type RuntimeSnapshot = { cursor: number; state: ProductState };
export type EditorReadResult = { draftId: string; revision: number; baseSha256: string; data: string; byteLength: number; encoding: 'utf-8' };
export type Unsubscribe = () => void;

export interface EventSource {
  handshake(request: RuntimeHandshakeRequest): Promise<RuntimeHandshakeResponse>;
  subscribe(
    afterCursor: number,
    onEvent: (event: RawProductEvent) => void,
    onError?: (error: unknown) => void,
  ): Promise<Unsubscribe>;
  getSnapshot(): Promise<RuntimeSnapshot>;
  send(envelope: RuntimeCommandEnvelope): Promise<RuntimeCommandReceipt>;
  readEditorDraft?(taskId: string, draftId: string, expectedLeaseRevision: number): Promise<EditorReadResult>;
  close(): Promise<void>;
}

export type RuntimeView = {
  connection: ConnectionState;
  product: ProductState;
  source: RuntimeSourceMetadata | null;
};

export { RuntimeClient } from './client';
export { LocalEventSource } from './local-event-source';
export type {
  LocalEventSourceOptions,
  LocalRequest,
  LocalTransport,
} from './local-event-source';
export { FetchRemoteTransport, RemoteEventSource } from './remote-event-source';
export { CyberAgentEventSource, FetchCyberAgentTransport } from './cyber-agent-event-source';
export type { CyberAgentRequest, CyberAgentTokenProvider, CyberAgentTransport, FetchCyberAgentTransportOptions } from './cyber-agent-event-source';
export { RuntimeSourceFactory, cyberAgentSourceDefinition } from './source-factory';
export type { RuntimeSourceDefinition, RuntimeSourceFactoryOptions, RuntimeSourceOption } from './source-factory';
export type {
  AccessTokenProvider,
  FetchRemoteTransportOptions,
  RemoteEventSourceOptions,
  RemoteRequest,
  RemoteTransport,
} from './remote-event-source';
export {
  classifyEventSequence,
  negotiateHandshake,
  validateCommandEnvelope,
  validateCommandReceipt,
  validateRuntimeSnapshot,
  validateSourceMetadata,
} from './conformance';
export type {
  RuntimeCommandEnvelope,
  RuntimeCommandReceipt,
  RuntimeHandshakeRequest,
  RuntimeHandshakeResponse,
  RuntimeSourceMetadata,
  RuntimeSourceMode,
} from './conformance';
