import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, test } from 'vitest';
import { initialProductState } from '@cyber/protocol';

import {
  classifyEventSequence,
  negotiateHandshake,
  validateCommandEnvelope,
  validateCommandReceipt,
  validateRuntimeSnapshot,
  type RuntimeCommandEnvelope,
  type RuntimeCommandReceipt,
  type RuntimeHandshakeRequest,
  type RuntimeHandshakeResponse,
} from './conformance';

const fixtureRoot = join(process.cwd(), 'tests', 'fixtures', 'runtime-conformance');
const fixture = <T>(name: string): T => JSON.parse(
  readFileSync(join(fixtureRoot, name), 'utf8'),
) as T;

describe('runtime source conformance contract', () => {
  test('freezes every required conformance case', () => {
    const manifest = fixture<{ cases: string[] }>('manifest.json');
    expect(manifest.cases).toEqual([
      'handshake-negotiation',
      'replay-after-cursor',
      'exact-duplicate',
      'gap-recovery',
      'snapshot-mismatch',
      'unknown-event',
      'command-rejection',
      'expired-approval',
      'stale-lease',
      'cancellation',
      'reconnect-during-active-execution',
    ]);
  });

  test('negotiates a supported handshake and retains auditable source metadata', () => {
    const value = fixture<{
      request: RuntimeHandshakeRequest;
      response: RuntimeHandshakeResponse;
    }>('handshake.json');

    expect(negotiateHandshake(value.request, value.response)).toEqual(value.response.source);
    expect(() => negotiateHandshake(
      { ...value.request, supportedProtocolVersions: [2] },
      value.response,
    )).toThrow('incompatible');
    expect(() => negotiateHandshake(value.request, {
      ...value.response,
      runtimeId: 'runtime-substituted',
    })).toThrow('runtime_identity_mismatch');
  });

  test.each([
    ['runtimeId', 'runtime-1\nforged'],
    ['principal', 'operator\rforged'],
    ['role', 'owner\tforged'],
    ['capabilities', ['events', 'reports\u0000forged']],
  ] as const)('rejects control characters in handshake %s', (field, value) => {
    const fixtureValue = fixture<{
      request: RuntimeHandshakeRequest;
      response: RuntimeHandshakeResponse;
    }>('handshake.json');
    const response = { ...fixtureValue.response, [field]: value };
    if (field === 'runtimeId' || field === 'principal') {
      response.source = { ...response.source, [field]: value };
    }
    if (field === 'capabilities') {
      response.source = { ...response.source, capabilities: [...value] };
    }
    expect(() => negotiateHandshake(fixtureValue.request, response)).toThrow('invalid_handshake_response');
  });

  test.each([
    ['duplicate-replay.json', ['applied', 'duplicate']],
    ['gap-recovery.json', ['applied', 'resync-required', 'applied', 'applied']],
    ['unknown-event.json', ['applied', 'applied']],
  ] as const)('classifies replay continuity from %s', (name, expected) => {
    const events = fixture<{ events: unknown[] }>(`../product-events/${name}`).events;
    const result = classifyEventSequence(initialProductState(), events);
    expect(result.outcomes).toEqual(expected);
    if (name === 'unknown-event.json') expect(result.state.rawEvents).toHaveLength(1);
  });

  test('rejects a snapshot whose cursor does not match its state', () => {
    expect(() => validateRuntimeSnapshot({
      cursor: 1,
      state: initialProductState(),
    }, 0)).toThrow('snapshot_cursor_mismatch');
  });

  test('validates accepted and rejected command receipts against their idempotency key', () => {
    const value = fixture<{
      accepted: { envelope: RuntimeCommandEnvelope; receipt: RuntimeCommandReceipt };
      rejected: { name: string; envelope: RuntimeCommandEnvelope; receipt: RuntimeCommandReceipt }[];
    }>('commands.json');

    expect(validateCommandReceipt(
      value.accepted.receipt,
      validateCommandEnvelope(value.accepted.envelope),
    ).status).toBe('accepted');
    for (const item of value.rejected) {
      expect(validateCommandReceipt(
        item.receipt,
        validateCommandEnvelope(item.envelope),
      )).toEqual(item.receipt);
    }
    expect(() => validateCommandReceipt(
      { ...value.accepted.receipt, idempotencyKey: 'cmd-other' },
      value.accepted.envelope,
    )).toThrow('receipt_idempotency_mismatch');
  });

  test.each(['eventId', 'cursor'] as const)('rejects client-forged %s fields', (field) => {
    expect(() => validateCommandEnvelope({
      idempotencyKey: `cmd-forged-${field}`,
      command: { type: 'task.cancel', [field]: field === 'cursor' ? 99 : 'event-forged' },
    } as RuntimeCommandEnvelope)).toThrow('invalid_command_envelope');
  });
});
