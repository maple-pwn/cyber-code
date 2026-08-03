import { expect, test } from 'vitest';

import { packageName } from './index';

test('exports its workspace package name', () => {
  expect(packageName).toBe('@cyber/protocol');
});
