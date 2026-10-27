import { defineConfig } from '@playwright/test';

// Full application routing with test-owned API responses and no backend setup.
export default defineConfig({
  testDir: './tests',
  testMatch: ['system-claude-code.spec.ts'],
  fullyParallel: false,
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: 60000,
  use: {
    baseURL: 'http://127.0.0.1:9532',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: 'pnpm exec vite --host 127.0.0.1 --port 9532 --strictPort',
    url: 'http://127.0.0.1:9532',
    reuseExistingServer: false,
    env: { VITE_API_URL: 'http://127.0.0.1:1' },
  },
});
