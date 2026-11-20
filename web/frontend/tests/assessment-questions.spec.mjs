import { test, expect, ready, visit, label, ASSESSMENT, PROJECT, UI_PERMISSIONS } from './fixtures.mjs';

test('assessment questions keep follow-ups across tabs and pause only the selected intent', async ({ page, mockApi }) => {
    const base = `/api/deep-assessments/${ASSESSMENT.id}`;
    const step = { id: 'record', title: '核对记录', status: 'running' };
    const task = { ...ASSESSMENT, status: 'running', revision: 10, steps: [step] };
    mockApi.respond('GET', base, { json: { task } });
    mockApi.respond('GET', `${base}/events`, { json: { events: [] } });
    mockApi.respond('GET', `${base}/intents`, { json: { intents: [step], total: 1 } });
    mockApi.respond('POST', `${base}/question`, { json: { answer: '记录正在核对中。', updated_at: task.updated_at } });
    mockApi.respond('POST', `${base}/control/intent-pause`, { json: {} });
    await page.goto(`/static/app/#/deep-assessments?id=${task.id}`);
    const main = await ready(page);
    await main.getByRole('button', { name: '测评问答' }).click();
    const drawer = page.getByRole('dialog', { name: '测评问答' });
    const ask = async question => {
        await drawer.getByRole('textbox').fill(question);
        await drawer.getByRole('button', { name: '发送' }).click();
    };
    await ask('当前进度？');
    await expect(drawer.getByRole('log')).toContainText('记录正在核对中。');
    await page.keyboard.press('Escape');
    await main.getByRole('tab', { name: '意图', exact: true }).click();
    await main.getByRole('button', { name: '测评问答' }).click();
    await expect(drawer.getByRole('log')).toContainText('当前进度？');
    await ask('这一步结束了吗？');
    await expect(drawer.getByRole('log')).toContainText('这一步结束了吗？');
    const questions = mockApi.calls('POST', `${base}/question`);
    expect(questions[1].body.history).toEqual([{ question: '当前进度？', answer: '记录正在核对中。' }]);
    await page.keyboard.press('Escape');
    await main.getByText(step.title, { exact: true }).click();
    await page.getByRole('dialog').getByRole('button', { name: '暂停此方向' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(mockApi.writes().map(call => call.path)).toEqual([`${base}/question`, `${base}/question`, `${base}/control/intent-pause`]);
    expect(mockApi.writes()[2].body).toEqual({ step_id: step.id, revision: 10 });
    expect(mockApi.unexpected).toEqual([]);
});

test.describe('project asset entry', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'asset:write'] });
    test('registers a manual asset in the open project without leaving the page', async ({ page, mockApi }) => {
        mockApi.respond('POST', '/api/assets/import', { json: { created: 1 } });
        const main = await visit(page, 'projects');
        const url = page.url();
        await main.getByRole('button', { name: label('assets.addAsset'), exact: true }).click();
        const drawer = page.getByRole('dialog', { name: label('assets.addAssetTitle') });
        await expect(drawer.getByRole('combobox')).toHaveText(PROJECT.name);
        await drawer.getByRole('textbox').fill('inventory.example.test');
        await drawer.getByRole('button', { name: label('assets.addAssetAction') }).click();
        await expect(drawer).toHaveCount(0);
        expect(page.url()).toBe(url);
        expect(mockApi.writes()).toHaveLength(1);
        expect(mockApi.writes()[0].body.assets).toEqual([expect.objectContaining({ project_id: PROJECT.id, domain: 'inventory.example.test' })]);
        expect(mockApi.unexpected).toEqual([]);
    });
});

test('mobile assessment questions preserve a failed draft and finish while the drawer is closed', async ({ page, mockApi }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const base = `/api/deep-assessments/${ASSESSMENT.id}`;
    mockApi.respond('GET', `${base}/events`, { json: { events: [] } });
    mockApi.respond('POST', `${base}/question`, { status: 502, json: { error: '模型暂不可用' } });
    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}`);
    const main = await ready(page);
    const open = main.getByRole('button', { name: '测评问答' });
    const drawer = page.getByRole('dialog', { name: '测评问答' });
    const question = '现在到哪一步了？';
    await open.click();
    await drawer.getByRole('textbox').fill(question);
    await drawer.getByRole('button', { name: '发送' }).click();
    await expect(drawer.getByText('模型暂不可用', { exact: true })).toBeVisible();
    await expect(drawer.getByRole('textbox')).toHaveValue(question);
    let finish;
    const pending = new Promise(resolve => { finish = resolve; });
    mockApi.respond('POST', `${base}/question`, async () => {
        await pending;
        return { json: { answer: '任务处于草稿状态。', updated_at: ASSESSMENT.updated_at } };
    });
    try {
        await drawer.getByRole('button', { name: '发送' }).click();
        await expect(drawer.getByText('正在读取进展并生成回答…')).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(drawer).toHaveCount(0);
    } finally {
        finish();
    }
    await open.click();
    await expect(drawer.getByRole('log')).toContainText('任务处于草稿状态。');
    expect(mockApi.writes().map(call => call.path)).toEqual([`${base}/question`, `${base}/question`]);
});
