import { expect, test, UI_PERMISSIONS, visit } from './fixtures.mjs';

const statusPath = '/api/system/update';
const checkPath = statusPath + '/check';
const applyPath = statusPath + '/apply';
const jobPath = statusPath + '/job';
const currentCommit = 'ab123456';
const nextCommit = 'cd123456';
const local = { installed: true, canBuild: true, commit: currentCommit };
const available = { ...local, remoteCommit: nextCommit, updateAvailable: true, behind: 1 };
const running = { id: 'fixture-update', kind: 'apply', state: 'running' };

test.use({ appPermissions: [...UI_PERMISSIONS, 'update:apply'] });

async function openUpdate(page) {
    const main = await visit(page, 'settings?section=update');
    await expect(main.getByRole('button', { name: '检查更新', exact: true })).toHaveCount(0);
    return main;
}

test('Update checks the configured source and reports when the versions match', async ({ page, mockApi }) => {
    mockApi.respond('GET', statusPath, { json: { status: local } });
    mockApi.respond('POST', checkPath, { json: { status: { ...local, remoteCommit: currentCommit } } });
    const main = await openUpdate(page);
    expect(mockApi.writes()).toEqual([]);
    await main.getByRole('button', { name: '更新', exact: true }).click();
    await expect(main.getByText('已是最新版', { exact: true })).toBeVisible();
    expect(mockApi.writes().map(entry => entry.path)).toEqual([checkPath]);
});

test('Update starts a single job and refreshes local status after it builds', async ({ page, mockApi }) => {
    let completed = false;
    const succeeded = { ...running, state: 'succeeded', result: {
        fromCommit: currentCommit, toCommit: nextCommit, binaryBuilt: true, needsRestart: true
    } };
    mockApi.respond('GET', statusPath, () => ({ json: {
        status: completed ? { ...local, commit: nextCommit } : local,
        job: completed ? succeeded : null,
        canRestart: false,
        needsRestart: completed
    } }));
    mockApi.respond('POST', checkPath, { json: { status: available } });
    mockApi.respond('POST', applyPath, { status: 202, json: { job_id: running.id, state: running.state } });
    mockApi.respond('GET', jobPath, () => ({ json: { job: completed ? succeeded : running } }));
    const main = await openUpdate(page);
    const update = main.getByRole('button', { name: '更新', exact: true });
    await update.click();
    await expect(main.getByText('进行中', { exact: true })).toBeVisible();
    await expect(update).toBeDisabled();
    expect(mockApi.writes().map(({ path, body }) => [path, body])).toEqual([
        [checkPath, {}],
        [applyPath, { restart: false }]
    ]);
    completed = true;
    await expect(main.getByText('已完成', { exact: true })).toBeVisible();
    await expect(main.getByText('二进制已换新，重启后才会生效', { exact: true })).toBeVisible();
    await expect(update).toBeEnabled();
});

test('Update reloads the page after the new service boot answers', async ({ page, mockApi }) => {
    let restarted = false;
    mockApi.respond('GET', statusPath, () => ({ json: {
        status: { ...local, commit: restarted ? nextCommit : currentCommit },
        buildRevision: restarted ? nextCommit + 'deadbeef' : currentCommit,
        bootID: restarted ? 'boot-new' : 'boot-old',
        autoRestart: true
    } }));
    mockApi.respond('POST', checkPath, { json: { status: available } });
    mockApi.respond('POST', applyPath, { status: 202, json: {
        job_id: running.id, state: running.state, bootID: 'boot-old'
    } });
    mockApi.respond('GET', jobPath, () => ({ json: { job: restarted ? null : running } }));
    const main = await openUpdate(page);
    await main.getByRole('button', { name: '更新', exact: true }).click();
    await expect(main.getByText('进行中', { exact: true })).toBeVisible();
    expect(mockApi.calls('POST', applyPath)[0].body).toEqual({ restart: true });
    restarted = true;
    await expect(main.getByText('还没有更新记录', { exact: true })).toBeVisible();
    await expect(main.getByText(nextCommit + 'dead', { exact: true })).toBeVisible();
    expect(mockApi.calls('POST', applyPath)).toHaveLength(1);
});

test('Update reports fetch failures and blocked installs without submitting a job', async ({ page, mockApi }) => {
    mockApi.respond('GET', statusPath, { json: { status: local } });
    const main = await openUpdate(page);
    const update = main.getByRole('button', { name: '更新', exact: true });

    mockApi.respond('POST', checkPath, { json: { status: { ...local, checkError: '示例更新源暂不可达' } } });
    await update.click();
    await expect(main.getByText('检查失败：示例更新源暂不可达', { exact: true })).toBeVisible();

    mockApi.respond('POST', checkPath, { json: { status: { ...available, ahead: 1, diverged: true } } });
    await update.click();
    await expect(main.getByText(/这是合并而不是下载/)).toBeVisible();

    mockApi.respond('POST', checkPath, { json: { status: {
        ...available, blockingChanges: [{ path: 'fixture.go', status: 'M' }]
    } } });
    await update.click();
    await expect(main.getByText(/有 1 个产品源码文件被本地改过/)).toBeVisible();
    expect(mockApi.writes().map(entry => entry.path)).toEqual([checkPath, checkPath, checkPath]);
});

test('Update handles a pending build at the same version and follows a concurrent job', async ({ page, mockApi }) => {
    const pending = { ...local, buildPending: true, remoteCommit: currentCommit };
    mockApi.respond('GET', statusPath, { json: { status: pending } });
    mockApi.respond('POST', checkPath, { json: { status: pending } });
    mockApi.respond('POST', applyPath, { status: 409, json: { error: '更新正在进行', job: running } });
    mockApi.respond('GET', jobPath, { json: { job: running } });
    const main = await openUpdate(page);
    const update = main.getByRole('button', { name: '更新', exact: true });
    await update.click();
    await expect(main.getByText('进行中', { exact: true })).toBeVisible();
    await expect(update).toBeDisabled();
    expect(mockApi.writes().map(entry => entry.path)).toEqual([checkPath, applyPath]);
});
