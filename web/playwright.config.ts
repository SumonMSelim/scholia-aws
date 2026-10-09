import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.e2e.ts',
  timeout: 90_000,
  use: {
    baseURL: process.env.SCHOLIA_BASE_URL ?? 'http://127.0.0.1:5287',
    ...devices['Desktop Chrome'],
  },
})
