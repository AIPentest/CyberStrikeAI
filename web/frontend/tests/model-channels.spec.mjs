import { test, expect, ready, UI_PERMISSIONS } from './fixtures.mjs';

function modelConfig() {
    return {
        ai: {
            default_channel: 'primary',
            channels: {
                primary: { provider: 'openai', model: 'primary-model', base_url: 'https://primary.example.test', api_key: '********' },
                'qwen-max': { name: 'deepseek', provider: 'openai_compatible', model: 'deepseek-flash', base_url: 'https://secondary.example.test', api_key: '********' }
            }
        },
        tools: []
    };
}

test.describe('model channel editing', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'config:write'] });

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
        await expect(save).toBeDisabled();
        await channel.click();
        await page.getByRole('option', { name: /\bdeepseek$/ }).click();
        await expect(channel).toHaveText('deepseek');
        await expect(model).toHaveValue('deepseek-flash');
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
        await expect(save).toBeDisabled();
        expect(mockApi.calls('PUT', '/api/config')[0].body).toEqual({
            ai_channel: { channel_id: 'qwen-max', make_default: false, model: 'manual-model', context_window: 65536 }
        });
        expect(config.ai.default_channel).toBe('primary');
        expect(config.ai.channels.primary.model).toBe('primary-model');

        await panel.getByRole('button', { name: '设为默认', exact: true }).click();
        await expect(channel).toHaveText('deepseek（默认）');
        expect(mockApi.calls('POST', '/api/config/apply')).toHaveLength(2);
        await page.reload();
        await expect(model).toHaveValue('manual-model');
        await expect(panel.getByLabel('模型上下文窗口（Token）', { exact: true })).toHaveValue('65536');
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
