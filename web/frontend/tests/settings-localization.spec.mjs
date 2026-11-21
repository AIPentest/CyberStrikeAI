import { test, expect, ready, UI_PERMISSIONS } from './fixtures.mjs';

const currentRepo = 'https://example.test/team/current.git';
const newRepo = 'https://example.test/team/mirror.git';
const defaultRepo = 'https://example.test/default/project.git';

function mockUpdateStatus(mockApi) {
    mockApi.respond('GET', '/api/system/update', { json: {
        status: { installed: true, buildPending: false, localChanges: [], blockingChanges: [] },
        source: { repo: currentRepo, configured: true, defaultRepo }
    } });
}

test.describe('model probe localization', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'config:write'] });

    test('Chinese field names and probe diagnostics retain their meaning when the language changes', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/config', { json: {
            ai: { default_channel: 'fixture-channel', channels: {
                'fixture-channel': {
                    provider: 'openai_compatible', base_url: 'https://example.test/models',
                    model: 'fixture-model', api_key: '********', max_total_tokens: 100000
                }
            } },
            vision: { model: 'fixture-vision' }, tools: []
        } });
        mockApi.respond('POST', '/api/config/test-openai', { json: {
            success: true, model: 'fixture-model', latency_ms: 961,
            context_backend: 'openai_compatible', context_warnings: ['context_window_unknown']
        } });
        mockApi.respond('POST', '/api/config/test-vision', { json: {
            success: true, model: 'fixture-vision', latency_ms: 994
        } });
        await page.goto('/static/app/#/settings?section=models');
        const main = await ready(page);
        const probe = main.locator('.cs-section-card').filter({ has: page.locator('.settings-probe-form') });
        await expect(probe.getByLabel('API 地址', { exact: true }).first()).toHaveValue('https://example.test/models');
        await expect(probe.getByText('最大输出令牌数', { exact: true })).toBeVisible();
        await probe.getByRole('button', { name: '测试连接', exact: true }).click();
        await probe.getByRole('button', { name: '测试视觉模型', exact: true }).click();
        await expect(probe.getByRole('cell', { name: /响应耗时.*961 毫秒/ })).toBeVisible();
        await expect(probe.getByText('连接成功', { exact: true })).toHaveCount(2);
        await expect(probe.getByText('961 毫秒', { exact: true })).toBeVisible();
        await expect(probe.getByText('994 毫秒', { exact: true })).toBeVisible();
        await expect(probe.getByText('未检测到上下文窗口，请手动填写', { exact: true })).toBeVisible();
        await expect(probe.getByText(/context_warnings|context_window_unknown|latency_ms/)).toHaveCount(0);
        expect(mockApi.calls('POST', '/api/config/test-openai')[0].body).toMatchObject({
            provider: 'openai_compatible', base_url: 'https://example.test/models',
            model: 'fixture-model', api_key: '********', max_total_tokens: 100000
        });

        await page.getByRole('button', { name: 'zh-CN', exact: true }).click();
        await page.getByText('English', { exact: true }).click();
        await expect(probe.getByRole('cell', { name: /Response latency.*961 ms/ })).toBeVisible();
        await expect(probe.getByText('961 ms', { exact: true })).toBeVisible();
        await expect(probe.getByText('Context notices', { exact: true })).toBeVisible();
    });
});

test.describe('update source management', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'update:apply'] });

    test('save errors keep the draft and clearing the address restores the default without running an update', async ({ page, mockApi }) => {
        mockUpdateStatus(mockApi);
        await page.goto('/static/app/#/settings?section=update');
        const main = await ready(page);
        const address = main.getByRole('textbox', { name: '更新源仓库地址' });
        await expect(address).toHaveValue(currentRepo);
        await expect(main.getByText('待编译', { exact: false }).first()).toBeVisible();
        const save = main.getByRole('button', { name: '保存更新源', exact: true });
        await expect(save).toBeDisabled();

        await address.fill(' ' + newRepo + ' ');
        mockApi.fail('POST', '/api/system/update/source', '示例地址暂时无法保存');
        await save.click();
        await expect(main.getByText('示例地址暂时无法保存', { exact: true })).toBeVisible();
        await expect(address).toHaveValue(' ' + newRepo + ' ');

        mockApi.respond('POST', '/api/system/update/source', { json: {
            source: { repo: newRepo, configured: true, defaultRepo }
        } });
        await save.click();
        await expect(address).toHaveValue(newRepo);
        await expect(main.getByText(newRepo, { exact: true })).toBeVisible();

        await address.clear();
        mockApi.respond('POST', '/api/system/update/source', { json: {
            source: { repo: '', configured: false, defaultRepo }
        } });
        await save.click();
        await expect(main.getByText(defaultRepo, { exact: true })).toBeVisible();
        await expect(save).toBeDisabled();
        expect(mockApi.writes().map(({ path, body }) => [path, body])).toEqual([
            ['/api/system/update/source', { repo: newRepo }],
            ['/api/system/update/source', { repo: newRepo }],
            ['/api/system/update/source', { repo: '' }]
        ]);
    });
});

test('update viewers can read the source and see the server configuration location', async ({ page, mockApi }) => {
    mockUpdateStatus(mockApi);
    await page.goto('/static/app/#/settings?section=update');
    const main = await ready(page);
    const address = main.getByRole('textbox', { name: '更新源仓库地址' });
    await expect(address).toHaveValue(currentRepo);
    await expect(address).toHaveAttribute('readonly', '');
    await expect(main.getByRole('button', { name: '保存更新源', exact: true })).toHaveCount(0);
    await expect(main.getByText(/配置文件中修改 update\.repo/)).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
});
