import type {
  ApprovalDecision,
  ProductState,
  RawProductEvent,
} from '@cyber/protocol';

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
  subscribe(afterCursor: number, onEvent: (event: RawProductEvent) => void): Promise<Unsubscribe>;
  getSnapshot(): Promise<RuntimeSnapshot>;
  send(command: RuntimeCommand): Promise<void>;
  close(): Promise<void>;
}

export type RuntimeView = { connection: ConnectionState; product: ProductState };

export { RuntimeClient } from './client';
