import { describe, expect, test } from 'vitest';

import { initialProductState, project, validateEvent, type RawProductEvent } from './index';

const rawTerminalEvent = (cursor: number, type: string, payload: Record<string, unknown>): RawProductEvent => ({
  schemaVersion: 1,
  eventId: `terminal-event-${cursor}`,
  taskId: 'task-1',
  cursor,
  occurredAt: '2026-08-05T12:00:00Z',
  type,
  source: { runtimeId: 'runtime-1' },
  payload,
});

describe('terminal protocol', () => {
  test('validates and projects an audited terminal lifecycle', () => {
    const events = [
      rawTerminalEvent(1, 'terminal.opened', { session: {
        id: 'terminal-1', profileId: 'default-shell', processId: 'pid-42',
        workingDirectory: '/lab', scopeId: 'scope-1', ownerClientId: 'client-1',
        leaseRevision: 2, columns: 120, rows: 40, outputLimitBytes: 1_048_576,
      } }),
      rawTerminalEvent(2, 'terminal.output', {
        sessionId: 'terminal-1', sequence: 1, data: 'b2sK', byteLength: 3,
      }),
      rawTerminalEvent(3, 'terminal.input.accepted', {
        sessionId: 'terminal-1', sequence: 1, byteLength: 3,
        sha256: 'dc51b8c96c2d745df9b5fd1680149e1a2a4e08ef294c5b49f7154f36f330a112',
      }),
      rawTerminalEvent(4, 'terminal.resized', { sessionId: 'terminal-1', columns: 100, rows: 30 }),
      rawTerminalEvent(5, 'terminal.exited', { sessionId: 'terminal-1', exitCode: 0, reason: 'exited' }),
    ];

    let state = initialProductState();
    for (const raw of events) state = project(state, validateEvent(raw)).state;
    const terminals = (state as unknown as { terminals: Record<string, {
      status: string; columns: number; rows: number; outputBytes: number;
      nextInputSequence: number; nextOutputSequence: number;
    }> }).terminals;

    expect(terminals['terminal-1']).toMatchObject({
      status: 'exited', columns: 100, rows: 30, outputBytes: 3,
      nextInputSequence: 2, nextOutputSequence: 2,
    });
  });

  test('rejects malformed byte streams and unsafe terminal metadata', () => {
    const session = {
      id: 'terminal-1', profileId: 'default-shell', processId: 'pid-42',
      workingDirectory: '/lab', scopeId: 'scope-1', ownerClientId: 'client-1',
      leaseRevision: 1, columns: 120, rows: 40, outputLimitBytes: 1_048_576,
    };
    expect(() => validateEvent(rawTerminalEvent(1, 'terminal.opened', { session: { ...session, profileId: 'sh\n-i' } }))).toThrow('invalid_event');
    expect(() => validateEvent(rawTerminalEvent(1, 'terminal.opened', { session: { ...session, profileId: 'sh -i' } }))).toThrow('invalid_event');
    expect(() => validateEvent(rawTerminalEvent(1, 'terminal.output', {
      sessionId: 'terminal-1', sequence: 0, data: 'not base64', byteLength: 3,
    }))).toThrow('invalid_event');
    expect(() => validateEvent(rawTerminalEvent(1, 'terminal.input.accepted', {
      sessionId: 'terminal-1', sequence: 1, byteLength: 3, sha256: 'short',
    }))).toThrow('invalid_event');
  });

  test('rejects out-of-order terminal output', () => {
    const opened = validateEvent(rawTerminalEvent(1, 'terminal.opened', { session: {
      id: 'terminal-1', profileId: 'default-shell', processId: 'pid-42',
      workingDirectory: '/lab', scopeId: 'scope-1', ownerClientId: 'client-1',
      leaseRevision: 1, columns: 120, rows: 40, outputLimitBytes: 1_048_576,
    } }));
    const state = project(initialProductState(), opened).state;
    const output = validateEvent(rawTerminalEvent(2, 'terminal.output', {
      sessionId: 'terminal-1', sequence: 2, data: 'b2sK', byteLength: 3,
    }));
    expect(() => project(state, output)).toThrow('terminal_output_sequence');
  });
});
