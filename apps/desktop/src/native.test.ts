import { describe, expect, test, vi } from 'vitest';

import { createNativeClient, type NativeOperation, type NotificationKind } from './native';

describe('desktop native capability boundary', () => {
  test('exposes only the documented native operations', async () => {
    const invoke = vi.fn().mockResolvedValue({
      operations: ['capabilities', 'notify', 'store_secret', 'delete_secret', 'export_report'],
    });
    const client = createNativeClient(invoke);

    await expect(client.capabilities()).resolves.toEqual([
      'capabilities',
      'notify',
      'store_secret',
      'delete_secret',
      'export_report',
    ]);
    expect(invoke).toHaveBeenCalledWith('capabilities');
  });

  test('rejects unsupported commands before invoking Tauri', async () => {
    const invoke = vi.fn();
    const client = createNativeClient(invoke);

    await expect(client.call('shell' as NativeOperation, {})).rejects.toThrow('unsupported native operation: shell');
    expect(invoke).not.toHaveBeenCalled();
  });

  test('validates native responses instead of trusting IPC data', async () => {
    const client = createNativeClient(vi.fn().mockResolvedValue({ stored: true, secret: 'leaked' }));

    await expect(client.storeSecret({ id: 'deepseek', secret: 'top-secret' }))
      .rejects.toThrow('invalid store_secret response');
  });

  test('returns a redacted receipt after secret storage', async () => {
    const invoke = vi.fn().mockResolvedValue({ id: 'deepseek', stored: true });
    const client = createNativeClient(invoke);

    const receipt = await client.storeSecret({ id: 'deepseek', secret: 'top-secret' });

    expect(receipt).toEqual({ id: 'deepseek', stored: true });
    expect(JSON.stringify(receipt)).not.toContain('top-secret');
    expect(invoke).toHaveBeenCalledWith('store_secret', {
      request: { id: 'deepseek', secret: 'top-secret' },
    });
  });

  test('allows notifications only for approvals and terminal task states', async () => {
    const invoke = vi.fn().mockResolvedValue({ accepted: true });
    const client = createNativeClient(invoke);

    await expect(client.notify({ kind: 'chat_message' as NotificationKind, title: 'Hello', body: 'World' }))
      .rejects.toThrow('invalid notify request');
    await expect(client.notify({ kind: 'approval_required', title: 'Approval', body: 'Review scope' }))
      .resolves.toEqual({ accepted: true });
    expect(invoke).toHaveBeenCalledTimes(1);
  });

  test('rejects path traversal before report export', async () => {
    const invoke = vi.fn();
    const client = createNativeClient(invoke);

    await expect(client.exportReport({ suggestedName: '../report.md', bytes: new Uint8Array([1]) }))
      .rejects.toThrow('invalid export_report request');
    expect(invoke).not.toHaveBeenCalled();
  });

  test('copies report bytes into the bounded export request', async () => {
    const invoke = vi.fn().mockResolvedValue({ status: 'exported' });
    const client = createNativeClient(invoke);
    const bytes = new Uint8Array([67, 89, 66, 69, 82]);

    const result = await client.exportReport({ suggestedName: 'report.md', bytes });
    bytes.fill(0);

    expect(result).toEqual({ status: 'exported' });
    expect(invoke).toHaveBeenCalledWith('export_report', {
      request: { suggestedName: 'report.md', bytes: [67, 89, 66, 69, 82] },
    });
  });

  test('validates delete receipts', async () => {
    const invoke = vi.fn().mockResolvedValue({ id: 'deepseek', deleted: true });
    const client = createNativeClient(invoke);

    await expect(client.deleteSecret('deepseek')).resolves.toEqual({ id: 'deepseek', deleted: true });
    expect(invoke).toHaveBeenCalledWith('delete_secret', { request: { id: 'deepseek' } });
  });
});
