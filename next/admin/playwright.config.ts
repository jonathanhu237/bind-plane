import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  // Setup links are single-use; retrying after a partial initialization would
  // exercise a consumed link rather than a fresh first-run installation.
  retries: process.env.E2E_FIRST_RUN === '1' ? 0 : (process.env.CI ? 2 : 0),
  reporter: process.env.CI ? 'line' : 'list',
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? 'http://127.0.0.1:5173',
    launchOptions: process.env.PLAYWRIGHT_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
      : undefined,
    // The required first-run gate fills a setup password and must not leave
    // screenshots or traces containing credentials in CI artifacts.
    trace: process.env.E2E_FIRST_RUN === '1' ? 'off' : 'retain-on-failure',
    screenshot: process.env.E2E_FIRST_RUN === '1' ? 'off' : 'only-on-failure',
    video: 'off',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
