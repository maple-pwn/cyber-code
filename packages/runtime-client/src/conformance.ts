import {
  project,
  validateEvent,
  type ProductState,
  type RawProductEvent,
} from '@cyber/protocol';

import type { RuntimeCommand, RuntimeSnapshot } from './index';

export type RuntimeSourceMode = 'demo' | 'local' | 'remote';

export type RuntimeSourceMetadata = {
  mode: RuntimeSourceMode;
  runtimeId: string;
  principal: string;
  capabilities: string[];
};

export type RuntimeHandshakeRequest = {
  supportedProtocolVersions: number[];
  afterCursor: number;
};

export type RuntimeHandshakeResponse = {
  protocolVersion: number;
  runtimeId: string;
  principal: string;
  role: string;
  capabilities: string[];
  source: RuntimeSourceMetadata;
};

export type RuntimeCommandEnvelope = {
  idempotencyKey: string;
  command: RuntimeCommand;
};

export type RuntimeCommandReceipt =
  | { idempotencyKey: string; status: 'accepted'; errorCode?: never }
  | { idempotencyKey: string; status: 'rejected'; errorCode: string };

const nonEmpty = (value: unknown): value is string =>
  typeof value === 'string' && value.trim().length > 0;

const stringList = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every(nonEmpty);

const safeCursor = (value: unknown): value is number =>
  Number.isSafeInteger(value) && (value as number) >= 0;

const exactKeys = (value: Record<string, unknown>, required: string[], optional: string[] = []): boolean => {
  const allowed = new Set([...required, ...optional]);
  return required.every((key) => Object.hasOwn(value, key))
    && Object.keys(value).every((key) => allowed.has(key));
};

const validCommand = (value: unknown): value is RuntimeCommand => {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
  const command = value as Record<string, unknown>;
  switch (command.type) {
    case 'task.create':
      return exactKeys(command, ['type', 'objective', 'runtimeId'], ['workspace'])
        && nonEmpty(command.objective)
        && nonEmpty(command.runtimeId)
        && (command.workspace === undefined || nonEmpty(command.workspace));
    case 'scope.confirm':
      return exactKeys(command, ['type', 'scopeId']) && nonEmpty(command.scopeId);
    case 'task.pause':
    case 'task.resume':
    case 'task.cancel':
      return exactKeys(command, ['type']);
    case 'approval.respond':
      return exactKeys(command, ['type', 'challengeId', 'decision'])
        && nonEmpty(command.challengeId)
        && (command.decision === 'allow_once' || command.decision === 'deny');
    case 'control.take':
      return exactKeys(command, ['type', 'expectedRevision'])
        && Number.isSafeInteger(command.expectedRevision)
        && (command.expectedRevision as number) >= 0;
    case 'instruction.send':
      return exactKeys(command, ['type', 'content']) && nonEmpty(command.content);
    default:
      return false;
  }
};

export function validateSourceMetadata(value: RuntimeSourceMetadata): RuntimeSourceMetadata {
  if (!['demo', 'local', 'remote'].includes(value.mode)
    || !nonEmpty(value.runtimeId)
    || !nonEmpty(value.principal)
    || !stringList(value.capabilities)) {
    throw new Error('invalid_source_metadata');
  }
  return value;
}

export function negotiateHandshake(
  request: RuntimeHandshakeRequest,
  response: RuntimeHandshakeResponse,
): RuntimeSourceMetadata {
  if (!Array.isArray(request.supportedProtocolVersions)
    || request.supportedProtocolVersions.length === 0
    || !request.supportedProtocolVersions.every((version) => Number.isSafeInteger(version) && version > 0)
    || !safeCursor(request.afterCursor)) {
    throw new Error('invalid_handshake_request');
  }
  if (!request.supportedProtocolVersions.includes(response.protocolVersion)) {
    throw new Error('incompatible');
  }
  if (!nonEmpty(response.runtimeId)
    || !nonEmpty(response.principal)
    || !nonEmpty(response.role)
    || !stringList(response.capabilities)) {
    throw new Error('invalid_handshake_response');
  }
  const metadata = validateSourceMetadata(response.source);
  if (metadata.runtimeId !== response.runtimeId
    || metadata.principal !== response.principal
    || JSON.stringify(metadata.capabilities) !== JSON.stringify(response.capabilities)) {
    throw new Error('runtime_identity_mismatch');
  }
  return metadata;
}

export function classifyEventSequence(
  initial: ProductState,
  rawEvents: unknown[],
): { state: ProductState; outcomes: ('applied' | 'duplicate' | 'resync-required')[] } {
  let state = initial;
  const outcomes: ('applied' | 'duplicate' | 'resync-required')[] = [];
  for (const raw of rawEvents) {
    const result = project(state, validateEvent(raw as RawProductEvent));
    outcomes.push(result.kind);
    if (result.kind !== 'resync-required') state = result.state;
  }
  return { state, outcomes };
}

export function validateRuntimeSnapshot(
  snapshot: RuntimeSnapshot,
  lastTrustedCursor: number,
): RuntimeSnapshot {
  if (!safeCursor(snapshot.cursor)) throw new Error('invalid_snapshot_cursor');
  if (!safeCursor(lastTrustedCursor)) throw new Error('invalid_trusted_cursor');
  if (snapshot.cursor < lastTrustedCursor) throw new Error('snapshot_cursor_behind');
  if (snapshot.state.committedCursor !== snapshot.cursor) throw new Error('snapshot_cursor_mismatch');
  return snapshot;
}

export function validateCommandEnvelope(envelope: RuntimeCommandEnvelope): RuntimeCommandEnvelope {
  if (!nonEmpty(envelope.idempotencyKey) || !validCommand(envelope.command)) {
    throw new Error('invalid_command_envelope');
  }
  return envelope;
}

export function validateCommandReceipt(
  receipt: RuntimeCommandReceipt,
  envelope: RuntimeCommandEnvelope,
): RuntimeCommandReceipt {
  validateCommandEnvelope(envelope);
  if (!nonEmpty(receipt.idempotencyKey)
    || (receipt.status !== 'accepted' && receipt.status !== 'rejected')) {
    throw new Error('invalid_command_receipt');
  }
  if (receipt.idempotencyKey !== envelope.idempotencyKey) {
    throw new Error('receipt_idempotency_mismatch');
  }
  if (receipt.status === 'rejected' && !nonEmpty(receipt.errorCode)) {
    throw new Error('invalid_command_receipt');
  }
  if (receipt.status === 'accepted' && 'errorCode' in receipt && receipt.errorCode !== undefined) {
    throw new Error('invalid_command_receipt');
  }
  return receipt;
}
