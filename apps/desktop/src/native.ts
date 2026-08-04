import { invoke as tauriInvoke } from '@tauri-apps/api/core';

export const nativeOperations = [
  'capabilities',
  'notify',
  'store_secret',
  'delete_secret',
  'export_report',
] as const;

export type NativeOperation = (typeof nativeOperations)[number];
export type NativeInvoke = (command: string, args?: Record<string, unknown>) => Promise<unknown>;
export type SecretReceipt = { id: string; stored: true };
export type NotificationKind = 'approval_required' | 'task_succeeded' | 'task_failed';

const notificationKinds: readonly string[] = ['approval_required', 'task_succeeded', 'task_failed'];
const maxReportBytes = 16 * 1024 * 1024;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hasExactKeys(value: Record<string, unknown>, keys: readonly string[]): boolean {
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  return actual.length === expected.length && actual.every((key, index) => key === expected[index]);
}

function isNativeOperation(value: string): value is NativeOperation {
  return nativeOperations.some((operation) => operation === value);
}

export function createNativeClient(invoke: NativeInvoke = tauriInvoke) {
  async function call(operation: NativeOperation, args?: Record<string, unknown>): Promise<unknown> {
    if (!isNativeOperation(operation)) {
      throw new Error(`unsupported native operation: ${operation}`);
    }
    return args === undefined ? invoke(operation) : invoke(operation, args);
  }

  return {
    call,
    async capabilities(): Promise<readonly NativeOperation[]> {
      const response = await call('capabilities');
      if (!isRecord(response) || !hasExactKeys(response, ['operations']) || !Array.isArray(response.operations)
        || response.operations.length !== nativeOperations.length
        || response.operations.some((operation, index) => operation !== nativeOperations[index])) {
        throw new Error('invalid capabilities response');
      }
      return response.operations as NativeOperation[];
    },
    async storeSecret(request: { id: string; secret: string }): Promise<SecretReceipt> {
      if (!request.id.trim() || !request.secret) {
        throw new Error('invalid store_secret request');
      }
      const response = await call('store_secret', { request });
      if (!isRecord(response) || !hasExactKeys(response, ['id', 'stored'])
        || response.id !== request.id || response.stored !== true) {
        throw new Error('invalid store_secret response');
      }
      return { id: response.id, stored: true };
    },
    async deleteSecret(id: string): Promise<{ id: string; deleted: true }> {
      if (!id.trim()) {
        throw new Error('invalid delete_secret request');
      }
      const response = await call('delete_secret', { request: { id } });
      if (!isRecord(response) || !hasExactKeys(response, ['id', 'deleted'])
        || response.id !== id || response.deleted !== true) {
        throw new Error('invalid delete_secret response');
      }
      return { id: response.id, deleted: true };
    },
    async notify(request: { kind: NotificationKind; title: string; body: string }): Promise<{ accepted: true }> {
      if (!notificationKinds.includes(request.kind) || !request.title.trim() || !request.body.trim()) {
        throw new Error('invalid notify request');
      }
      const response = await call('notify', { request });
      if (!isRecord(response) || !hasExactKeys(response, ['accepted']) || response.accepted !== true) {
        throw new Error('invalid notify response');
      }
      return { accepted: true };
    },
    async exportReport(request: { suggestedName: string; bytes: Uint8Array }): Promise<{ status: 'exported' | 'cancelled' }> {
      const { suggestedName, bytes } = request;
      if (!suggestedName.trim() || suggestedName === '.' || suggestedName === '..'
        || suggestedName.includes('/') || suggestedName.includes('\\')
        || bytes.byteLength === 0 || bytes.byteLength > maxReportBytes) {
        throw new Error('invalid export_report request');
      }
      const response = await call('export_report', {
        request: { suggestedName, bytes: Array.from(bytes) },
      });
      if (!isRecord(response) || !hasExactKeys(response, ['status'])
        || (response.status !== 'exported' && response.status !== 'cancelled')) {
        throw new Error('invalid export_report response');
      }
      return { status: response.status };
    },
  };
}
