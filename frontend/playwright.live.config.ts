import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.live.spec.ts',
  outputDir: '../.cache/live-test-results',
  timeout: 180_000,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: process.env.PILOT_BASE_URL || 'http://127.0.0.1:5175',
    launchOptions: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } : {},
    trace: 'off', // One-time access links and passwords must not be recorded in traces.
  },
  projects: [
    { name: 'pilot-desktop', use: { viewport: { width: 1440, height: 1000 } } },
    { name: 'pilot-mobile', use: { viewport: { width: 360, height: 800 } } },
  ],
});
