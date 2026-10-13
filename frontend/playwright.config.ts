import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './tests',
  workers: 1,
  timeout: 60000,
  outputDir: '../private/platform-v2/playwright',
  reporter: [['list']],
  use: {
    headless: true,
    viewport: { width: 1440, height: 1060 },
    launchOptions: {
      executablePath:
        process.env.AF_TEST_BROWSER_PATH ||
        'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
    },
  },
})
