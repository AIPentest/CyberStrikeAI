import { test, expect, visit, label, UI_PERMISSIONS } from './fixtures.mjs';

test.use({ appPermissions: [...UI_PERMISSIONS, 'config:write'] });

test('model channel tests native image input while audit settings start collapsed', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/config', { json: {
        ai: { default_channel: 'deepseek', channels: {
            deepseek: { name: 'DeepSeek', model: 'deepseek-flash', base_url: 'https://api.deepseek.com', api_key: '********' }
        } },
        image_input_defaults: { deepseek: true },
        hitl: { audit_model: {} },
        tools: []
    } });
    mockApi.respond('POST', '/api/config/test-openai', { json: {
        success: true, model: 'deepseek-flash', latency_ms: 120
    } });
    const main = await visit(page, 'settings?section=models');
    const testImage = main.getByRole('button', { name: label('settingsProbe.testImage'), exact: true });
    const testAudit = main.getByRole('button', { name: label('settingsProbe.testAuditModel'), exact: true });
    await expect(main.getByText(label('settingsProbe.imageInputSupported'), { exact: true })).toBeVisible();
    await expect(testAudit).toBeHidden();
    await testImage.click();
    await expect(main.getByText(label('settingsProbe.imageReadSuccess'), { exact: true })).toBeVisible();
    const imageCalls = mockApi.calls('POST', '/api/config/test-openai');
    expect(imageCalls).toHaveLength(1);
    expect(imageCalls[0].body).toMatchObject({ channel_id: 'deepseek', model: 'deepseek-flash', test_image: true });
    await main.getByText(label('settingsProbe.auditModelTitle'), { exact: true }).click();
    await expect(testAudit).toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(testAudit).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(391);
});
