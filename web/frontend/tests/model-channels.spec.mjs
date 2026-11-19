import { test, expect, ready, UI_PERMISSIONS } from './fixtures.mjs';

function modelConfig() {
    return {
        ai: {
            default_channel: 'primary',
            channels: {
                primary: { provider: 'openai', model: 'primary-model', base_url: 'https://primary.example.test', api_key: '********' },
                'qwen-max': { provider: 'openai_compatible', model: 'deepseek-flash', base_url: 'https://secondary.example.test', api_key: '********' }
            }
        },
        tools: []
    };
}

test.describe('model channel editing', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'config:write'] });

    test('uses the server default until a manual context window is saved', async ({ page, mockApi }) => {
        const config = modelConfig();
        config.ai.default_channel = 'qwen-max';
        config.context_window_defaults = { 'qwen-max': 1048576 };
        mockApi.respond('GET', '/api/config', () => ({ json: config }));
        mockApi.respond('POST', '/api/config/test-openai', { json: {
            success: true, model: 'deepseek-flash', context_warnings: ['context_window_unknown']
        } });
        mockApi.respond('PUT', '/api/config', ({ body }) => {
            config.ai.channels['qwen-max'].context_window = body.ai_channel.context_window;
            return { json: {} };
        });
        mockApi.respond('POST', '/api/config/apply', { json: {} });
        await page.goto('/static/app/#/settings?section=models');
        const panel = (await ready(page)).locator('.settings-channel-panel');
        const window = panel.getByLabel('模型上下文窗口（Token）', { exact: true });
        const save = panel.getByRole('button', { name: '保存通道', exact: true });
        await expect(window).toHaveValue('1048576');
        await expect(panel.locator('.settings-probe-form').getByText('默认值', { exact: true })).toHaveCount(0);
        await expect(save).toBeDisabled();
        await panel.getByRole('button', { name: '测试连接', exact: true }).click();
        await expect(panel.getByRole('cell', { name: /上下文窗口.*1048576/ })).toBeVisible();
        await expect(panel.getByText('未检测到上下文窗口，请手动填写', { exact: true })).toHaveCount(0);
        await expect(save).toBeDisabled();

        await window.fill('65536');
        await save.click();
        await expect(save).toBeDisabled();
        await expect(window).toHaveValue('65536');
        await window.clear();
        await save.click();
        await expect(window).toHaveValue('1048576');
        expect(mockApi.calls('PUT', '/api/config').map(({ body }) => body.ai_channel.context_window)).toEqual([65536, 0]);
    });

    test('shows an example, fills a detected context window, and saves it explicitly', async ({ page, mockApi }) => {
        const config = modelConfig();
        const model = config.ai.channels['qwen-max'];
        config.ai.default_channel = 'qwen-max';
        mockApi.respond('GET', '/api/config', () => ({ json: config }));
        const result = {
            success: true, model: 'deepseek-flash', context_window: 1048576,
            context_source: 'openai_models_context_window'
        };
        mockApi.respond('POST', '/api/config/test-openai', { json: result });
        mockApi.respond('POST', '/api/config/detect-context', { json: { ...result, context_runtime_unknown: true } });
        mockApi.respond('PUT', '/api/config', ({ body }) => {
            model.context_window = body.ai_channel.context_window;
            return { json: {} };
        });
        mockApi.respond('POST', '/api/config/apply', { json: {} });
        await page.goto('/static/app/#/settings?section=models');
        const panel = (await ready(page)).locator('.settings-channel-panel');
        const window = panel.getByLabel('模型上下文窗口（Token）', { exact: true });
        const detect = panel.getByRole('button', { name: '自动检测', exact: true });
        const connection = panel.getByRole('button', { name: '测试连接', exact: true });
        const save = panel.getByRole('button', { name: '保存通道', exact: true });
        await expect(window).toHaveValue('');
        await expect(window).toHaveAttribute('placeholder', '例如 131072');
        await expect(save).toBeDisabled();

        await connection.click();
        await expect(window).toHaveValue('1048576');
        await expect(panel.getByText('模型接口返回的上下文窗口', { exact: true })).toBeVisible();
        await window.fill('65536');
        await connection.click();
        await expect(connection).toBeEnabled();
        await expect(window).toHaveValue('65536');
        await detect.click();
        await expect(detect).toBeEnabled();
        await expect(window).toHaveValue('65536');

        mockApi.respond('POST', '/api/config/detect-context', { json: result });
        await detect.click();
        await expect(window).toHaveValue('1048576');
        expect(mockApi.calls('PUT', '/api/config')).toEqual([]);
        await save.click();
        await expect(save).toBeDisabled();
        expect(mockApi.calls('PUT', '/api/config')[0].body).toMatchObject({
            ai_channel: { channel_id: 'qwen-max', context_window: 1048576 }
        });
        await page.reload();
        await expect(window).toHaveValue('1048576');
    });

    test('manual edits are saved on the selected channel and the default changes explicitly', async ({ page, mockApi }) => {
        const config = modelConfig();
        mockApi.respond('GET', '/api/config', () => ({ json: config }));
        mockApi.respond('PUT', '/api/config', ({ body }) => {
            const { channel_id, make_default, ...changes } = body.ai_channel;
            Object.assign(config.ai.channels[channel_id], changes);
            if (make_default) config.ai.default_channel = channel_id;
            return { json: {} };
        });
        mockApi.respond('POST', '/api/config/apply', { json: {} });
        mockApi.respond('POST', '/api/config/test-openai', { json: { success: true, model: 'manual-model' } });
        await page.goto('/static/app/#/settings?section=models');
        const panel = (await ready(page)).locator('.settings-channel-panel');
        const model = panel.locator('.settings-probe-form').getByLabel('模型', { exact: true });
        const channel = panel.getByRole('combobox', { name: '通道', exact: true });
        const save = panel.getByRole('button', { name: '保存通道', exact: true });
        await expect(model).toHaveValue('primary-model');
        await expect(channel).toHaveText('primary');
        await expect(save).toBeDisabled();
        await channel.click();
        await page.getByRole('option', { name: /\bqwen-max$/ }).click();
        await expect(channel).toHaveText('qwen-max');
        await expect(model).toHaveValue('deepseek-flash');
        await panel.getByLabel('通道名称', { exact: true }).fill('deepseek');
        await model.fill('manual-model');
        await panel.getByLabel('模型上下文窗口（Token）', { exact: true }).fill('65536');

        await panel.getByRole('button', { name: '测试连接', exact: true }).click();
        await expect(panel.getByText('连接成功', { exact: true })).toBeVisible();
        expect(mockApi.calls('PUT', '/api/config')).toEqual([]);
        expect(mockApi.calls('POST', '/api/config/test-openai')[0].body).toMatchObject({
            channel_id: 'qwen-max', model: 'manual-model', api_key: '********'
        });

        await save.click();
        await expect(model).toHaveValue('manual-model');
        await expect(channel).toHaveText('deepseek');
        await expect(save).toBeDisabled();
        expect(mockApi.calls('PUT', '/api/config')[0].body).toEqual({
            ai_channel: { channel_id: 'qwen-max', make_default: false, name: 'deepseek', model: 'manual-model', context_window: 65536 }
        });
        expect(config.ai.default_channel).toBe('primary');
        expect(config.ai.channels.primary.model).toBe('primary-model');

        const setDefault = panel.getByRole('button', { name: '设为默认', exact: true });
        await setDefault.click();
        await expect(setDefault).toHaveCount(0);
        await expect(panel.locator('.settings-probe-form').getByText('默认', { exact: true })).toHaveCount(0);
        await expect(channel).toHaveText('deepseek');
        expect(mockApi.calls('POST', '/api/config/apply')).toHaveLength(2);
        await page.reload();
        await expect(model).toHaveValue('manual-model');
        await expect(channel).toHaveText('deepseek');
        await expect(panel.getByLabel('模型上下文窗口（Token）', { exact: true })).toHaveValue('65536');
        await page.getByRole('tab', { name: '基本设置' }).click();
        await expect(page.getByRole('cell', { name: /默认 AI 通道.*deepseek/ })).toBeVisible();
    });

    test('save errors retain the draft and apply errors allow retrying the saved change', async ({ page, mockApi }) => {
        const config = modelConfig();
        mockApi.respond('GET', '/api/config', () => ({ json: config }));
        mockApi.fail('PUT', '/api/config', '示例通道保存失败');
        await page.goto('/static/app/#/settings?section=models');
        const panel = (await ready(page)).locator('.settings-channel-panel');
        const model = panel.locator('.settings-probe-form').getByLabel('模型', { exact: true });
        const save = panel.getByRole('button', { name: '保存通道' });
        await model.fill('manual-model');
        await save.click();
        await expect(panel.getByText('示例通道保存失败', { exact: true })).toBeVisible();
        await expect(model).toHaveValue('manual-model');
        expect(mockApi.calls('POST', '/api/config/apply')).toEqual([]);

        mockApi.respond('PUT', '/api/config', () => {
            config.ai.channels.primary.model = 'manual-model';
            return { json: {} };
        });
        mockApi.fail('POST', '/api/config/apply', '示例配置应用失败');
        await save.click();
        const retry = panel.getByRole('button', { name: '重试应用' });
        await expect(retry).toBeEnabled();
        await expect(model).toHaveValue('manual-model');
        mockApi.respond('POST', '/api/config/apply', { json: {} });
        await retry.click();
        await expect(retry).toHaveCount(0);
        expect(mockApi.calls('PUT', '/api/config')).toHaveLength(2);
        expect(mockApi.calls('POST', '/api/config/apply')).toHaveLength(2);
    });
});

test('model configuration remains readable with view permission', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/config', { json: modelConfig() });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/static/app/#/settings?section=models');
    const panel = (await ready(page)).locator('.settings-channel-panel');
    const model = panel.locator('.settings-probe-form').getByLabel('模型', { exact: true });
    await expect(model).toHaveValue('primary-model');
    await expect(model).not.toBeEditable();
    await expect(panel.getByRole('button', { name: '保存通道' })).toHaveCount(0);
    expect(mockApi.writes()).toEqual([]);
});

test('model channels with the same name can be selected by their IDs in the menu', async ({ page, mockApi }) => {
    const config = modelConfig();
    config.ai.channels.primary.name = 'deepseek';
    config.ai.channels['qwen-max'].name = 'deepseek';
    mockApi.respond('GET', '/api/config', { json: config });
    await page.goto('/static/app/#/settings?section=models');
    const panel = (await ready(page)).locator('.settings-channel-panel');
    const channel = panel.getByRole('combobox', { name: '通道', exact: true });
    await channel.click();
    await expect(page.getByRole('option', { selected: true })).toHaveText('deepseek · ID primary');
    await page.getByRole('option', { name: /deepseek · ID qwen-max$/ }).click();
    await expect(channel).toHaveText('deepseek');
    await expect(panel.locator('.settings-probe-form').getByLabel('模型', { exact: true })).toHaveValue('deepseek-flash');
});
