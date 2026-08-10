import { describe, expect, test, vi } from 'vitest';

import type { LocalRequest } from '@cyber/runtime-client';

import { createNativeClient, type NativeOperation, type NotificationKind } from './native';

describe('desktop native capability boundary', () => {
  test('exposes only the documented native operations', async () => {
    const invoke = vi.fn().mockResolvedValue({
      operations: [
        'capabilities',
        'notify',
        'store_secret',
        'load_secret',
        'delete_secret',
        'export_report',
        'pick_inputs',
        'runtime_start',
        'runtime_request',
        'runtime_restart',
        'runtime_stop',
        'cyber_agent_start',
        'cyber_agent_stop',
      ],
    });
    const client = createNativeClient(invoke);

    await expect(client.capabilities()).resolves.toEqual([
      'capabilities',
      'notify',
      'store_secret',
      'load_secret',
      'delete_secret',
      'export_report',
      'pick_inputs',
      'runtime_start',
      'runtime_request',
      'runtime_restart',
      'runtime_stop',
      'cyber_agent_start',
      'cyber_agent_stop',
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

  test('loads a credential only into caller memory', async () => {
    const invoke = vi.fn().mockResolvedValue({ id: 'remote-primary', secret: 'remote-token' });
    const client = createNativeClient(invoke);

    await expect(client.loadSecret('remote-primary')).resolves.toEqual({
      id: 'remote-primary',
      secret: 'remote-token',
    });
    expect(invoke).toHaveBeenCalledWith('load_secret', { request: { id: 'remote-primary' } });
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
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

  test('returns bounded task inputs selected by the native host', async () => {
    const invoke = vi.fn().mockResolvedValue([{ filename: 'api.yaml', mediaType: 'application/yaml', bytes: [1, 2, 3] }]);
    const client = createNativeClient(invoke);
    await expect(client.pickInputs()).resolves.toEqual([{ filename: 'api.yaml', mediaType: 'application/yaml', bytes: new Uint8Array([1, 2, 3]) }]);
    expect(invoke).toHaveBeenCalledWith('pick_inputs');
  });

  test('validates delete receipts', async () => {
    const invoke = vi.fn().mockResolvedValue({ id: 'deepseek', deleted: true });
    const client = createNativeClient(invoke);

    await expect(client.deleteSecret('deepseek')).resolves.toEqual({ id: 'deepseek', deleted: true });
    expect(invoke).toHaveBeenCalledWith('delete_secret', { request: { id: 'deepseek' } });
  });

  test('exposes the narrow runtime process bridge without accepting a bearer', async () => {
    const invoke = vi.fn()
      .mockResolvedValueOnce({ id: 'desktop-startup', type: 'handshake', handshake: {} })
      .mockResolvedValueOnce({ id: 'local-1', type: 'events', events: [] })
      .mockResolvedValueOnce({ id: 'desktop-restart', type: 'handshake', handshake: {} })
      .mockResolvedValueOnce({ stopped: true });
    const client = createNativeClient(invoke);

    await client.runtimeStart();
    await client.runtimeRequest({ id: 'local-1', type: 'events', afterCursor: 0 });
    await client.runtimeRestart();
    await expect(client.runtimeStop()).resolves.toEqual({ stopped: true });

    expect(invoke.mock.calls).toEqual([
      ['runtime_start'],
      ['runtime_request', { request: { id: 'local-1', type: 'events', afterCursor: 0 } }],
      ['runtime_restart'],
      ['runtime_stop'],
    ]);
    await expect(client.runtimeRequest({
      id: 'forged',
      type: 'health',
      bearer: 'forged',
    } as never)).rejects.toThrow('invalid runtime_request request');
  });

  test('allows only a bounded editor draft read through the runtime bridge', async () => {
    const invoke = vi.fn().mockResolvedValue({ id: 'editor-1', type: 'editor', editor: {} });
    const client = createNativeClient(invoke);

    await client.runtimeRequest({
      id: 'editor-1', type: 'editor', taskId: 'task-1', draftId: 'draft-1', expectedLeaseRevision: 2,
    });

    expect(invoke).toHaveBeenCalledWith('runtime_request', { request: {
      id: 'editor-1', type: 'editor', taskId: 'task-1', draftId: 'draft-1', expectedLeaseRevision: 2,
    } });
    await expect(client.runtimeRequest({
      id: 'editor-bad', type: 'editor', taskId: '../task', draftId: 'draft-1', expectedLeaseRevision: 0,
    })).rejects.toThrow('invalid runtime_request request');
  });

  test('rejects malformed runtime stop receipts', async () => {
    const client = createNativeClient(vi.fn().mockResolvedValue({ stopped: 'yes' }));

    await expect(client.runtimeStop()).rejects.toThrow('invalid runtime_stop response');
  });

  test('starts and stops the bundled cyber-agent through validated receipts', async () => {
    const invoke = vi.fn()
      .mockResolvedValueOnce({ endpoint: 'http://127.0.0.1:43210', token: 'session-token', version: '1.0.0' })
      .mockResolvedValueOnce({ stopped: true });
    const client = createNativeClient(invoke);

    await expect(client.cyberAgentStart()).resolves.toEqual({
      endpoint: 'http://127.0.0.1:43210',
      token: 'session-token',
      version: '1.0.0',
    });
    await expect(client.cyberAgentStop()).resolves.toEqual({ stopped: true });
    expect(invoke.mock.calls).toEqual([
      ['cyber_agent_start'],
      ['cyber_agent_stop'],
    ]);
  });

  test('rejects malformed cyber-agent lifecycle receipts', async () => {
    const client = createNativeClient(vi.fn()
      .mockResolvedValueOnce({ endpoint: 'http://127.0.0.1:43210', token: '', version: '1.0.0' })
      .mockResolvedValueOnce({ stopped: 'yes' }));

    await expect(client.cyberAgentStart()).rejects.toThrow('invalid cyber_agent_start response');
    await expect(client.cyberAgentStop()).rejects.toThrow('invalid cyber_agent_stop response');
  });

  test('rejects malformed native input selections', async () => {
    const client = createNativeClient(vi.fn().mockResolvedValue([
      { filename: 'api.yaml', mediaType: 'application/yaml', bytes: [1, -1, 256] },
    ]));

    await expect(client.pickInputs()).rejects.toThrow('invalid pick_inputs response');
  });

  test('rejects empty native requests before crossing the IPC boundary', async () => {
    const invoke = vi.fn();
    const client = createNativeClient(invoke);

    await expect(client.storeSecret({ id: ' ', secret: 'secret' })).rejects.toThrow('invalid store_secret request');
    await expect(client.loadSecret(' ')).rejects.toThrow('invalid load_secret request');
    await expect(client.deleteSecret(' ')).rejects.toThrow('invalid delete_secret request');
    await expect(client.notify({ kind: 'task_succeeded', title: ' ', body: 'Done' })).rejects.toThrow('invalid notify request');
    expect(invoke).not.toHaveBeenCalled();
  });

  test('rejects malformed native host responses across capability methods', async () => {
    const invoke = vi.fn()
      .mockResolvedValueOnce({ operations: ['capabilities'] })
      .mockResolvedValueOnce({ id: 'remote-primary', secret: '' })
      .mockResolvedValueOnce({ id: 'remote-primary', deleted: false })
      .mockResolvedValueOnce({ accepted: false })
      .mockResolvedValueOnce({ status: 'written' })
      .mockResolvedValueOnce({ filename: 'not-an-array' });
    const client = createNativeClient(invoke);

    await expect(client.capabilities()).rejects.toThrow('invalid capabilities response');
    await expect(client.loadSecret('remote-primary')).rejects.toThrow('invalid load_secret response');
    await expect(client.deleteSecret('remote-primary')).rejects.toThrow('invalid delete_secret response');
    await expect(client.notify({ kind: 'task_failed', title: 'Task failed', body: 'Review logs' }))
      .rejects.toThrow('invalid notify response');
    await expect(client.exportReport({ suggestedName: 'report.md', bytes: new Uint8Array([1]) }))
      .rejects.toThrow('invalid export_report response');
    await expect(client.pickInputs()).rejects.toThrow('invalid pick_inputs response');
  });

  test('validates every bounded runtime request variant before invoking the host', async () => {
    const invoke = vi.fn().mockResolvedValue({ ok: true });
    const client = createNativeClient(invoke);
    const requests: LocalRequest[] = [
      { id: 'handshake-1', type: 'handshake', handshake: { supportedProtocolVersions: [1], afterCursor: 0 } },
      { id: 'snapshot-1', type: 'snapshot' },
      { id: 'command-1', type: 'command', command: { idempotencyKey: 'pause-1', command: { type: 'task.pause' } } },
    ];

    for (const request of requests) await client.runtimeRequest(request);

    expect(invoke).toHaveBeenCalledTimes(requests.length);
    await expect(client.runtimeRequest({ id: 'unknown-1', type: 'unknown' } as never))
      .rejects.toThrow('invalid runtime_request request');
  });
});
