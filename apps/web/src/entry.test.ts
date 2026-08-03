import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { expect, test } from 'vitest';

test('loads the React application entry from index.html', () => {
  const root = process.cwd().endsWith('/apps/web') ? process.cwd() : resolve(process.cwd(), 'apps/web');
  const html = readFileSync(resolve(root, 'index.html'), 'utf8');
  expect(html).toContain('<script type="module" src="/src/main.tsx"></script>');
});
