// Faz 14 #148 — Playwright config for Personel console + portal smoke suite.
//
// Targets the pilot stack on vm3 (192.168.5.44). Override via env:
//   PERSONEL_CONSOLE_URL, PERSONEL_PORTAL_URL, PERSONEL_KEYCLOAK_URL
//
// CI matrix should set these to localhost with a pre-warmed compose stack.
import { defineConfig, devices } from '@playwright/test';

const consoleURL = process.env.PERSONEL_CONSOLE_URL ?? 'http://192.168.5.44:3000';
const portalURL = process.env.PERSONEL_PORTAL_URL ?? 'http://192.168.5.44:3001';

export default defineConfig({
  testDir: './specs',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: [
    ['html', { outputFolder: '../../reports/playwright-report', open: 'never' }],
    ['json', { outputFile: '../../reports/playwright-results.json' }],
    ['list'],
  ],
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    actionTimeout: 15_000,
    navigationTimeout: 20_000,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    locale: 'tr-TR',
    timezoneId: 'Europe/Istanbul',
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: 'console-chromium',
      testMatch: /console-.*\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: consoleURL,
      },
    },
    {
      name: 'console-firefox',
      testMatch: /console-.*\.spec\.ts/,
      use: {
        ...devices['Desktop Firefox'],
        baseURL: consoleURL,
      },
    },
    {
      name: 'portal-chromium',
      testMatch: /portal-.*\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: portalURL,
      },
    },
    {
      name: 'employee-detail',
      testMatch: /employee-detail\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: consoleURL,
      },
    },
    {
      name: 'audit-search',
      testMatch: /audit-search\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: consoleURL,
      },
    },
    {
      name: 'dsr-dry-run',
      testMatch: /dsr-dry-run\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: consoleURL,
      },
    },
  ],
});
