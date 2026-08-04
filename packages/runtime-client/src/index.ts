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
  | { type: 'instruction.send'; content: string };

export type RuntimeSnapshot = { cursor: number; state: ProductState };
export type Unsubscribe = () => void;

export interface EventSource {
  handshake(request: RuntimeHandshakeRequest): Promise<RuntimeHandshakeResponse>;
  subscribe(afterCursor: number, onEvent: (event: RawProductEvent) => void): Promise<Unsubscribe>;
  getSnapshot(): Promise<RuntimeSnapshot>;
  send(envelope: RuntimeCommandEnvelope): Promise<RuntimeCommandReceipt>;
  close(): Promise<void>;
}

export type RuntimeView = {
  connection: ConnectionState;
  product: ProductState;
  source: RuntimeSourceMetadata | null;
};

export { RuntimeClient } from './client';
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
