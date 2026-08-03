import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, test } from 'vitest';

const tokenPath = process.cwd().endsWith('/packages/ui')
  ? resolve(process.cwd(), 'src/tokens.css')
  : resolve(process.cwd(), 'packages/ui/src/tokens.css');
const css = readFileSync(tokenPath, 'utf8');

describe('Forensic Noir tokens', () => {
  test('defines surface, text, semantic state, severity, confidence, and focus tokens', () => {
    for (const token of [
      '--cyber-bg', '--cyber-surface', '--cyber-text', '--cyber-muted', '--cyber-focus',
      '--cyber-critical', '--cyber-high', '--cyber-medium', '--cyber-low',
      '--cyber-success', '--cyber-warning', '--cyber-danger', '--cyber-info',
      '--cyber-space-1', '--cyber-space-6',
    ]) expect(css).toContain(token);
    expect(css).toContain('.cyber-confidence-dots');
    expect(css).toContain('overflow-wrap: anywhere');
  });

  test('defines desktop and compact breakpoints plus reduced motion', () => {
    expect(css).toContain('@media (min-width: 1280px)');
    expect(css).toContain('@media (max-width: 1279px)');
    expect(css).toContain('@media (max-width: 767px)');
    expect(css).toContain('@media (prefers-reduced-motion: reduce)');
    expect(css).toMatch(/animation-duration:\s*0\.01ms/);
    expect(css).toMatch(/transition-duration:\s*0\.01ms/);
  });

  test('uses high contrast text and focus colors on the base background', () => {
    const luminance = (hex: string) => {
      const channels = hex.match(/[\da-f]{2}/gi)?.map((value) => Number.parseInt(value, 16) / 255) ?? [];
      const [red, green, blue] = channels.map((value) => value <= 0.03928
        ? value / 12.92
        : ((value + 0.055) / 1.055) ** 2.4);
      return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
    };
    const contrast = (foreground: string, background: string) => {
      const [lighter, darker] = [luminance(foreground), luminance(background)].sort((a, b) => b - a);
      return (lighter + 0.05) / (darker + 0.05);
    };

    expect(contrast('#f2f5f7', '#080b0f')).toBeGreaterThanOrEqual(7);
    expect(contrast('#69d7c6', '#080b0f')).toBeGreaterThanOrEqual(4.5);
  });
});
