import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/interactions',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 30000,
  use: { baseURL: 'http://127.0.0.1:9531', trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  webServer: {
    command: 'pnpm exec vite --config vite.interactions.config.ts --host 127.0.0.1 --port 9531 --strictPort',
    url: 'http://127.0.0.1:9531/tests/interactions/fixture.html',
    reuseExistingServer: false,
  },
});
