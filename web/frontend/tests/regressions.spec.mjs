import { test, expect, visit, ready, label, UI_PERMISSIONS, PROJECT, CONVERSATION, ASSESSMENT, STAMP } from './fixtures.mjs';

test('token usage: summary values render with daily rows and update to zero after filtering', async ({ page, mockApi }, testInfo) => {
    const summary = {
        events: 2, modelCalls: 4, promptTokens: 800, completionTokens: 400,
        totalTokens: 1200, cachedTokens: 600, cacheWriteTokens: 100, reasoningTokens: 50
    };
    const empty = { events: 0, modelCalls: 0, promptTokens: 0, completionTokens: 0, totalTokens: 0, cachedTokens: 0, cacheWriteTokens: 0, reasoningTokens: 0 };
    mockApi.respond('GET', '/api/usage/tokens', (_, url) => ({ json: {
        summary: url.searchParams.get('days') === '7' ? summary : empty,
        today: empty,
        byDay: url.searchParams.get('days') === '7' ? [{ key: '2026-11-01', ...summary }] : [],
        byModel: [], byOrchestration: [], recent: []
    } }));
    const main = await visit(page, 'settings?section=token-usage');
    const grid = main.locator('.cs-stat-grid');
    const daily = main.locator('.cs-section-card').filter({ hasText: label('tokenUsageUI.trendTitle', '每日用量') });
    await expect(daily.getByRole('gridcell', { name: '2026-11-01', exact: true })).toBeVisible();
    await expect(grid.locator('.cs-stat-value')).toHaveText(['1,200', '800', '400', '600', '100', '50', '4', '75.0%']);
    await expect(grid.locator('.semi-skeleton-title')).toHaveCount(0);
    await page.screenshot({ path: testInfo.outputPath('token-usage-populated.png'), fullPage: true });

    await main.getByRole('combobox').filter({ hasText: '7 天' }).click();
    await page.getByRole('option', { name: /30 天$/ }).click();
    await expect(grid.locator('.cs-stat-value')).toHaveText(['0', '0', '0', '0', '0', '0', '0', '—']);
    await expect(grid.locator('.semi-skeleton-title')).toHaveCount(0);
    await expect(daily.getByText(label('tokenUsageUI.noDailyData', '暂无每日用量记录'), { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
});

test('token usage: explicit dashboard loading still resolves and links to visible summary values', async ({ page, mockApi }) => {
    let releaseUsage;
    const pendingUsage = new Promise(resolve => { releaseUsage = resolve; });
    mockApi.respond('GET', '/api/usage/tokens', async () => {
        await pendingUsage;
        return { json: { summary: { totalTokens: 1200 }, today: {}, byDay: [], byModel: [], byOrchestration: [], recent: [] } };
    });
    await page.goto('/static/app/#/dashboard');
    const metric = page.getByRole('button', { name: label('dashboard.tokenUsage', 'Token 用量（7 天）'), exact: true });
    await expect(metric.locator('.semi-skeleton-title')).toBeVisible();
    await expect(metric.locator('.cs-stat-value')).toHaveCount(0);
    releaseUsage();
    await expect(metric.locator('.cs-stat-value')).toHaveText('1,200');
    await expect(metric.locator('.semi-skeleton-title')).toHaveCount(0);
    await metric.click();
    await expect(page).toHaveURL(/#\/settings\?section=token-usage$/);
    const main = await ready(page);
    await expect(main.locator('.cs-stat-grid .cs-stat-value')).toHaveText(['1,200', '0', '0', '0', '0', '0', '0', '—']);
    expect(mockApi.writes()).toEqual([]);
});

test('legacy update bookmark opens the current settings tab', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/system/update', { json: { status: {} } });
    mockApi.respond('GET', '/api/storage/meta', { json: {} });
    mockApi.respond('GET', '/api/storage/status', { json: {} });
    // Storage and update share the current section shell.
    await page.goto('/static/app/#system-update');
    await expect(page).toHaveURL(/#\/settings\?section=update$/);
    await expect.poll(() => mockApi.calls('GET', '/api/system/update').length).toBeGreaterThan(0);
});

test('read notification does not add to unread count and its timestamp is shown', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/notifications/summary', { json: {
        unreadCount: 0, items: [{ id: 'read-fixture', type: 'fixture', title: '示例已读通知', ts: STAMP, read: true, count: 7 }]
    } });
    await visit(page, 'platform-rbac');
    await page.getByRole('button', { name: label('notifications.title', '通知'), exact: true }).click();
    const item = page.getByRole('button', { name: /示例已读通知/ });
    await expect(item.locator('.cs-notification-meta')).toContainText('2026');
    await expect(page.locator('.semi-badge-count')).toHaveCount(0);
    expect(mockApi.writes()).toEqual([]);
});

test('refreshing a conversation detects an active backend run and disables duplicate sending', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [{ conversationId: CONVERSATION.id }] } });
    await page.goto('/static/app/#/chat?conversation=' + CONVERSATION.id);
    const main = await ready(page);
    await page.reload();
    await ready(page);
    await expect(main.getByRole('button', { name: /停止$/ })).toBeDisabled();
    await expect(main.getByRole('button', { name: /发送$/ })).toHaveCount(0);
    expect(mockApi.writes()).toEqual([]);
});

function mockEmptyConversation(mockApi, projectId = '') {
    const conversation = { ...CONVERSATION, projectId, messages: [] };
    mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
    mockApi.respond('GET', `/api/conversations/${conversation.id}`, () => ({ json: conversation }));
    mockApi.respond('GET', '/api/conversations', () => ({ json: { conversations: [conversation], total: 1 } }));
    return conversation;
}

test.describe('chat composer projects', () => {
    test('binds and unbinds an existing empty conversation without losing its draft or sending during save', async ({ page, mockApi }) => {
        const conversation = mockEmptyConversation(mockApi);
        const path = `/api/conversations/${conversation.id}/project`;
        const noProject = label('chat.noProject', '不绑定项目');
        const draft = 'FAKE：切换项目时保留这份会话草稿。';
        let releaseSave;
        const pendingSave = new Promise(resolve => { releaseSave = resolve; });
        mockApi.respond('PUT', path, async request => {
            await pendingSave;
            conversation.projectId = request.body.projectId;
            return { json: { success: true, projectId: conversation.projectId } };
        });
        try {
            const main = await visit(page, `chat?conversation=${conversation.id}`);
            const context = main.locator('.chat-composer-context');
            const project = context.getByRole('combobox').filter({ hasText: new RegExp(`${noProject}|${PROJECT.name}`) });
            const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
            const send = main.getByRole('button', { name: /发送$/ });
            // The header may display the project name or its existing abbreviated ID.
            const headerProject = main.locator('.chat-header-tags .semi-tag').filter({ hasText: new RegExp(`${PROJECT.name}|${PROJECT.id.slice(0, 8)}`) });
            await expect(context.getByRole('combobox')).toHaveCount(2);
            await expect(project).toHaveText(noProject);
            await expect(headerProject).toHaveCount(0);
            await input.fill(draft);
            await expect(send).toBeEnabled();
            await expect.poll(() => mockApi.calls('GET', '/api/conversations').some(call => call.fulfilled)).toBe(true);
            const initialListReads = mockApi.calls('GET', '/api/conversations').length;

            await project.click();
            await page.getByRole('listbox').getByText(PROJECT.name, { exact: true }).click();
            await expect.poll(() => mockApi.calls('PUT', path).length).toBe(1);
            expect(mockApi.calls('PUT', path)[0].body).toEqual({ projectId: PROJECT.id });
            await expect(project).toBeDisabled();
            await expect(send).toBeDisabled();
            await input.press('Enter');
            await expect(input).toHaveValue(draft);
            expect(mockApi.writes().map(call => call.path)).toEqual([path]);

            releaseSave();
            await expect(project).toBeEnabled();
            await expect(project).toHaveText(PROJECT.name);
            await expect(headerProject).toBeVisible();
            await expect(main.getByRole('heading', { name: conversation.title, exact: true })).toBeVisible();
            await expect.poll(() => mockApi.calls('GET', '/api/conversations').length).toBeGreaterThan(initialListReads);
            await expect(input).toHaveValue(draft);
            const boundListReads = mockApi.calls('GET', '/api/conversations').length;

            await project.click();
            await page.getByRole('listbox').getByText(noProject, { exact: true }).click();
            await expect(project).toBeEnabled();
            await expect(project).toHaveText(noProject);
            await expect(headerProject).toHaveCount(0);
            await expect(send).toBeEnabled();
            await expect(input).toHaveValue(draft);
            await expect.poll(() => mockApi.calls('GET', '/api/conversations').length).toBeGreaterThan(boundListReads);
            expect(mockApi.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([
                { method: 'PUT', path, body: { projectId: PROJECT.id } },
                { method: 'PUT', path, body: { projectId: '' } }
            ]);
        } finally {
            releaseSave();
        }
    });

    test('keeps the server project and draft when an existing empty conversation cannot be unbound', async ({ page, mockApi }) => {
        const conversation = mockEmptyConversation(mockApi, PROJECT.id);
        const path = `/api/conversations/${conversation.id}/project`;
        const draft = 'FAKE：保存失败也不能丢失这份草稿。';
        mockApi.fail('PUT', path, 'fixture 项目保存失败');
        const main = await visit(page, `chat?conversation=${conversation.id}`);
        const context = main.locator('.chat-composer-context');
        const project = context.getByRole('combobox').filter({ hasText: PROJECT.name });
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        const headerProject = main.locator('.chat-header-tags .semi-tag').filter({ hasText: new RegExp(`${PROJECT.name}|${PROJECT.id.slice(0, 8)}`) });
        await expect(project).toHaveText(PROJECT.name);
        await expect(headerProject).toBeVisible();
        await input.fill(draft);
        await project.click();
        await page.getByRole('listbox').getByText(label('chat.noProject', '不绑定项目'), { exact: true }).click();
        await expect(page.getByText('fixture 项目保存失败', { exact: false })).toBeVisible();
        await expect(project).toBeEnabled();
        await expect(project).toHaveText(PROJECT.name);
        await expect(headerProject).toBeVisible();
        await expect(input).toHaveValue(draft);
        await expect(main.getByRole('button', { name: /发送$/ })).toBeEnabled();
        expect(mockApi.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([
            { method: 'PUT', path, body: { projectId: '' } }
        ]);
        expect(conversation.projectId).toBe(PROJECT.id);
    });

    test('selects a new conversation project locally and includes it only in the first send', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        const message = 'FAKE：整理这个示例项目的工作记录。';
        const id = mockApi.enableChatStream(message);
        const main = await visit(page, 'chat');
        const context = main.locator('.chat-composer-context');
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        await expect(context.getByRole('combobox')).toHaveCount(2);
        await input.fill(message);
        await context.getByRole('combobox').filter({ hasText: label('chat.agentModePiSingle') }).click();
        for (const key of ['chat.agentModePiSingle', 'chat.agentModeDeep', 'chat.agentModePlanExecuteLabel', 'chat.agentModeSupervisorLabel']) {
            await expect(page.getByRole('listbox').getByText(label(key), { exact: true })).toBeVisible();
        }
        await page.getByRole('listbox').getByText(label('chat.agentModePiSingle'), { exact: true }).click();
        await context.getByRole('combobox').filter({ hasText: label('chat.noProject', '不绑定项目') }).click();
        await page.getByRole('listbox').getByText(PROJECT.name, { exact: true }).click();
        await expect(context.getByRole('combobox').filter({ hasText: PROJECT.name })).toBeVisible();
        await expect(input).toHaveValue(message);
        await expect(page).toHaveURL(/#\/chat$/);
        expect(mockApi.writes()).toEqual([]);

        await input.press('Enter');
        await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
        expect(mockApi.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
            method: 'POST', path: '/api/agent/stream',
            body: { message, conversationId: '', projectId: PROJECT.id }
        }]);
    });
});

test.describe('chat session settings', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'hitl:write'] });

    test('restores legacy model preferences, saves changes, and sends them with a new message', async ({ page, mockApi }) => {
        await page.setViewportSize({ width: 390, height: 844 });
        mockApi.respond('GET', '/api/config', { json: { ai: {
            default_channel: 'demo-primary',
            channels: {
                'demo-primary': { name: '示例通道 A', model: 'fixture-a' },
                'demo-backup': { name: '示例通道 B', model: 'fixture-b' }
            }
        } } });
        mockApi.respond('GET', '/api/hitl/default-config', { json: {
            defaultMode: 'off', defaultTimeoutSeconds: 120, conversationHumanApproval: true
        } });
        const message = 'FAKE：请翻译这句示例文本。';
        const id = mockApi.enableChatStream(message);
        const main = await visit(page, 'chat');
        await page.evaluate(() => {
            localStorage.setItem('cyberstrike-chat-ai-channel', 'DEMO_BACKUP');
            localStorage.setItem('cyberstrike-chat-reasoning-mode', 'on');
            localStorage.setItem('cyberstrike-chat-reasoning-effort', 'low');
        });
        await page.reload();
        await ready(page);
        const openSettings = main.getByRole('button', { name: label('chat.sessionSettingsTitle'), exact: true });
        await openSettings.click();
        const settings = page.getByRole('dialog').filter({ hasText: label('chat.sessionSettingsTitle') });
        const channel = settings.getByRole('combobox', { name: label('chat.aiChannelLabel') });
        const mode = settings.getByRole('combobox', { name: label('chat.reasoningModeLabel') });
        const effort = settings.getByRole('combobox', { name: label('chat.reasoningEffortLabel') });
        await expect(channel).toHaveText('示例通道 B');
        await expect(effort).toContainText('low');
        await channel.click();
        await expect(page.getByRole('option', { name: `${label('chat.aiChannelDefault')} · 示例通道 A` })).toBeVisible();
        await page.getByRole('listbox').getByText('示例通道 A', { exact: true }).click();
        await mode.click();
        await page.getByRole('listbox').getByText(label('chat.reasoningModeOff'), { exact: true }).click();
        await expect(effort).toBeDisabled();
        await mode.click();
        await page.getByRole('listbox').getByText(label('chat.reasoningModeOn'), { exact: true }).click();
        await effort.click();
        await page.getByRole('listbox').getByText('medium', { exact: true }).click();
        await expect(effort).toContainText('medium');
        await expect.poll(async () => (await settings.boundingBox())?.width).toBeLessThanOrEqual(390);
        await page.reload();
        await ready(page);
        await openSettings.click();
        await expect(channel).toHaveText('示例通道 A');
        await expect(effort).toContainText('medium');
        const collaborationMode = settings.getByRole('combobox', { name: label('chat.hitlModeLabel') });
        await collaborationMode.click();
        await page.getByRole('listbox').getByText(label('chat.hitlModeApproval'), { exact: true }).click();
        await expect(collaborationMode).toHaveText(label('chat.hitlModeApproval'));
        await settings.locator('button.semi-sidesheet-close').click();
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        await input.fill(message);
        await input.press('Enter');
        await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
        expect(mockApi.writes().map(({ path, body }) => ({ path, body }))).toEqual([{
            path: '/api/agent/stream',
            body: {
                message, conversationId: '', aiChannelId: 'demo-primary',
                reasoning: { mode: 'on', effort: 'medium' },
                hitl: {
                    enabled: true, mode: 'approval', reviewer: 'human', timeoutSeconds: 120,
                    sensitiveTools: expect.arrayContaining(['tool_search', 'TaskCreate'])
                }
            }
        }]);
    });

    test('restores editable session HITL controls and persists every change', async ({ page, mockApi }) => {
        const path = `/api/hitl/config/${CONVERSATION.id}`;
        let hitl = {
            enabled: false, mode: 'off', reviewer: 'human',
            sensitiveTools: ['fixture-existing-tool'], timeoutSeconds: 90
        };
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        mockApi.respond('GET', path, () => ({ json: {
            hitl, defaultTimeoutSeconds: 90, hitlGlobalToolWhitelist: []
        } }));
        mockApi.respond('PUT', '/api/hitl/config', request => {
            const { conversationId, ...config } = request.body;
            hitl = config;
            return { json: { ok: true } };
        });
        await page.goto('/static/app/#/chat?conversation=' + CONVERSATION.id);
        const main = await ready(page);
        const openSettings = main.getByRole('button', { name: label('chat.sessionSettingsTitle'), exact: true });
        const settings = page.getByRole('dialog');
        await openSettings.click();
        const mode = settings.getByRole('combobox', { name: label('chat.hitlModeLabel') });
        const timeout = settings.getByRole('combobox', { name: label('chat.hitlTimeoutLabel') });
        const human = settings.getByRole('radio', { name: label('chat.hitlReviewerHuman') });
        const auditAgent = settings.getByRole('radio', { name: label('chat.hitlReviewerAgent') });
        const tools = settings.getByRole('textbox', { name: label('chat.hitlWhitelistTools') });
        await expect(mode).toHaveText(label('chat.hitlModeOff'));
        await expect(human).toBeChecked();
        await expect(timeout).toHaveText('90s');
        await expect(tools).toHaveValue('fixture-existing-tool');
        expect(mockApi.writes()).toEqual([]);

        const savedCount = async count => {
            await expect.poll(() => mockApi.calls('PUT', '/api/hitl/config').length).toBe(count);
        };
        await mode.click();
        await page.getByRole('listbox').getByText(label('chat.hitlModeApproval'), { exact: true }).click();
        await savedCount(1);
        await mode.click();
        await page.getByRole('listbox').getByText(label('chat.hitlModeReviewEdit'), { exact: true }).click();
        await savedCount(2);
        await settings.getByText(label('chat.hitlReviewerAgent'), { exact: true }).click();
        await savedCount(3);
        await expect(auditAgent).toBeChecked();
        await timeout.click();
        await page.getByRole('listbox').getByText(label('chat.hitlTimeoutOneMinute'), { exact: true }).click();
        await savedCount(4);
        await tools.fill('fixture-read, fixture-write');
        await tools.press('Tab');
        await savedCount(5);
        await mode.click();
        await page.getByRole('listbox').getByText(label('chat.hitlModeOff'), { exact: true }).click();
        await savedCount(6);

        const expected = {
            conversationId: CONVERSATION.id,
            reviewer: 'audit_agent',
            sensitiveTools: ['fixture-read', 'fixture-write'],
            timeoutSeconds: 60
        };
        expect(mockApi.calls('PUT', '/api/hitl/config').map(call => call.body)).toEqual([
            { conversationId: CONVERSATION.id, enabled: true, mode: 'approval', reviewer: 'human', sensitiveTools: ['fixture-existing-tool'], timeoutSeconds: 90 },
            { conversationId: CONVERSATION.id, enabled: true, mode: 'review_edit', reviewer: 'human', sensitiveTools: ['fixture-existing-tool'], timeoutSeconds: 90 },
            { conversationId: CONVERSATION.id, enabled: true, mode: 'review_edit', reviewer: 'audit_agent', sensitiveTools: ['fixture-existing-tool'], timeoutSeconds: 90 },
            { conversationId: CONVERSATION.id, enabled: true, mode: 'review_edit', reviewer: 'audit_agent', sensitiveTools: ['fixture-existing-tool'], timeoutSeconds: 60 },
            { ...expected, enabled: true, mode: 'review_edit' },
            { ...expected, enabled: false, mode: 'off' }
        ]);
        await expect(settings.getByRole('status')).toHaveText(label('chat.hitlApplyOkSync'));

        await settings.locator('button.semi-sidesheet-close').click();
        await openSettings.click();
        await expect(mode).toHaveText(label('chat.hitlModeOff'));
        await expect(auditAgent).toBeChecked();
        await expect(timeout).toHaveText(label('chat.hitlTimeoutOneMinute'));
        await expect(tools).toHaveValue('fixture-read, fixture-write');
        expect(mockApi.writes()).toHaveLength(6);
    });

    test('sets conversation-level human approval when the default reviewer is inherited', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/hitl/default-config', { json: {
            defaultMode: 'approval', defaultReviewer: 'audit_agent', defaultTimeoutSeconds: 180, conversationHumanApproval: true
        } });
        const message = 'FAKE：请润色这句示例文本。';
        const id = mockApi.enableChatStream(message);
        mockApi.respond('GET', `/api/hitl/config/${id}`, { json: {
            hitl: { enabled: true, mode: 'approval', reviewer: 'human', timeoutSeconds: 180 },
            defaultMode: 'approval', defaultReviewer: 'audit_agent', defaultTimeoutSeconds: 180, conversationHumanApproval: true
        } });
        const main = await visit(page, 'chat');
        const settingsButton = main.getByRole('button', { name: label('chat.sessionSettingsTitle'), exact: true });
        await settingsButton.click();
        const settings = page.getByRole('dialog');
        const auditAgent = settings.getByRole('radio', { name: label('chat.hitlReviewerAgent') });
        const human = settings.getByRole('radio', { name: label('chat.hitlReviewerHuman') });
        await expect(auditAgent).toBeChecked();
        await settings.getByText(label('chat.hitlReviewerHuman'), { exact: true }).click();
        await expect(human).toBeChecked();
        expect(mockApi.writes()).toEqual([]);

        await settings.locator('button.semi-sidesheet-close').click();
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        await input.fill(message);
        await input.press('Enter');
        await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
        expect(mockApi.calls('POST', '/api/agent/stream')[0]?.body.hitl).toMatchObject({
            enabled: true, mode: 'approval', reviewer: 'human', timeoutSeconds: 180
        });
        await page.reload();
        await ready(page);
        await settingsButton.click();
        await expect(human).toBeChecked();
        await expect(settings.getByRole('combobox', { name: label('chat.hitlModeLabel') })).toHaveText(label('chat.hitlModeApproval'));
    });

    test('restores the server value when a session HITL change fails', async ({ page, mockApi }) => {
        const path = `/api/hitl/config/${CONVERSATION.id}`;
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        mockApi.respond('GET', path, { json: {
            hitl: { enabled: false, mode: 'off', reviewer: 'human', sensitiveTools: [], timeoutSeconds: 300 },
            defaultTimeoutSeconds: 300
        } });
        mockApi.fail('PUT', '/api/hitl/config', 'fixture 保存失败');
        await page.goto('/static/app/#/chat?conversation=' + CONVERSATION.id);
        const main = await ready(page);
        await main.getByRole('button', { name: label('chat.sessionSettingsTitle'), exact: true }).click();
        const settings = page.getByRole('dialog');
        const mode = settings.getByRole('combobox', { name: label('chat.hitlModeLabel') });
        await expect(mode).toHaveText(label('chat.hitlModeOff'));
        await mode.click();
        await page.getByRole('listbox').getByText(label('chat.hitlModeApproval'), { exact: true }).click();
        await expect(page.getByText('fixture 保存失败', { exact: false })).toBeVisible();
        await expect(mode).toHaveText(label('chat.hitlModeOff'));
        await expect.poll(() => mockApi.calls('GET', path).length).toBeGreaterThanOrEqual(2);
        expect(mockApi.calls('PUT', '/api/hitl/config')).toHaveLength(1);
    });

    for (const reasoningMode of ['off', 'on']) {
        test(`composer model popover persists preferences and sends reasoning ${reasoningMode}`, async ({ page, mockApi }) => {
            mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
            mockApi.respond('GET', '/api/config', { json: { ai: {
                default_channel: 'demo-primary',
                channels: {
                    'demo-primary': { name: '示例通道 A', model: 'fixture-a' },
                    'demo-backup': { name: '示例通道 B', model: 'fixture-b' }
                }
            } } });
            const message = `FAKE：使用推理 ${reasoningMode} 整理示例文本。`;
            const id = mockApi.enableChatStream(message);
            const main = await visit(page, 'chat');
            const openModel = main.getByRole('button', { name: label('chat.modelSettingsAria'), exact: true });
            await expect(openModel).toHaveClass(/chat-composer-model/);
            await openModel.click();
            const popover = page.locator('.chat-model-popover');
            const channel = popover.getByRole('combobox', { name: label('chat.aiChannelLabel'), exact: true });
            const mode = popover.getByRole('button', { name: label('chat.reasoningModeLabel'), exact: true });
            const slider = popover.getByRole('slider', { name: label('chat.reasoningEffortLabel'), exact: true });
            const value = popover.locator('.chat-reasoning-value[role="status"]');
            const menu = page.locator('.chat-reasoning-mode-menu');
            await expect(popover).toBeVisible();
            await expect(popover.getByRole('combobox')).toHaveCount(1);
            await expect(channel).toHaveAttribute('aria-labelledby', /\S+/);
            await expect(popover.getByRole('combobox', { name: label('chat.hitlModeLabel'), exact: true })).toHaveCount(0);
            await channel.click();
            await page.getByRole('listbox').getByText('示例通道 B', { exact: true }).click();
            await mode.click();
            await menu.getByRole('menuitem', { name: label('chat.reasoningModeOn') }).click();
            await slider.press('Home');
            for (let step = 0; step < 3; step++) await slider.press('ArrowRight');
            await expect(slider).toHaveAttribute('aria-valuenow', '3');
            await expect(value).toHaveText(label('chat.reasoningEffortLabels.high', '高'));
            await mode.click();
            await menu.getByRole('menuitem', { name: label('chat.reasoningModeOff') }).click();
            await expect(slider).toHaveAttribute('aria-disabled', 'true');
            await expect(value).toHaveText(label('chat.reasoningModeOff'));
            if (reasoningMode === 'on') {
                await mode.click();
                await menu.getByRole('menuitem', { name: label('chat.reasoningModeOn') }).click();
                await expect(slider).toHaveAttribute('aria-disabled', 'false');
                await expect(slider).toHaveAttribute('aria-valuenow', '3');
                await expect(value).toHaveText(label('chat.reasoningEffortLabels.high', '高'));
            }
            await expect.poll(() => page.evaluate(() => ({
                channel: localStorage.getItem('cyberstrike-chat-ai-channel'),
                mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
                effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
            }))).toEqual({ channel: 'demo-backup', mode: reasoningMode, effort: 'high' });
            await main.locator('.chat-header-title').click();
            await expect(popover).not.toBeVisible();
            expect(mockApi.writes()).toEqual([]);

            await page.reload();
            await ready(page);
            await openModel.click();
            await expect(channel).toHaveText('示例通道 B');
            await expect(mode).toHaveAttribute('title', new RegExp(label(reasoningMode === 'off' ? 'chat.reasoningModeOff' : 'chat.reasoningModeOn')));
            await expect(slider).toHaveAttribute('aria-valuenow', '3');
            await expect(slider).toHaveAttribute('aria-disabled', String(reasoningMode === 'off'));
            await expect(value).toHaveText(reasoningMode === 'off'
                ? label('chat.reasoningModeOff') : label('chat.reasoningEffortLabels.high', '高'));
            await main.locator('.chat-header-title').click();
            const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
            await input.fill(message);
            await input.press('Enter');
            await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
            expect(mockApi.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
                method: 'POST', path: '/api/agent/stream',
                body: {
                    message, conversationId: '', aiChannelId: 'demo-backup',
                    reasoning: reasoningMode === 'off' ? { mode: 'off' } : { mode: 'on', effort: 'high' }
                }
            }]);
        });
    }

    test('composer reasoning slider covers all six levels and clamps keyboard boundaries', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        mockApi.respond('GET', '/api/config', { json: { ai: {
            default_channel: 'demo-primary',
            channels: { 'demo-primary': { name: '示例通道 A', model: 'fixture-a' } }
        } } });
        const main = await visit(page, 'chat');
        await main.getByRole('button', { name: label('chat.modelSettingsAria'), exact: true }).click();
        const popover = page.locator('.chat-model-popover');
        const mode = popover.getByRole('button', { name: label('chat.reasoningModeLabel'), exact: true });
        const slider = popover.getByRole('slider', { name: label('chat.reasoningEffortLabel'), exact: true });
        const value = popover.locator('.chat-reasoning-value[role="status"]');
        await expect(popover.getByRole('combobox')).toHaveCount(1);
        await expect(popover.getByRole('slider')).toHaveCount(1);
        await expect(slider).toHaveAttribute('aria-valuemin', '0');
        await expect(slider).toHaveAttribute('aria-valuemax', '5');
        await expect(slider).toHaveAttribute('aria-valuenow', '0');
        await expect(value).toHaveText(label('chat.reasoningModeDefault'));

        await slider.press('End');
        await expect(slider).toHaveAttribute('aria-valuenow', '5');
        await slider.press('Home');
        const levels = [
            ['', label('chat.reasoningModeDefault')],
            ['low', '低'], ['medium', '中'], ['high', '高'], ['xhigh', '超高'], ['max', '最高']
        ];
        for (const [index, [effort, text]] of levels.entries()) {
            if (index > 0) await slider.press('ArrowRight');
            await expect(slider).toHaveAttribute('aria-valuenow', String(index));
            await expect(slider).toHaveAttribute('aria-disabled', 'false');
            await expect(value).toHaveText(text);
            await expect(mode).toHaveAttribute('title', new RegExp(label(index === 0 ? 'chat.reasoningModeDefault' : 'chat.reasoningModeOn')));
            await expect.poll(() => page.evaluate(() => ({
                mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
                effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
            }))).toEqual({ mode: index === 0 ? 'default' : 'on', effort });
        }
        await slider.press('ArrowRight');
        await expect(slider).toHaveAttribute('aria-valuenow', '5');
        await expect(value).toHaveText('最高');
        await expect.poll(() => page.evaluate(() => ({
            mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
            effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
        }))).toEqual({ mode: 'on', effort: 'max' });
        await slider.press('Home');
        await slider.press('ArrowLeft');
        await expect(slider).toHaveAttribute('aria-valuenow', '0');
        await expect(value).toHaveText(label('chat.reasoningModeDefault'));
        await slider.press('ArrowRight');
        await expect(slider).toHaveAttribute('aria-valuenow', '1');
        await slider.press('ArrowLeft');
        await expect(slider).toHaveAttribute('aria-valuenow', '0');
        await expect(value).toHaveText(label('chat.reasoningModeDefault'));
        await expect(mode).toHaveAttribute('title', new RegExp(label('chat.reasoningModeDefault')));
        await expect.poll(() => page.evaluate(() => ({
            mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
            effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
        }))).toEqual({ mode: 'default', effort: '' });
        expect(mockApi.writes()).toEqual([]);
    });

    test('composer reasoning slider selects high with a mouse click and enables reasoning', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        mockApi.respond('GET', '/api/config', { json: { ai: {
            default_channel: 'demo-primary',
            channels: { 'demo-primary': { name: '示例通道 A', model: 'fixture-a' } }
        } } });
        const main = await visit(page, 'chat');
        await main.getByRole('button', { name: label('chat.modelSettingsAria'), exact: true }).click();
        const popover = page.locator('.chat-model-popover');
        const slider = popover.getByRole('slider', { name: label('chat.reasoningEffortLabel'), exact: true });
        const mode = popover.getByRole('button', { name: label('chat.reasoningModeLabel'), exact: true });
        await expect(slider).toHaveAttribute('aria-valuenow', '0');
        const rail = popover.locator('.semi-slider-rail');
        await rail.hover();
        const box = await rail.boundingBox();
        expect(box).not.toBeNull();
        // Click between marks on the unfilled rail; do not bypass the pointer handlers.
        await rail.click({ position: { x: box.width * 0.64, y: box.height / 2 } });
        await expect(slider).toHaveAttribute('aria-valuenow', '3');
        await expect(popover.locator('.chat-reasoning-value[role="status"]')).toHaveText('高');
        await expect(mode).toHaveAttribute('title', new RegExp(label('chat.reasoningModeOn')));
        await expect.poll(() => page.evaluate(() => ({
            mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
            effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
        }))).toEqual({ mode: 'on', effort: 'high' });
        expect(mockApi.writes()).toEqual([]);
    });

    test('composer reasoning reset preserves the selected channel and sends without reasoning', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        mockApi.respond('GET', '/api/config', { json: { ai: {
            default_channel: 'demo-primary',
            channels: {
                'demo-primary': { name: '示例通道 A', model: 'fixture-a' },
                'demo-backup': { name: '示例通道 B', model: 'fixture-b' }
            }
        } } });
        const message = 'FAKE：重置推理后仍使用所选示例通道。';
        const id = mockApi.enableChatStream(message);
        const main = await visit(page, 'chat');
        const openModel = main.getByRole('button', { name: label('chat.modelSettingsAria'), exact: true });
        await openModel.click();
        const popover = page.locator('.chat-model-popover');
        const channel = popover.getByRole('combobox', { name: label('chat.aiChannelLabel'), exact: true });
        const mode = popover.getByRole('button', { name: label('chat.reasoningModeLabel'), exact: true });
        const slider = popover.getByRole('slider', { name: label('chat.reasoningEffortLabel'), exact: true });
        const value = popover.locator('.chat-reasoning-value[role="status"]');
        const reset = popover.getByRole('button', { name: label('chat.reasoningReset', '重置推理设置'), exact: true });
        await channel.click();
        await page.getByRole('listbox').getByText('示例通道 B', { exact: true }).click();
        await slider.press('End');
        await expect(slider).toHaveAttribute('aria-valuenow', '5');
        await mode.click();
        await page.locator('.chat-reasoning-mode-menu').getByRole('menuitem', { name: label('chat.reasoningModeOff') }).click();
        await expect(slider).toHaveAttribute('aria-disabled', 'true');
        await expect.poll(() => page.evaluate(() => ({
            channel: localStorage.getItem('cyberstrike-chat-ai-channel'),
            mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
            effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
        }))).toEqual({ channel: 'demo-backup', mode: 'off', effort: 'max' });

        await expect(reset).toBeEnabled();
        await reset.click();
        await expect(channel).toHaveText('示例通道 B');
        await expect(slider).toHaveAttribute('aria-valuenow', '0');
        await expect(slider).toHaveAttribute('aria-disabled', 'false');
        await expect(value).toHaveText(label('chat.reasoningModeDefault'));
        await expect(mode).toHaveAttribute('title', new RegExp(label('chat.reasoningModeDefault')));
        await expect.poll(() => page.evaluate(() => ({
            channel: localStorage.getItem('cyberstrike-chat-ai-channel'),
            mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
            effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
        }))).toEqual({ channel: 'demo-backup', mode: 'default', effort: '' });
        expect(mockApi.writes()).toEqual([]);
        await page.reload();
        await ready(page);
        await openModel.click();
        await expect(channel).toHaveText('示例通道 B');
        await expect(slider).toHaveAttribute('aria-valuenow', '0');
        await expect(slider).toHaveAttribute('aria-disabled', 'false');
        await expect(value).toHaveText(label('chat.reasoningModeDefault'));
        await expect(mode).toHaveAttribute('title', new RegExp(label('chat.reasoningModeDefault')));
        await main.locator('.chat-header-title').click();
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        await input.fill(message);
        await input.press('Enter');
        await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
        expect(mockApi.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
            method: 'POST', path: '/api/agent/stream',
            body: { message, conversationId: '', aiChannelId: 'demo-backup' }
        }]);
        expect(mockApi.calls('POST', '/api/agent/stream')[0].body).not.toHaveProperty('reasoning');
    });

    test('composer reasoning modes round-trip auto, default and off without losing retained effort', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        mockApi.respond('GET', '/api/config', { json: { ai: {
            default_channel: 'demo-primary',
            channels: { 'demo-primary': { name: '示例通道 A', model: 'fixture-a' } }
        } } });
        const message = 'FAKE：保留自动推理模式与示例强度。';
        const id = mockApi.enableChatStream(message);
        const main = await visit(page, 'chat');
        const openModel = main.getByRole('button', { name: label('chat.modelSettingsAria'), exact: true });
        await openModel.click();
        const popover = page.locator('.chat-model-popover');
        const mode = popover.getByRole('button', { name: label('chat.reasoningModeLabel'), exact: true });
        const slider = popover.getByRole('slider', { name: label('chat.reasoningEffortLabel'), exact: true });
        const value = popover.locator('.chat-reasoning-value[role="status"]');
        const menu = page.locator('.chat-reasoning-mode-menu');
        const modes = {
            default: label('chat.reasoningModeDefault'), off: label('chat.reasoningModeOff'),
            on: label('chat.reasoningModeOn'), auto: label('chat.reasoningModeAuto')
        };
        await expect(value).toHaveText(modes.default);
        await mode.click();
        await expect(menu.getByRole('menuitem')).toHaveCount(4);
        for (const name of Object.values(modes)) await expect(menu.getByRole('menuitem', { name })).toBeVisible();
        await menu.getByRole('menuitem', { name: modes.default }).click();
        // Mode menu actions and genuine keyboard input share the same persisted preferences.
        const transitions = [
            ['auto', 'auto', '', 0, modes.auto],
            ['End', 'auto', 'max', 5, '最高'],
            ['off', 'off', 'max', 5, modes.off],
            ['Home', 'off', 'max', 5, modes.off],
            ['auto', 'auto', 'max', 5, '最高'],
            ['Home', 'default', '', 0, modes.default],
            ['auto', 'auto', '', 0, modes.auto],
            ['End', 'auto', 'max', 5, '最高'],
            ['default', 'default', '', 0, modes.default],
            ['on', 'on', '', 0, modes.on],
            ['ArrowRight', 'on', 'low', 1, '低'],
            ['auto', 'auto', 'low', 1, '低'],
            ['ArrowRight', 'auto', 'medium', 2, '中'],
            ['off', 'off', 'medium', 2, modes.off],
            ['on', 'on', 'medium', 2, '中'],
            ['auto', 'auto', 'medium', 2, '中']
        ];
        for (const [action, expectedMode, effort, level, text] of transitions) {
            await test.step(`${action}: ${expectedMode}/${effort || 'unset'}`, async () => {
                if (modes[action]) {
                    await mode.click();
                    await menu.getByRole('menuitem', { name: modes[action] }).click();
                } else {
                    await slider.press(action);
                }
                await expect(mode).toHaveAttribute('title', new RegExp(modes[expectedMode]));
                await expect(slider).toHaveAttribute('aria-valuenow', String(level));
                await expect(slider).toHaveAttribute('aria-disabled', String(expectedMode === 'off'));
                await expect(value).toHaveText(text);
                await expect.poll(() => page.evaluate(() => ({
                    mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
                    effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
                }))).toEqual({ mode: expectedMode, effort });
            });
        }
        expect(mockApi.writes()).toEqual([]);
        await page.reload();
        await ready(page);
        await openModel.click();
        await expect(mode).toHaveAttribute('title', new RegExp(modes.auto));
        await expect(slider).toHaveAttribute('aria-valuenow', '2');
        await expect(value).toHaveText('中');
        await main.locator('.chat-header-title').click();
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        await input.fill(message);
        await input.press('Enter');
        await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
        expect(mockApi.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
            method: 'POST', path: '/api/agent/stream',
            body: { message, conversationId: '', reasoning: { mode: 'auto', effort: 'medium' } }
        }]);
    });

    test('composer HITL entry edits a local draft in a HITL-only sheet and includes it in the first send', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        mockApi.respond('GET', '/api/hitl/default-config', { json: {
            defaultMode: 'off', defaultReviewer: 'human', defaultTimeoutSeconds: 120, conversationHumanApproval: true
        } });
        const message = 'FAKE：保留草稿并使用人机协同整理示例文本。';
        const id = mockApi.enableChatStream(message);
        const hitl = {
            enabled: true, mode: 'approval', reviewer: 'audit_agent',
            sensitiveTools: ['fixture-read', 'fixture-write'], timeoutSeconds: 60
        };
        mockApi.respond('GET', `/api/hitl/config/${id}`, { json: { hitl } });
        const main = await visit(page, 'chat');
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        const controls = main.locator('.chat-composer-controls');
        const openHitl = controls.getByRole('button', { name: label('chat.hitlTitle'), exact: true });
        await expect(controls.getByRole('button', { name: label('chat.attachmentsLabel', '附件'), exact: true })).toBeEnabled();
        await input.fill(message);
        await openHitl.click();
        const settings = page.getByRole('dialog').filter({ hasText: label('chat.hitlTitle') });
        await expect(settings).toHaveClass(/semi-sidesheet-inner/);
        for (const key of ['chat.aiChannelLabel', 'chat.reasoningModeLabel', 'chat.reasoningEffortLabel']) {
            await expect(settings.getByRole('combobox', { name: label(key), exact: true })).toHaveCount(0);
        }
        const mode = settings.getByRole('combobox', { name: label('chat.hitlModeLabel'), exact: true });
        const timeout = settings.getByRole('combobox', { name: label('chat.hitlTimeoutLabel'), exact: true });
        const tools = settings.getByRole('textbox', { name: label('chat.hitlWhitelistTools'), exact: true });
        await expect(mode).toHaveText(label('chat.hitlModeOff'));
        await mode.click();
        await page.getByRole('listbox').getByText(label('chat.hitlModeApproval'), { exact: true }).click();
        await settings.getByText(label('chat.hitlReviewerAgent'), { exact: true }).click();
        await timeout.click();
        await page.getByRole('listbox').getByText(label('chat.hitlTimeoutOneMinute'), { exact: true }).click();
        await tools.fill('fixture-read, fixture-write');
        await tools.press('Tab');
        expect(mockApi.writes()).toEqual([]);
        await settings.locator('button.semi-sidesheet-close').click();
        await expect(input).toHaveValue(message);

        await openHitl.click();
        await expect(mode).toHaveText(label('chat.hitlModeApproval'));
        await expect(settings.getByRole('radio', { name: label('chat.hitlReviewerAgent'), exact: true })).toBeChecked();
        await expect(timeout).toHaveText(label('chat.hitlTimeoutOneMinute'));
        await expect(tools).toHaveValue('fixture-read, fixture-write');
        expect(mockApi.writes()).toEqual([]);
        await settings.locator('button.semi-sidesheet-close').click();
        await input.press('Enter');
        await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
        expect(mockApi.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
            method: 'POST', path: '/api/agent/stream', body: { message, conversationId: '', hitl }
        }]);
    });

    test('composer HITL entry saves an existing conversation and shares the full header settings', async ({ page, mockApi }) => {
        const conversation = mockEmptyConversation(mockApi, PROJECT.id);
        let hitl = {
            enabled: true, mode: 'approval', reviewer: 'human',
            sensitiveTools: ['fixture-existing-tool'], timeoutSeconds: 90
        };
        mockApi.respond('GET', `/api/hitl/config/${conversation.id}`, () => ({ json: {
            hitl, defaultTimeoutSeconds: 90, hitlGlobalToolWhitelist: []
        } }));
        mockApi.respond('PUT', '/api/hitl/config', request => {
            const { conversationId, ...config } = request.body;
            hitl = config;
            return { json: { ok: true } };
        });
        const main = await visit(page, `chat?conversation=${conversation.id}`);
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        const draft = 'FAKE：保存人机协同设置时保留会话草稿。';
        await input.fill(draft);
        const openHitl = main.locator('.chat-composer-controls').getByRole('button', { name: label('chat.hitlTitle'), exact: true });
        await openHitl.click();
        const settings = page.getByRole('dialog').filter({ hasText: label('chat.hitlTitle') });
        const mode = settings.getByRole('combobox', { name: label('chat.hitlModeLabel'), exact: true });
        await expect(mode).toHaveText(label('chat.hitlModeApproval'));
        expect(mockApi.writes()).toEqual([]);
        await mode.click();
        await page.getByRole('listbox').getByText(label('chat.hitlModeReviewEdit'), { exact: true }).click();
        await expect.poll(() => mockApi.calls('PUT', '/api/hitl/config').map(call => call.body)).toEqual([{
            conversationId: conversation.id, enabled: true, mode: 'review_edit', reviewer: 'human',
            sensitiveTools: ['fixture-existing-tool'], timeoutSeconds: 90
        }]);
        await expect(settings.getByRole('status')).toHaveText(label('chat.hitlApplyOkSync'));
        await settings.locator('button.semi-sidesheet-close').click();

        await main.locator('.chat-header').getByRole('button', { name: label('chat.sessionSettingsTitle'), exact: true }).click();
        const fullSettings = page.getByRole('dialog').filter({ hasText: label('chat.sessionSettingsTitle') });
        for (const key of ['chat.aiChannelLabel', 'chat.reasoningModeLabel', 'chat.reasoningEffortLabel']) {
            await expect(fullSettings.getByRole('combobox', { name: label(key), exact: true })).toBeVisible();
        }
        await expect(fullSettings.getByRole('combobox', { name: label('chat.hitlModeLabel'), exact: true })).toHaveText(label('chat.hitlModeReviewEdit'));
        await fullSettings.locator('button.semi-sidesheet-close').click();
        await openHitl.click();
        await expect(mode).toHaveText(label('chat.hitlModeReviewEdit'));
        await settings.locator('button.semi-sidesheet-close').click();
        await expect(input).toHaveValue(draft);
        expect(mockApi.writes()).toHaveLength(1);
    });
});

test.describe('chat composer read-only permissions', () => {
    test.use({ appPermissions: UI_PERMISSIONS.filter(permission => permission !== 'chat:write') });

    test('disables the composer reasoning slider, mode and reset without chat:write', async ({ page, mockApi }) => {
        const conversation = mockEmptyConversation(mockApi);
        mockApi.respond('GET', '/api/config', { json: { ai: {
            default_channel: 'demo-primary',
            channels: {
                'demo-primary': { name: '示例通道 A', model: 'fixture-a' },
                'demo-backup': { name: '示例通道 B', model: 'fixture-b' }
            }
        } } });
        const main = await visit(page, `chat?conversation=${conversation.id}`);
        await page.evaluate(() => {
            localStorage.setItem('cyberstrike-chat-ai-channel', 'demo-backup');
            localStorage.setItem('cyberstrike-chat-reasoning-mode', 'auto');
            localStorage.setItem('cyberstrike-chat-reasoning-effort', 'max');
        });
        await page.reload();
        await ready(page);
        await main.getByRole('button', { name: label('chat.modelSettingsAria'), exact: true }).click();
        const popover = page.locator('.chat-model-popover');
        const channel = popover.getByRole('combobox', { name: label('chat.aiChannelLabel'), exact: true });
        const mode = popover.getByRole('button', { name: label('chat.reasoningModeLabel'), exact: true });
        const slider = popover.getByRole('slider', { name: label('chat.reasoningEffortLabel'), exact: true });
        const reset = popover.getByRole('button', { name: label('chat.reasoningReset', '重置推理设置'), exact: true });
        const value = popover.locator('.chat-reasoning-value[role="status"]');
        await expect(popover.getByRole('combobox')).toHaveCount(1);
        await expect(channel).toHaveText('示例通道 B');
        await expect(channel).toBeDisabled();
        await expect(mode).toHaveAttribute('title', new RegExp(label('chat.reasoningModeAuto')));
        await expect(mode).toBeDisabled();
        await expect(reset).toBeDisabled();
        await expect(slider).toHaveAttribute('aria-disabled', 'true');
        await expect(slider).toHaveAttribute('aria-valuenow', '5');
        await expect(value).toHaveText('最高');
        const track = popover.locator('.semi-slider-track');
        await expect(popover.locator('.chat-reasoning-picker')).toHaveClass(/is-max/);
        await expect(track).toHaveCSS('background-image', /linear-gradient/);
        await expect(track).toHaveCSS('animation-name', 'none');
        expect(await track.evaluate(element => getComputedStyle(element, '::after').animationName)).toBe('none');
        for (const key of ['Home', 'ArrowRight', 'End']) {
            await slider.press(key);
            await expect(slider).toHaveAttribute('aria-valuenow', '5');
        }
        await expect(value).toHaveText('最高');
        await expect.poll(() => page.evaluate(() => ({
            channel: localStorage.getItem('cyberstrike-chat-ai-channel'),
            mode: localStorage.getItem('cyberstrike-chat-reasoning-mode'),
            effort: localStorage.getItem('cyberstrike-chat-reasoning-effort')
        }))).toEqual({ channel: 'demo-backup', mode: 'auto', effort: 'max' });
        await main.locator('.chat-header-title').click();
        await expect(main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') })).toBeDisabled();
        await expect(main.getByRole('button', { name: /发送$/ })).toBeDisabled();
        expect(mockApi.writes()).toEqual([]);
    });

    test('disables stored project changes and HITL editing without their respective write permissions', async ({ page, mockApi }) => {
        const conversation = mockEmptyConversation(mockApi, PROJECT.id);
        mockApi.respond('GET', `/api/hitl/config/${conversation.id}`, { json: {
            hitl: {
                enabled: true, mode: 'approval', reviewer: 'human',
                sensitiveTools: ['fixture-existing-tool'], timeoutSeconds: 90
            }
        } });
        const main = await visit(page, `chat?conversation=${conversation.id}`);
        const context = main.locator('.chat-composer-context');
        await expect(context.getByRole('combobox').filter({ hasText: PROJECT.name })).toBeDisabled();
        await expect(main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') })).toBeDisabled();
        await expect(main.getByRole('button', { name: /发送$/ })).toBeDisabled();
        const controls = main.locator('.chat-composer-controls');
        await expect(controls.getByRole('button', { name: label('chat.attachmentsLabel', '附件'), exact: true })).toBeDisabled();
        await controls.getByRole('button', { name: label('chat.hitlTitle'), exact: true }).click();
        const settings = page.getByRole('dialog').filter({ hasText: label('chat.hitlTitle') });
        const mode = settings.getByRole('combobox', { name: label('chat.hitlModeLabel'), exact: true });
        const timeout = settings.getByRole('combobox', { name: label('chat.hitlTimeoutLabel'), exact: true });
        const tools = settings.getByRole('textbox', { name: label('chat.hitlWhitelistTools'), exact: true });
        await expect(mode).toHaveText(label('chat.hitlModeApproval'));
        await expect(mode).toBeDisabled();
        await expect(timeout).toHaveText('90s');
        await expect(timeout).toBeDisabled();
        await expect(tools).toHaveValue('fixture-existing-tool');
        await expect(tools).toBeDisabled();
        await expect(settings.getByRole('radio', { name: label('chat.hitlReviewerHuman'), exact: true })).toBeChecked();
        for (const key of ['chat.hitlReviewerHuman', 'chat.hitlReviewerAgent']) {
            await expect(settings.getByRole('radio', { name: label(key), exact: true })).toBeDisabled();
        }
        await settings.locator('button.semi-sidesheet-close').click();
        expect(mockApi.writes()).toEqual([]);
    });
});

test.describe('running-status stop permission', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'tasks:write'] });
    test('an authorized user can stop the displayed background run', async ({ page, mockApi }) => {
        let running = true;
        mockApi.respond('GET', '/api/agent-loop/tasks', () => ({ json: {
            tasks: running ? [{ conversationId: CONVERSATION.id }] : []
        } }));
        mockApi.respond('POST', '/api/agent-loop/cancel', () => {
            running = false;
            return { json: { status: 'cancelled' } };
        });
        await page.goto('/static/app/#/chat?conversation=' + CONVERSATION.id);
        const main = await ready(page);
        await main.getByRole('button', { name: /停止$/ }).click();
        await expect(main.getByRole('button', { name: /发送$/ })).toBeVisible();
        expect(mockApi.calls('POST', '/api/agent-loop/cancel')[0]?.body).toEqual({ conversationId: CONVERSATION.id });
    });
});

test('switching conversation hides the previous approval and keeps each draft separate', async ({ page, mockApi }) => {
    const second = { ...CONVERSATION, id: 'fixture-conversation-other', title: '另一个示例会话' };
    const item = { id: 'fixture-approval', toolName: 'fixture-read-only-request', conversationId: CONVERSATION.id };
    mockApi.respond('GET', '/api/conversations', { json: { conversations: [CONVERSATION, second] } });
    mockApi.respond('GET', '/api/conversations/' + second.id, { json: second });
    mockApi.respond('GET', '/api/conversations/' + second.id + '/plan-tasks', { json: { tasks: [], total: 0 } });
    let release;
    const pending = new Promise(resolve => { release = resolve; });
    mockApi.respond('GET', '/api/hitl/pending', async (_, url) => {
        if (url.searchParams.get('conversationId') === second.id) {
            await pending;
            return { json: { items: [] } };
        }
        return { json: { items: [item] } };
    });
    try {
        await page.goto('/static/app/#/chat?conversation=' + CONVERSATION.id);
        const main = await ready(page);
        const card = main.locator('.chat-hitl-card').filter({ hasText: item.toolName });
        const composer = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        await expect(card).toBeVisible();
        await expect(card.getByRole('button', { name: /批准/ })).toBeDisabled();
        await composer.fill('仅属于第一个示例会话的草稿');
        await main.getByRole('button', { name: new RegExp('^' + second.title) }).click();
        await expect(main.getByRole('heading', { name: second.title, exact: true })).toBeVisible();
        await expect(card).toHaveCount(0);
        await expect(composer).toHaveValue('');
        await main.getByRole('button', { name: new RegExp('^' + CONVERSATION.title) }).click();
        await expect(composer).toHaveValue('仅属于第一个示例会话的草稿');
        expect(mockApi.writes()).toEqual([]);
    } finally {
        release();
    }
});

test.describe('resource administration', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'rbac:write'] });
    test('selecting a role submits the role ID', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/rbac/roles', { json: {
            roles: [{ id: 'custom-role-fixture-001', name: '演示审阅员', permissions: ['project:read'] }]
        } });
        mockApi.respond('POST', '/api/rbac/users', { json: { user: { id: 'fixture-user' } } });
        const main = await visit(page, 'platform-rbac');
        await main.getByRole('tab', { name: /^平台用户/ }).click();
        await main.getByRole('button', { name: /新建用户$/ }).click();
        const dialog = page.getByRole('dialog').filter({ hasText: '新建用户' });
        await dialog.locator('input').nth(0).fill('fixture_user');
        await dialog.locator('input[type=password]').fill('not-a-real-password');
        await dialog.locator('.semi-select').click();
        await page.locator('.semi-select-option-text').getByText('演示审阅员', { exact: true }).click();
        await dialog.getByRole('button', { name: '确定', exact: true }).click();
        await expect.poll(() => mockApi.calls('POST', '/api/rbac/users')[0]?.body.roles).toEqual(['custom-role-fixture-001']);
    });

    test('the resource grant form submits the selected user and resource', async ({ page, mockApi }) => {
        mockApi.respond('GET', '/api/rbac/resources', { json: {
            resources: [{ id: 'fixture-project', label: '示例项目' }], total: 1, has_more: false
        } });
        mockApi.respond('POST', '/api/rbac/resource-assignments', { json: { created: 1 } });
        const main = await visit(page, 'platform-rbac');
        await main.getByRole('tab', { name: /^资源授权/ }).click();
        await main.getByRole('button', { name: /新增资源授权$/ }).click();
        const grant = page.getByRole('dialog').filter({ hasText: '新增资源授权' });
        await grant.locator('.semi-select').nth(0).click();
        await page.locator('.semi-select-option-text').getByText('示例用户', { exact: true }).click();
        await grant.locator('.semi-select').nth(2).click();
        await page.locator('.semi-select-option-text').getByText('示例项目', { exact: true }).click();
        await grant.getByRole('button', { name: '保存授权' }).click();
        await expect.poll(() => mockApi.calls('POST', '/api/rbac/resource-assignments')[0]?.body).toEqual({
            user_id: 'FAKE-PLAYWRIGHT-USER', resource_type: 'project', resource_ids: ['fixture-project']
        });
    });
});

test('API docs uses the completed URL of a read-only project endpoint', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/openapi/spec', { json: {
        openapi: '3.0.0', paths: { '/api/projects/{id}': { get: {
            tags: ['项目'], parameters: [{ name: 'id', in: 'path', required: true, schema: { type: 'string' } }],
            responses: { 200: { description: '示例项目' } }
        } } }
    } });
    const main = await visit(page, 'api-docs');
    await main.getByRole('button', { name: /projects/ }).click();
    await main.getByRole('textbox', { name: '路径参数 id' }).fill('fixture-project');
    await main.getByRole('button', { name: /发送请求/ }).click();
    await expect.poll(() => mockApi.calls('GET', '/api/projects/fixture-project').length).toBeGreaterThan(0);
});

test('a legacy acceptance link opens the selected criterion and writes revision-checked evidence', async ({ page, mockApi }) => {
    const id = ASSESSMENT.id;
    const path = '/api/deep-assessments/' + id + '/acceptance';
    mockApi.respond('GET', '/api/deep-assessments/' + id + '/events', { json: { events: [] } });
    mockApi.respond('GET', path, { json: { rows: [{
        number: 2, text: '复核示例报告', status: 'missing', fact_ids: [], receipt_ids: [], revision: 3
    }] } });
    mockApi.respond('GET', path + '/sources', { json: { sources: [
        { id: 'fixture-fact', title: '示例观察事实', state: 'observed' }
    ] } });
    mockApi.respond('PUT', path + '/2', { json: { saved: true } });
    await page.goto('/static/app/#deep-assessments?assessment_id=' + id + '&tab=acceptance&criterion=2');
    const main = await ready(page);
    await expect(main.getByRole('tab', { name: /验收/ })).toHaveAttribute('aria-selected', 'true');
    const criterion = page.getByRole('dialog').filter({ hasText: '验收项 2' });
    const edit = criterion.getByRole('button', { name: '编辑证据关联' });
    await edit.click();
    const editor = page.getByRole('dialog', { name: '编辑验收证据关联' });
    await expect(editor).toHaveClass(/semi-sidesheet-inner/);
    await page.keyboard.press('Escape');
    await expect(editor).toHaveCount(0);
    await expect(criterion).toBeVisible();
    await edit.click();
    const evidence = editor.getByRole('checkbox', { name: '关联 示例观察事实' });
    await evidence.press('Space');
    await expect(evidence).toBeChecked();
    const review = editor.getByRole('checkbox', { name: '标记待人工复核' });
    await review.press('Space');
    await expect(review).toBeChecked();
    await editor.getByRole('textbox', { name: '复核说明' }).fill('示例说明');
    await editor.getByRole('button', { name: '保存证据关联' }).click();
    await expect.poll(() => mockApi.calls('PUT', path + '/2')[0]?.body).toEqual({
        revision: 3, fact_ids: ['fixture-fact'], receipt_ids: [], needs_review: true, review_note: '示例说明'
    });
});

test.describe('sign-in integration', () => {
    test.use({ authenticated: false });
    test('successful sign-in opens the app with a synthetic scoped session', async ({ page, mockApi }) => {
        // The fixture validates its own synthetic token on all subsequent requests.
        mockApi.respond('POST', '/api/auth/login', { json: {
            token: 'FAKE-PLAYWRIGHT-NOT-A-REAL-TOKEN', expires_at: new Date(Date.now() + 3600000).toISOString(),
            permissions: UI_PERMISSIONS, roles: [], user: { id: 'fixture-user', username: 'fixture' }
        } });
        await page.goto('/static/app/');
        const form = page.locator('.cs-login-form');
        await form.getByLabel(label('login.username', '用户名'), { exact: true }).fill('fixture');
        await form.getByLabel(label('login.password', '密码'), { exact: true }).fill('fixture-password');
        await form.getByRole('button', { name: /登录$/ }).click();
        await ready(page);
        await expect(form).toHaveCount(0);
    });
});
