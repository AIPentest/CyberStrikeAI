import { defineConfig } from '@playwright/test';
import { fileURLToPath } from 'node:url';

const frontend = fileURLToPath(new URL('.', import.meta.url));

export default defineConfig({
    testDir: fileURLToPath(new URL('./tests', import.meta.url)),
    testMatch: '*.spec.mjs',
    outputDir: fileURLToPath(new URL('./test-results', import.meta.url)),
    fullyParallel: false,
    workers: 1,
    retries: 0,
    forbidOnly: Boolean(process.env.CI),
    timeout: 45_000,
    expect: { timeout: 10_000 },
    reporter: [
        ['list'],
        ['html', { outputFolder: fileURLToPath(new URL('./playwright-report', import.meta.url)), open: 'never' }]
    ],
    use: {
        baseURL: 'http://127.0.0.1:5199/static/app/',
        browserName: 'chromium',
        channel: process.env.CI ? undefined : 'chrome',
        headless: true,
        viewport: { width: 1440, height: 1000 },
        locale: 'zh-CN',
        timezoneId: 'Asia/Shanghai',
        colorScheme: 'light',
        serviceWorkers: 'block',
        actionTimeout: 10_000,
        navigationTimeout: 30_000,
        screenshot: { mode: 'on', fullPage: true },
        trace: 'retain-on-failure'
    },
    webServer: {
        command: 'npm run dev -- --host 127.0.0.1 --port 5199 --strictPort',
        cwd: frontend,
        url: 'http://127.0.0.1:5199/static/app/',
        reuseExistingServer: false,
        timeout: 60_000,
        // Extra protection for a server started here; browser routing also guards reused servers.
        env: { CSAI_API_ORIGIN: 'http://127.0.0.1:9' }
    }
});
