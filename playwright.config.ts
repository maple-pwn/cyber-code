import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './apps/web/e2e',
  testMatch: '**/*.spec.ts',
  webServer: {
    command: 'npm exec --yes --package=pnpm@10.15.1 -- pnpm --filter @cyber/web dev -- --host 127.0.0.1 --port 4173',
    port: 4173,
    reuseExistingServer: !process.env.CI,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
