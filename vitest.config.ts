import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'jsdom',
    setupFiles: ['./apps/web/src/test-setup.ts'],
    exclude: ['**/node_modules/**', '**/dist/**', 'editors/**', 'apps/web/tests/**'],
    coverage: {
      provider: 'v8',
      thresholds: {
        statements: 85,
        lines: 85,
        functions: 85,
        branches: 80,
      },
    },
  },
});
