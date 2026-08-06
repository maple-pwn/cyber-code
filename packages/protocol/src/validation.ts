import type { EventSourceRef, JsonObject, KnownEventType, RawProductEvent, ValidatedProductEvent } from './index';

const isPlainObject = (value: unknown): value is JsonObject => {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
};
const isObject = isPlainObject;
const isJsonArray = (value: unknown[]): boolean => {
  if (Object.getOwnPropertySymbols(value).length > 0) return false;
  for (const key of Object.keys(value)) if (!/^(0|[1-9]\d*)$/.test(key) || Number(key) >= value.length) return false;
  for (let index = 0; index < value.length; index += 1) if (!Object.hasOwn(value, index) || !isJsonValue(value[index])) return false;
  return true;
};
const isJsonValue = (value: unknown): boolean => value === null || typeof value === 'string' || typeof value === 'boolean' || (typeof value === 'number' && Number.isFinite(value)) || (Array.isArray(value) && isJsonArray(value)) || (isPlainObject(value) && Object.getOwnPropertySymbols(value).length === 0 && Object.values(value).every(isJsonValue));
const isString = (value: unknown): value is string => typeof value === 'string' && value.length > 0;
const isStrings = (value: unknown): value is string[] => Array.isArray(value) && value.every(isString);
const knownTypes = new Set<KnownEventType>(['task.created', 'task.started', 'task.paused', 'task.resumed', 'task.cancel.requested', 'task.cancelled', 'task.completed', 'task.failed', 'task.blocked', 'scope.proposed', 'scope.confirmed', 'runtime.capabilities.updated', 'control.acquired', 'control.transferred', 'control.released', 'approval.requested', 'approval.resolved', 'question.requested', 'question.resolved', 'agent.started', 'agent.progressed', 'agent.completed', 'agent.failed', 'tool.started', 'tool.completed', 'tool.failed', 'evidence.committed', 'finding.created', 'finding.verifying', 'finding.confirmed', 'finding.rejected', 'finding.mitigated', 'report.drafted', 'report.edited', 'report.validation.failed', 'report.validated', 'report.frozen', 'report.exported', 'terminal.opened', 'terminal.output', 'terminal.input.accepted', 'terminal.resized', 'terminal.exited']);
const isKnownType = (type: string): type is KnownEventType => knownTypes.has(type as KnownEventType);
const has = (payload: JsonObject, ...keys: string[]) => keys.every((key) => payload[key] !== undefined);
const validScope = (value: unknown) => isObject(value) && isString(value.id) && isString(value.principal) && isString(value.workspace) && isString(value.validity) && isStrings(value.targets) && isStrings(value.allowedActions) && isStrings(value.deniedActions) && isString(value.riskCeiling);
const validAgent = (value: unknown) => isObject(value) && isString(value.id) && isString(value.name) && isString(value.status);
const validEvidence = (value: unknown) => isObject(value) && isString(value.id) && isString(value.taskId) && isString(value.kind) && isString(value.summary) && isObject(value.data);
const validFinding = (value: unknown) => isObject(value) && isString(value.id) && isString(value.title) && isString(value.severity) && ['candidate', 'verifying', 'confirmed', 'rejected', 'mitigated'].includes(value.status as string) && isString(value.confidence) && isStrings(value.evidenceIds);
const validLease = (value: unknown) => isObject(value) && isString(value.clientId) && Number.isSafeInteger(value.revision) && (value.revision as number) > 0;
const validChallenge = (value: unknown) => isObject(value) && has(value, 'id', 'agentId', 'action', 'target', 'parameterDigest', 'risk', 'expiresAt') && ['id', 'agentId', 'action', 'target', 'parameterDigest', 'risk', 'expiresAt'].every((key) => isString(value[key])) && !Number.isNaN(Date.parse(value.expiresAt as string));
const validReport = (value: unknown) => isObject(value) && isString(value.id) && isString(value.taskId) && Number.isSafeInteger(value.version) && (value.version as number) >= 0 && (value.status === 'draft' || value.status === 'frozen') && typeof value.narrative === 'string' && typeof value.recommendations === 'string' && typeof value.humanNotes === 'string' && Array.isArray(value.findings);
const exactKeys = (value: JsonObject, keys: string[]) => Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key));
const auditableText = (value: unknown): value is string => {
  if (!isString(value)) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint <= 0x1f || (codePoint >= 0x7f && codePoint <= 0x9f)
      || codePoint === 0x2028 || codePoint === 0x2029) return false;
  }
  return true;
};
const safeTerminalIdentifier = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(value);
const positiveInteger = (value: unknown, maximum = Number.MAX_SAFE_INTEGER) => Number.isSafeInteger(value) && (value as number) > 0 && (value as number) <= maximum;
const terminalSize = (value: unknown) => positiveInteger(value, 1000);
const decodedBase64Length = (value: string): number | null => {
  if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value)) return null;
  return (value.length / 4) * 3 - (value.endsWith('==') ? 2 : value.endsWith('=') ? 1 : 0);
};
const validTerminalSession = (value: unknown) => isObject(value)
  && exactKeys(value, ['id', 'profileId', 'processId', 'workingDirectory', 'scopeId', 'ownerClientId', 'leaseRevision', 'columns', 'rows', 'outputLimitBytes'])
  && ['id', 'processId', 'workingDirectory', 'scopeId', 'ownerClientId'].every((key) => auditableText(value[key]))
  && safeTerminalIdentifier(value.profileId)
  && positiveInteger(value.leaseRevision) && terminalSize(value.columns) && terminalSize(value.rows)
  && positiveInteger(value.outputLimitBytes, 64 * 1024 * 1024);

function isValidPayload(type: KnownEventType, payload: JsonObject): boolean {
  switch (type) {
    case 'task.created': case 'task.started': return isString(payload.title);
    case 'task.failed': case 'task.blocked': case 'tool.failed': case 'agent.failed': case 'report.validation.failed': return isString(payload.reason) && (type !== 'agent.failed' || isString(payload.agentId)) && (type !== 'tool.failed' || isString(payload.callId)) && (type !== 'report.validation.failed' || isString(payload.reportId));
    case 'scope.proposed': case 'scope.confirmed': return validScope(payload.scope);
    case 'runtime.capabilities.updated': return isStrings(payload.capabilities);
    case 'control.acquired': case 'control.transferred': return validLease(payload.lease);
    case 'control.released': return isString(payload.clientId);
    case 'approval.requested': return validChallenge(payload.challenge);
    case 'approval.resolved': return isString(payload.challengeId) && (payload.decision === 'allow_once' || payload.decision === 'deny');
    case 'question.requested': return isString(payload.questionId) && isString(payload.prompt);
    case 'question.resolved': return isString(payload.questionId) && isString(payload.answer);
    case 'agent.started': return validAgent(payload.agent);
    case 'agent.progressed': return isString(payload.agentId) && typeof payload.progress === 'number';
    case 'agent.completed': return isString(payload.agentId);
    case 'tool.started': return isString(payload.callId) && isString(payload.name);
    case 'tool.completed': return isString(payload.callId) && typeof payload.success === 'boolean' && isStrings(payload.evidenceIds);
    case 'evidence.committed': return validEvidence(payload.evidence);
    case 'finding.created': return validFinding(payload.finding);
    case 'finding.verifying': case 'finding.confirmed': case 'finding.mitigated': return isString(payload.findingId);
    case 'finding.rejected': return isString(payload.findingId) && isString(payload.reason);
    case 'report.drafted': case 'report.edited': return validReport(payload.report);
    case 'report.validated': return isString(payload.reportId);
    case 'report.frozen': return isString(payload.reportId) && Number.isSafeInteger(payload.version) && (payload.version as number) > 0;
    case 'report.exported': return isString(payload.reportId) && isString(payload.format);
    case 'terminal.opened': return exactKeys(payload, ['session']) && validTerminalSession(payload.session);
    case 'terminal.output': {
      if (!exactKeys(payload, ['sessionId', 'sequence', 'data', 'byteLength']) || !auditableText(payload.sessionId) || !positiveInteger(payload.sequence) || !positiveInteger(payload.byteLength, 1024 * 1024) || typeof payload.data !== 'string') return false;
      return decodedBase64Length(payload.data) === payload.byteLength;
    }
    case 'terminal.input.accepted': return exactKeys(payload, ['sessionId', 'sequence', 'byteLength', 'sha256']) && auditableText(payload.sessionId) && positiveInteger(payload.sequence) && positiveInteger(payload.byteLength, 1024 * 1024) && typeof payload.sha256 === 'string' && /^[a-f0-9]{64}$/.test(payload.sha256);
    case 'terminal.resized': return exactKeys(payload, ['sessionId', 'columns', 'rows']) && auditableText(payload.sessionId) && terminalSize(payload.columns) && terminalSize(payload.rows);
    case 'terminal.exited': return exactKeys(payload, ['sessionId', 'exitCode', 'reason']) && auditableText(payload.sessionId) && Number.isSafeInteger(payload.exitCode) && auditableText(payload.reason);
    default: return Object.keys(payload).length === 0;
  }
}

export function validateEvent(raw: RawProductEvent): ValidatedProductEvent {
  if (raw.schemaVersion !== 1) throw new Error('unsupported_schema_version');
  if (!isString(raw.eventId) || !isString(raw.taskId) || typeof raw.cursor !== 'number' || !Number.isSafeInteger(raw.cursor) || raw.cursor < 1 || !isString(raw.occurredAt) || Number.isNaN(Date.parse(raw.occurredAt)) || !isString(raw.type) || !isObject(raw.source) || !isString(raw.source.runtimeId) || !isObject(raw.payload) || !isJsonValue(raw)) throw new Error('invalid_event');
  const source: EventSourceRef = { runtimeId: raw.source.runtimeId };
  if (raw.source.agentId !== undefined) { if (!isString(raw.source.agentId)) throw new Error('invalid_event'); source.agentId = raw.source.agentId; }
  if (raw.source.toolCallId !== undefined) { if (!isString(raw.source.toolCallId)) throw new Error('invalid_event'); source.toolCallId = raw.source.toolCallId; }
  const event = { schemaVersion: 1 as const, eventId: raw.eventId, taskId: raw.taskId, cursor: raw.cursor, occurredAt: raw.occurredAt, type: raw.type, source, payload: raw.payload };
  if (isKnownType(raw.type)) { if (!isValidPayload(raw.type, raw.payload)) throw new Error('invalid_event'); return { ...event, kind: 'known' } as ValidatedProductEvent; }
  return { ...event, kind: 'unknown' };
}
