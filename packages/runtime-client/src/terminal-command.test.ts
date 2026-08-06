import { describe, expect, test } from 'vitest';

import { validateCommandEnvelope, type RuntimeCommandEnvelope } from './conformance';

describe('terminal command contract', () => {
  test.each([
    { type: 'terminal.open', sessionId: 'terminal-1', profileId: 'default-shell', workingDirectory: '/lab', scopeId: 'scope-1', columns: 120, rows: 40, outputLimitBytes: 1_048_576, expectedLeaseRevision: 2 },
    { type: 'terminal.input', sessionId: 'terminal-1', sequence: 1, data: 'bHMK', byteLength: 3, expectedLeaseRevision: 2 },
    { type: 'terminal.resize', sessionId: 'terminal-1', columns: 100, rows: 30, expectedLeaseRevision: 2 },
    { type: 'terminal.cancel', sessionId: 'terminal-1', expectedLeaseRevision: 2 },
  ])('accepts structured $type commands', (command) => {
    expect(validateCommandEnvelope({ idempotencyKey: `cmd-${command.type}`, command } as unknown as RuntimeCommandEnvelope).command).toEqual(command);
  });

  test('rejects arbitrary shell strings and forged authority fields', () => {
    for (const command of [
      { type: 'terminal.open', command: 'sh -c "rm -rf /"', workingDirectory: '/lab' },
      { type: 'terminal.open', sessionId: 'terminal-1', profileId: 'sh -i', workingDirectory: '/lab', scopeId: 'scope-1', columns: 120, rows: 40, outputLimitBytes: 1_048_576, expectedLeaseRevision: 2 },
      { type: 'terminal.open', sessionId: 'terminal-1', profileId: 'default-shell', workingDirectory: '/lab', scopeId: 'scope-1', columns: 120, rows: 40, outputLimitBytes: 1_048_576, expectedLeaseRevision: 2, approval: 'allow' },
    ]) expect(() => validateCommandEnvelope({ idempotencyKey: 'cmd-terminal', command } as unknown as RuntimeCommandEnvelope)).toThrow('invalid_command_envelope');
  });
});
