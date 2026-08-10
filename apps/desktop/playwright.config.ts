import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.spec.ts',
  webServer: {
    command: 'npm exec --yes --package=pnpm@10.15.1 -- pnpm --filter @cyber/desktop exec vite --host 127.0.0.1 --port 4174',
    port: 4174,
    reuseExistingServer: !process.env.CI,
  },
  use: { ...devices['Desktop Chrome'], baseURL: 'http://127.0.0.1:4174', viewport: { width: 1024, height: 768 } },
  projects: [
    { name: 'desktop' },
    { name: 'desktop-200-percent', use: { deviceScaleFactor: 2 } },
  ],
});
