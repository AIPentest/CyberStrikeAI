const { test } = require('node:test');
const assert = require('node:assert/strict');
const { fixture, queue } = require('./helpers/batch-task-ui-fixture.cjs');

test('original batch task page loads, filters and paginates existing queues', async t => {
    const { page, state, refresh } = await fixture(t);
    for (let i = 0; i < 22; i++) state.queues.set('q-' + i, { ...queue('q-' + i), title: 'Fixture ' + i, status: 'paused' });
    await refresh();
    assert.equal(await page.locator('#batch-queues-list .batch-queue-item').count(), 10);
    await page.locator('#batch-queues-pagination button[onclick="goBatchQueuesPage(2)"]').first().click();
    await page.waitForFunction(() => document.querySelector('#batch-queues-pagination .pagination-page').textContent.includes('2'));
    await page.locator('#batch-queues-page-size-pagination').selectOption('20');
    await page.waitForFunction(() => document.querySelectorAll('#batch-queues-list .batch-queue-item').length === 20);
    await page.locator('.tasks-filter-select-trigger').click();
    await page.locator('.tasks-filter-select-option[data-value="pending"]').click();
    await page.waitForFunction(() => document.querySelectorAll('#batch-queues-list .batch-queue-item').length === 1);
    await page.locator('.tasks-filter-select-trigger').click();
    await page.locator('.tasks-filter-select-option[data-value="all"]').click();
    await page.locator('#batch-queues-search').fill('Fixture 21');
    await page.waitForFunction(() => document.querySelectorAll('#batch-queues-list .batch-queue-item').length === 1 && document.querySelector('#batch-queues-list').textContent.includes('Fixture 21'));
    assert.ok(state.requests.every(r => !r.path.startsWith('/api/long-tasks') && r.path !== '/api/tasks'));
    assert.equal(await page.locator('#long-task-workspace, #page-assessments').count(), 0);
    assert.deepEqual(state.pageErrors, []);
});

test('original creation and detail dialogs retain batch controls without automatically starting work', async t => {
    const { page, state } = await fixture(t);
    await page.locator('#page-tasks button[onclick="showBatchImportModal()"]').click();
    // The existing modal focuses its initial field on the next animation frame.
    // Wait for that frame before filling another field, especially in parallel CI.
    await page.waitForFunction(() => document.activeElement === document.getElementById('batch-tasks-input'));
    await page.locator('#batch-tasks-input').fill('Read local fixture A\nRead local fixture B');
    await page.locator('#batch-queue-title').fill('Restored batch workflow');
    assert.equal(await page.locator('#batch-queue-execute-now').isChecked(), false);
    await page.locator('#batch-import-modal button[onclick="createBatchQueue()"]').click();
    await page.locator('#batch-queue-detail-content [data-bq-detail-for="new-queue"]').waitFor();
    const created = state.requests.find(r => r.method === 'POST' && r.path === '/api/batch-tasks');
    assert.deepEqual(created.body.tasks, ['Read local fixture A', 'Read local fixture B']);
    assert.equal(created.body.executeNow, false);
    assert.equal(created.body.scheduleMode, 'manual');
    assert.equal(await page.locator('#batch-queue-detail-content .batch-task-item').count(), 2);
    await page.locator('#batch-queue-add-task-btn').click();
    await page.locator('#add-task-message').fill('Read local fixture C');
    await page.locator('#add-batch-task-modal button[onclick="saveAddBatchTask()"]').click();
    await page.waitForFunction(() => document.querySelectorAll('#batch-queue-detail-content .batch-task-item').length === 3);
    await page.locator('#batch-queue-start-btn').click();
    await page.locator('#batch-queue-pause-btn').waitFor({ state: 'visible' });
    await page.locator('#batch-queue-pause-btn').click();
    await page.locator('#batch-queue-start-btn').waitFor({ state: 'visible' });
    assert.equal(state.queues.get('new-queue').status, 'paused');
    await page.locator('#batch-queue-delete-btn').click();
    await page.waitForFunction(() => !document.querySelector('#batch-queues-list [data-queue-id="new-queue"]'));
    assert.equal(state.queues.has('new-queue'), false);
    assert.deepEqual(state.pageErrors, []);
});

test('restored task list and creation modal fit desktop, dark mode and mobile', async t => {
    const { page, state } = await fixture(t);
    for (const width of [1440, 390]) {
        await page.setViewportSize({ width, height: 900 });
        await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
        await page.locator('#page-tasks button[onclick="showBatchImportModal()"]').click();
        const modal = page.locator('#batch-import-modal .modal-content');
        await page.locator('#batch-tasks-input').waitFor({ state: 'visible' });
        const rect = await modal.boundingBox();
        assert.ok(rect && rect.width <= width && rect.x >= 0);
        await page.locator('#batch-import-modal button[onclick="closeBatchImportModal()"]').last().click();
    }
    assert.deepEqual(state.pageErrors, []);
});
