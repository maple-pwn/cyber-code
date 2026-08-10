import { describe, expect, test } from 'vitest';

import { validateCommandEnvelope, type RuntimeCommandEnvelope } from './conformance';

const digest = (character: string) => character.repeat(64);
const reference = { findingId: 'finding-1', evidenceId: 'evidence-1', startLine: 4, endLine: 8 };

describe('editor command contract', () => {
  test.each([
    { type: 'editor.open', draftId: 'draft-1', path: '/lab/app.go', scopeId: 'scope-1', evidenceReferences: [reference], expectedLeaseRevision: 2 },
    { type: 'editor.save', draftId: 'draft-1', revision: 1, baseSha256: digest('a'), data: 'aGVsbG8K', byteLength: 6, expectedLeaseRevision: 2 },
    { type: 'editor.apply', draftId: 'draft-1', revision: 1, proposedSha256: digest('b'), expectedLeaseRevision: 2 },
    { type: 'editor.discard', draftId: 'draft-1', expectedLeaseRevision: 2 },
  ])('accepts structured $type commands', (command) => {
    expect(validateCommandEnvelope({ idempotencyKey: `cmd-${command.type}`, command } as unknown as RuntimeCommandEnvelope).command).toEqual(command);
  });

  test('rejects malformed content and forged editor authority', () => {
    for (const command of [
      { type: 'editor.save', draftId: 'draft-1', revision: 1, baseSha256: digest('a'), data: 'not base64', byteLength: 6, expectedLeaseRevision: 2 },
      { type: 'editor.apply', draftId: 'draft-1', revision: 1, proposedSha256: digest('b'), expectedLeaseRevision: 2, reviewer: 'forged' },
      { type: 'editor.open', draftId: 'draft-1', path: '/lab/app.go', scopeId: 'scope-1', evidenceReferences: [{ ...reference, startLine: 9, endLine: 8 }], expectedLeaseRevision: 2 },
    ]) expect(() => validateCommandEnvelope({ idempotencyKey: 'cmd-editor', command } as unknown as RuntimeCommandEnvelope)).toThrow('invalid_command_envelope');
  });
});
