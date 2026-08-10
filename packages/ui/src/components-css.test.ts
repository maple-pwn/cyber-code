/// <reference types="node" />

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, test } from 'vitest';

const stylePath = process.cwd().endsWith('/packages/ui')
  ? resolve(process.cwd(), 'src/components.css')
  : resolve(process.cwd(), 'packages/ui/src/components.css');
const css = readFileSync(stylePath, 'utf8');

describe('report content containment', () => {
  test('allows every report component layer to shrink inside its parent', () => {
    expect(css).toMatch(/\.cyber-report-editor\s*\{[^}]*min-width:\s*0;[^}]*max-width:\s*100%;[^}]*overflow:\s*hidden;/s);
    expect(css).toMatch(/\.cyber-report-narrative\s*\{[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/s);
    expect(css).toMatch(/\.cyber-report-markdown\s*\{[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/s);
  });

  test('contains wide Markdown tables in a dedicated horizontal scroller', () => {
    expect(css).toMatch(/\.cyber-table-scroll\s*\{[^}]*max-width:\s*100%;[^}]*overflow-x:\s*auto;/s);
  });

  test('provides a bounded desktop report layout and narrow-screen section strip', () => {
    expect(css).toMatch(/\.cyber-report-layout\s*\{[^}]*grid-template-columns:\s*minmax\(180px, 240px\) minmax\(0, 1fr\);/s);
    expect(css).toMatch(/\.cyber-report-sections\s*\{[^}]*position:\s*sticky;/s);
    expect(css).toMatch(/@media \(max-width: 767px\)\s*\{[\s\S]*?\.cyber-report-layout\s*\{[^}]*grid-template-columns:\s*1fr;/s);
    expect(css).toMatch(/@media \(max-width: 767px\)\s*\{[\s\S]*?\.cyber-report-sections ul\s*\{[^}]*overflow-x:\s*auto;/s);
  });
});
