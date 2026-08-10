/// <reference types="node" />

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, test } from 'vitest';

const stylePath = process.cwd().endsWith('/packages/product-app')
  ? resolve(process.cwd(), 'src/styles.css')
  : resolve(process.cwd(), 'packages/product-app/src/styles.css');
const css = readFileSync(stylePath, 'utf8');

describe('new task responsive layout', () => {
  test('uses the available workspace width on desktop', () => {
    expect(css).toMatch(/\.page-new-task form\s*\{[^}]*width:\s*100%;/s);
    expect(css).toMatch(/\.page-new-task fieldset\s*\{[^}]*grid-template-columns:\s*repeat\(3, minmax\(0, 1fr\)\);/s);
  });

  test('adapts runtime choices to laptop and phone widths', () => {
    expect(css).toMatch(/@media \(min-width: 768px\) and \(max-width: 1279px\)\s*\{[\s\S]*?\.page-new-task fieldset\s*\{\s*grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);\s*\}/);
    expect(css).toMatch(/@media \(max-width: 767px\)\s*\{[\s\S]*?\.page-new-task fieldset\s*\{\s*grid-template-columns:\s*1fr;\s*\}/);
  });
});

describe('report page containment', () => {
  test('keeps report grid items within the workspace width', () => {
    expect(css).toMatch(/\.page-reports\s*\{[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/s);
    expect(css).toMatch(/\.desktop-report-editor\s*\{[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/s);
  });

  test('keeps the asset graph visible on narrow screens for internal scrolling', () => {
    expect(css).toMatch(/@media \(max-width: 767px\)\s*\{[\s\S]*?\.asset-graph-canvas\s*\{\s*display:\s*block;/s);
  });
});
