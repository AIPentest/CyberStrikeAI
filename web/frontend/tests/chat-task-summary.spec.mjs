import { test, expect, ready, CONVERSATION, label } from './fixtures.mjs';

const summaryStart = '已完成本轮检查，发现两项需要复核的问题。';
const summary = `## 检查结果\n\n${summaryStart}\n\n` + Array.from({ length: 35 }, (_, i) => `### 证据 ${i + 1}\n\n这是本地模拟记录的证据说明，保留测试范围、验证结果和待确认事项。`).join('\n\n');

function processDetails(text) {
    const details = [];
    for (let i = 1; i <= 24; i++) {
        details.push({ eventType: 'iteration', message: `第 ${i} 轮`, data: { iteration: i, piScope: 'main' } });
        details.push({ eventType: 'tool_call', data: { toolCallId: `call-${i}`, agentName: 'main', toolName: 'read_file', argumentsObj: { path: 'fixture.txt' } } });
        details.push({ eventType: 'tool_result', message: 'fixture evidence', data: { toolCallId: `call-${i}`, result: 'fixture evidence', success: true } });
    }
    details.push({ eventType: 'tool_call', data: { toolCallId: 'exit', toolName: 'exit', agentName: 'main', argumentsObj: { final_result: text } } });
    details.push({ eventType: 'tool_result', message: text, data: { toolCallId: 'exit', result: text, success: true } });
    details.push({ eventType: 'agent_usage_summary', data: { agentName: 'main', modelCalls: 24, promptTokens: 1000, completionTokens: 200, totalTokens: 1200 } });
    details.push({ eventType: 'finalization_check', message: '已交付本轮回复。', data: { finalizable: true, finalized: true, finalText: text } });
    return details;
}

function completedConversation(text = summary) {
    return { ...CONVERSATION, messages: [CONVERSATION.messages[0], { ...CONVERSATION.messages[1], content: text, processDetails: processDetails(text) }] };
}

for (const [width, colorScheme] of [[1440, 'light'], [390, 'dark']]) {
    test(`history opens at the expanded summary after 24 iterations at ${width}px`, async ({ page, mockApi }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.emulateMedia({ colorScheme });
        mockApi.respond('GET', `/api/conversations/${CONVERSATION.id}`, { json: completedConversation() });
        await page.goto(`/static/app/#/chat?conversation=${CONVERSATION.id}`);
        const main = await ready(page);
        const result = main.getByRole('region', { name: '任务总结', exact: true });
        await expect(result.locator('details')).toHaveAttribute('open', '');
        await expect(result.locator('summary')).toBeInViewport({ ratio: 1 });
        await expect(result.getByText(summaryStart, { exact: true })).toBeInViewport();
        await expect(main.locator('.chat-msg-user [data-chat-summary]')).toHaveCount(0);
        const order = await main.locator('.chat-msg-assistant .chat-turn-body').evaluate(el => {
            const children = [...el.children];
            return children[children.length - 1].matches('[data-chat-summary]');
        });
        expect(order).toBe(true);
        expect(await result.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);

        // The report is readable without opening exit, and the original tool evidence remains available.
        await result.locator('summary').click();
        await expect(result.locator('details')).not.toHaveAttribute('open', '');
        await result.locator('summary').click();
        await expect(result.getByText(summaryStart, { exact: true })).toBeVisible();
        await main.locator('.chat-messages').evaluate(el => { el.scrollTop = 0; });
        await main.locator('.chat-tool').first().locator('.semi-collapse-header').click();
        await expect(main.locator('.chat-tool').first()).toContainText('fixture evidence');
        await page.reload();
        await ready(page);
        await expect(result.locator('summary')).toBeInViewport({ ratio: 1 });
        expect(mockApi.writes()).toEqual([]);
    });
}

test('Plan-Execute completion focuses the summary while reading earlier steps and survives history reconciliation', async ({ page, mockApi }) => {
    const path = `/api/conversations/${CONVERSATION.id}`;
    const previous = completedConversation('上一轮的总结');
    mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
    mockApi.respond('GET', path, { json: previous });
    await page.goto(`/static/app/#/chat?conversation=${CONVERSATION.id}`);
    const main = await ready(page);
    await main.getByRole('combobox', { name: label('chat.agentModeSelectAria', '选择对话执行模式') }).click();
    await page.getByRole('option').filter({ hasText: 'Plan-Execute' }).click();
    await expect(main.locator('.chat-composer-mode')).toHaveText('Plan-Execute');

    let finishStream;
    const streamGate = new Promise(resolve => { finishStream = resolve; });
    let restoreHistory;
    const historyGate = new Promise(resolve => { restoreHistory = resolve; });
    mockApi.respond('POST', '/api/multi-agent/stream', async () => {
        await streamGate;
        mockApi.respond('GET', path, async () => {
            await historyGate;
            return { json: { ...previous, messages: [...previous.messages, { id: 'new-user', role: 'user', content: '继续整理证据' }, { id: 'new-assistant', role: 'assistant', content: summary, processDetails: processDetails(summary) }] } };
        });
        const events = [
            ...processDetails(summary).map(detail => ({ type: detail.eventType, message: detail.message || '', data: detail.data })),
            { type: 'response', message: summary, data: { conversationId: CONVERSATION.id, messageId: 'new-assistant', finalizable: true } },
            { type: 'done', data: {} }
        ];
        return { contentType: 'text/event-stream; charset=utf-8', body: events.map(event => `data: ${JSON.stringify(event)}\n\n`).join('') };
    });
    const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容'), exact: true });
    try {
        await input.fill('继续整理证据');
        await main.getByRole('button', { name: label('chat.send', '发送'), exact: true }).click();
        await expect(main.locator('.chat-msg-live')).toBeVisible();
        await main.locator('.chat-messages').evaluate(el => { el.scrollTop = 0; el.dispatchEvent(new Event('scroll')); });
        finishStream();
        const liveSummary = main.locator('.chat-msg-live [data-chat-summary]');
        await expect(liveSummary.locator('summary')).toBeInViewport({ ratio: 1 });
        await expect(liveSummary.getByText(summaryStart, { exact: true })).toBeInViewport();
        restoreHistory();
        await expect(main.locator('.chat-msg-live')).toHaveCount(0);
        const stored = main.locator('.chat-msg-assistant').last().locator('[data-chat-summary]');
        await expect(stored.locator('summary')).toBeInViewport({ ratio: 1 });
        await expect(stored.getByText(summaryStart, { exact: true })).toBeInViewport();
        expect(mockApi.writes()).toHaveLength(1);
    } finally {
        finishStream();
        restoreHistory();
    }
});

test('a run without final text retains its diagnostic and has no fabricated summary', async ({ page, mockApi }) => {
    const conversation = completedConversation('');
    conversation.messages[1].processDetails = [{ eventType: 'error', message: '模型调用失败，尚未生成最终正文' }];
    mockApi.respond('GET', `/api/conversations/${CONVERSATION.id}`, { json: conversation });
    await page.goto(`/static/app/#/chat?conversation=${CONVERSATION.id}`);
    const main = await ready(page);
    await expect(main.locator('[data-chat-summary]')).toHaveCount(0);
    await expect(main.getByText('模型调用失败，尚未生成最终正文', { exact: true })).toBeInViewport();
});
