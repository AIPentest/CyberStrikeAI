// 控制台「一键更新」：这一页动的就是当前正在应答请求的那套安装树本身。
// 三条硬规矩，每一条都是这个仓库已经踩过一次的坑：
//   1) 查不了必须说查不了。checkError 非空时页面写出原因，绝不渲染成「已是最新」——
//      把"没查成"画成"没更新"，操作员要等到下一次故障才发现树早就没动了。
//   2) 提示只念真实 HTTP 结果，而且只用平台确实存在的 showNotification；没有就什么都不做。
//   3) 服务端来的字符串进 HTML 一律 escapeHtml，进属性一律 escapeAttr。
const UPDATE_POLL_INTERVAL_MS = 1500;
// 重启守侧：进程退出到新进程接客之间的间隙，页面试探的节奏与放弃前的耐心。
const UPDATE_WATCHDOG_INTERVAL_MS = 2000;
const UPDATE_WATCHDOG_TIMEOUT_MS = 90000;

// withUpdateBusy 按动作挑错误文案的前缀，所有 key 都必须在三份语言里存在。
const UPDATE_FAILURE_KEYS = {
    check: 'checkFailed',
    apply: 'applyFailed',
    rollback: 'rollbackFailed',
    load: 'loadFailed',
    source: 'sourceSaveFailed',
    adopt: 'adoptFailed',
    restart: 'restartFailed',
};

let updateConsoleState = null;
let updateCheck = { done: false, error: '' };
let updateBusy = false;          // 页面自己发着的动作请求（检查 / 启动更新 / 回滚 / 重启）
let updateChecking = false;      // 只影响「检查更新」按钮的措辞
let updatePollTimer = null;
let updatePollInFlight = null;
let updateRestartExpected = false;  // 更新结束后预计重启，构建阶段仍照常读进度
let updateRestartPending = false;   // 正在等待服务恢复
let updateWatchdogTimer = null;     // 重启守侧自己的定时器，跟任务进度轮询互不干扰
let updateWatchdogDeadline = 0;
let updateWatchdogSlow = null;      // null=还没超时；'running'=仍由旧进程应答；'down'=服务没回来
let updateAutoCheckDone = false; // 每个会话只自动查一次远端，不每次重绘都去打扰
let updateSourceDraft = null;    // 编辑中的更新源仓库地址；null=还贴着服务端保存的值
let updateSourceError = '';      // 最近一次保存/接入失败的原因，画在区块里
let updateActionError = '';      // 最近一次动作（检查/更新/回滚/重启）失败的原因，画在控制台顶部
let updateAdoptPlan = null;      // 预览回来的接入计划；确认后清掉

function updateT(key, opts) {
    const k = 'update.' + key;
    if (typeof window.t !== 'function') return k;
    // i18next 默认把插值里的 / " ' 也转成 HTML 实体；控制台的结果要么进 escapeHtml（渲染），
    // 要么进 confirm（纯文本），两种去向都不需要它先转义——开着反而会把日期、分支名里的 /
    // 显示成 &#x2F;（实测：devtree/deploy/... 在确认框里就是这么显示的）。
    const merged = Object.assign({ interpolation: { escapeValue: false } }, opts || {});
    return window.t(k, merged);
}

function updateSourceOf() {
    return (updateConsoleState && updateConsoleState.source) || { repo: '', configured: false, defaultRepo: '' };
}

// 草稿优先：轮询重绘不许把正在输入的地址冲掉；保存成功后草稿丢弃、回到服务端的值。
function updateSourceRepo() {
    if (typeof updateSourceDraft === 'string') return updateSourceDraft;
    return updateSourceOf().repo || '';
}

function updateSourceRepoChanged(value) {
    updateSourceDraft = String(value == null ? '' : value);
}

// scheduleUpdateAutoCheck answers the question the page exists to answer, without making
// somebody press a button to find out whether the button would do anything.
//
// The constraints are what keep it polite: one check per session, only for a tree that is
// actually a git installation, never while a job is already running, and always after the
// first paint - the page's own claim has to reach the screen before the network does. A
// failure is not retried here: the page says so plainly, and the operator presses 检查更新
// when they want to ask again.
function scheduleUpdateAutoCheck() {
    if (updateAutoCheckDone) return false;
    const status = updateConsoleState && updateConsoleState.status;
    if (!status || !status.installed || isUpdateJobRunning()) return false;
    updateAutoCheckDone = true;
    setTimeout(() => {
        if (!isUpdateConsoleActive() || isUpdateJobRunning() || updateRestartPending) return;
        checkForUpdates({ quiet: true });
    }, 1200);
    return true;
}

function isUpdateConsoleActive() {
    // 控制台住在系统设置页的「一键更新」分区里：页面激活还不够，分区也得是当前选中的那个。
    const page = document.getElementById('page-settings');
    const section = document.getElementById('settings-section-update');
    return !!(page && page.classList.contains('active') &&
        section && section.classList.contains('active'));
}

function updateStatusOf() {
    return (updateConsoleState && updateConsoleState.status) || {};
}

function updateJobOf() {
    const job = updateConsoleState && updateConsoleState.job;
    return job && typeof job === 'object' ? job : null;
}

function updateCanRestart() {
    return !!(updateConsoleState && updateConsoleState.canRestart);
}

function isUpdateJobRunning() {
    const job = updateJobOf();
    return !!(job && job.state === 'running');
}

function resetUpdateCheck() {
    // 一次 GET 拿不到 behind/updateAvailable（它不联网），留着上一次的 done=true
    // 就会把"没查"画成"已是最新"。
    updateCheck = { done: false, error: '' };
}

// ---------------------------------------------------------------------------
// 请求层
// ---------------------------------------------------------------------------

// 每个调用都要拿到状态码和解析后的体：apply/rollback 的 409 里带着那个正在跑的任务，
// 直接 throw 掉就等于把"已经有一次在跑了，这是它的进度"扔了。
// ok 不等于状态码：rollback 在"源码回了、二进制没回"这种半途情况下是按 202 回答并带着
// error 字段的（handler 里的 statusForError 把 no_toolchain 归到 202），只信 resp.ok
// 就会对着一次没做完的回滚播报成功。
async function runUpdateRequest(method, url, body, request = apiFetch) {
    const resp = await request(url, {
        method: method,
        headers: { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await resp.json().catch(() => ({}));
    const refused = !!(data && typeof data.error === 'string' && data.error);
    return {
        ok: !!resp.ok && !refused,
        status: resp.status,
        data: data || {},
        error: refused ? data.error : ('HTTP ' + resp.status),
    };
}

// A proxy 401 or a dropped connection during the switch must not clear the session.
function fetchDuringUpdate(url, options = {}) {
    const headers = { Accept: 'application/json', ...options.headers };
    if (typeof authToken === 'string' && authToken) headers.Authorization = 'Bearer ' + authToken;
    return fetch(url, {
        ...options,
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        headers,
        signal: typeof AbortSignal !== 'undefined' && AbortSignal.timeout ? AbortSignal.timeout(5000) : undefined,
    });
}

async function fetchUpdateStatus() {
    const r = await runUpdateRequest('GET', '/api/system/update');
    if (!r.ok) throw new Error(r.error);
    updateConsoleState = {
        status: r.data.status || {},
        job: r.data.job || null,
        canRestart: !!r.data.canRestart,
        bootID: r.data.bootID || '',
        needsRestart: !!r.data.needsRestart,
        binaryBuiltAt: r.data.binaryBuiltAt || '',
        source: r.data.source || { repo: '', configured: false, defaultRepo: '' },
    };
    return updateConsoleState;
}

// notify 只在平台真的载了 showNotification 时才动。它绝不能成为"操作看起来失败"的原因：
// 请求已经成功了，为了播报一句去调一个页面上不存在的函数，会把成功炸成失败。
function notify(message, type) {
    if (typeof showNotification === 'function') {
        try {
            showNotification(message, type);
        } catch (e) {
            console.warn('notify', e);
        }
    }
}

// 动作期的重绘必须头尾对称。忙时要画"占用中"（按钮带 disabled），闲时要再画回来——
// 这个页面的检查流程（包括打开页面后的自动检查）会在忙期里重绘，若结束时不再重绘，
// 那份 disabled 就留在 DOM 上把按钮永久冻死：实测打开页面自动检查一次后，「保存更新源」
// 带 disabled + data-state-disabled 留在页面上，再点永远没有反应，连请求都发不出去。
// 横幅改成状态（updateActionError）由 renderUpdateConsole 渲染，退出重绘就不会把它擦掉；
// 重启守侧接管画面时（updateRestartPending）一律不重绘，别把自恢复视图盖回控制台。
async function withUpdateBusy(fn, label) {
    if (updateBusy) return;
    updateBusy = true;
    updateActionError = '';
    if (!updateRestartPending) renderUpdateConsole();
    try {
        await fn();
    } catch (err) {
        const reason = err && err.message ? err.message : String(err);
        // 请求炸在半路上时，画面还停在"正在检查更新源…"这种已经不再成立的状态上，
        // 光加一条红字没用：横幅跟随状态重绘，画面本身也要按当前状态画一遍。
        updateActionError = updateT(UPDATE_FAILURE_KEYS[label] || 'loadFailed', { reason: reason });
        if (!updateRestartPending) renderUpdateConsole();
        // 内联横幅会被下一次动作开始时的重绘冲掉，toast 才是留得住的那一份。
        notify(updateActionError, 'error');
    } finally {
        updateBusy = false;
        if (!updateRestartPending) renderUpdateConsole();
    }
}

// ---------------------------------------------------------------------------
// 入口与轮询
// ---------------------------------------------------------------------------

async function loadUpdateConsole() {
    stopUpdatePolling();
    // 重启已经发起：控制台让位给自恢复视图，不再去读一个正在退出的服务。
    if (updateRestartPending) {
        startUpdateWatchdog();
        return;
    }
    resetUpdateCheck();
    updateActionError = '';     // 重新进页 = 干净画面，上一次动作的红字不跟过来
    const el = document.getElementById('update-console');
    if (!el) return;
    el.innerHTML = '<div class="empty-state">' + escapeHtml(updateT('loading')) + '</div>';
    try {
        await fetchUpdateStatus();
    } catch (err) {
        el.innerHTML = '<div class="empty-state">' +
            escapeHtml(updateT(UPDATE_FAILURE_KEYS.load, { reason: err.message })) + '</div>';
        return;
    }
    renderUpdateConsole();
    const job = updateJobOf();
    updateRestartExpected = !!(job && job.restartRequested && job.state === 'running');
    if (job && job.state === 'succeeded' && job.restartRequested && job.result?.binaryBuilt) {
        startUpdateWatchdog();
        return;
    }
    if (isUpdateJobRunning()) startUpdatePolling();
    scheduleUpdateAutoCheck();
}

// 只有一个定时器：不清就重新 arm，是"页面开两次之后两条轮询往同一份日志里写"的来路。
function startUpdatePolling() {
    stopUpdatePolling();
    updatePollTimer = setInterval(pollUpdateJob, UPDATE_POLL_INTERVAL_MS);
}

function stopUpdatePolling() {
    if (updatePollTimer) clearInterval(updatePollTimer);
    updatePollTimer = null;
}

// ---------------------------------------------------------------------------
// 重启守侧：请求重启之后，盯着"新进程是否已经接客"，接上了就整页刷新
// ---------------------------------------------------------------------------

// 服务启动标记变了才刷新。会话过期和代理的错误页都不代表换版。
// 离开控制台就静默停表：不把已经走开的用户从别的页面拽回更新页。
let updateWatchdogProbing = false;

async function watchdogProbe() {
    if (!isUpdateConsoleActive()) {
        stopUpdateWatchdog();
        return;
    }
    if (updateWatchdogProbing) return;
    updateWatchdogProbing = true;
    let status = 0;
    let bootID = '';
    let legacyUnauthorized = false;
    const previousBoot = updateConsoleState && updateConsoleState.bootID;
    try {
        const resp = await fetchDuringUpdate('/api/auth/validate');
        status = resp.status;
        bootID = resp.headers?.get('X-CyberStrike-Boot-ID') || '';
        if (status === 401 && !bootID) {
            // A downgrade may return to a build that predates the boot header.
            const data = await resp.json().catch(() => null);
            legacyUnauthorized = data?.error === '未授权访问，请先登录';
        }
    } catch (err) {
        status = 0;
    } finally {
        updateWatchdogProbing = false;
    }
    if (!isUpdateConsoleActive() || !updateWatchdogTimer) return;
    if ((bootID && bootID !== previousBoot && (status === 200 || status === 401)) || legacyUnauthorized) {
        stopUpdateWatchdog();
        const params = new URLSearchParams(location.search);
        params.set('restarted', Date.now());
        location.replace(location.pathname + '?' + params + '#system-update');
        return;
    }
    if (updateRestartExpected && previousBoot && bootID === previousBoot && status === 200) {
        // The progress request may have hit a transient network error during the build.
        stopUpdateWatchdog();
        updateRestartPending = false;
        renderUpdateConsole();
        startUpdatePolling();
        return;
    }
    if (updateWatchdogSlow === null && Date.now() > updateWatchdogDeadline) {
        updateWatchdogSlow = bootID || status === 200 ? 'running' : 'down';
        renderUpdateWatchdog();
    }
}

function startUpdateWatchdog() {
    if (updateWatchdogTimer) return;
    updateRestartPending = true;
    updateWatchdogSlow = null;
    updateWatchdogDeadline = Date.now() + UPDATE_WATCHDOG_TIMEOUT_MS;
    renderUpdateWatchdog();
    updateWatchdogTimer = setInterval(watchdogProbe, UPDATE_WATCHDOG_INTERVAL_MS);
}

function stopUpdateWatchdog() {
    if (updateWatchdogTimer) clearInterval(updateWatchdogTimer);
    updateWatchdogTimer = null;
}

async function pollUpdateJob() {
    if (!isUpdateConsoleActive()) {
        // 页面已经切走了。router.js 只在进入时调 initPage，离开时没人喊，
        // 所以这里自己停，不留野定时器。
        stopUpdatePolling();
        return;
    }
    const timer = updatePollTimer;
    if (!timer || updatePollInFlight === timer) return;
    updatePollInFlight = timer;
    // 传输层死亡（连接被拒、进程已经退出）是 reject 而不是带状态码的回应：两者在这里
    // 归一——重启进行中它就是"进程退了"，否则才是要播报的读取失败。
    let r;
    try {
        r = await runUpdateRequest('GET', '/api/system/update/job', undefined,
            updateRestartExpected ? fetchDuringUpdate : apiFetch);
    } catch (err) {
        r = { ok: false, error: err && err.message ? err.message : String(err) };
    } finally {
        if (updatePollInFlight === timer) updatePollInFlight = null;
    }
    if (timer !== updatePollTimer || !isUpdateConsoleActive()) return;
    if (!r.ok) {
        stopUpdatePolling();
        if (updateRestartExpected) {
            // The page may have missed the last progress response before handoff.
            startUpdateWatchdog();
            return;
        }
        notify(updateT('jobPollFailed', { reason: r.error }), 'error');
        return;
    }
    const job = r.data && r.data.job ? r.data.job : null;
    if (updateConsoleState) updateConsoleState.job = job;
    renderUpdateConsole();
    if (job && job.state === 'running') updateRestartExpected = !!job.restartRequested;
    if (!job && updateRestartExpected) {
        stopUpdatePolling();
        startUpdateWatchdog();
        return;
    }
    if (job && job.state !== 'running') {
        stopUpdatePolling();
        updateRestartExpected = false;
        if (job.state === 'succeeded' && job.restartRequested && job.result?.binaryBuilt) {
            startUpdateWatchdog();
            return;
        }
        updateRestartPending = false;
        reportFinishedJob(job);
    }
}

// 完成时的这一句只念服务端写下来的结论，但补编译与普通更新是两种完成：commits=0 却换了
// 二进制时念"a1b2c3d → a1b2c3d"等于没说，得说"补编译完成"。
function updateDoneToast(result) {
    if (result && result.binaryBuilt && result.fromCommit && result.fromCommit === result.toCommit) {
        return updateT('buildDoneToast');
    }
    const tail = result && result.toCommit ? ' ' + result.fromCommit + ' → ' + result.toCommit : '';
    return updateT('jobDoneToast') + tail;
}

// 播报只念服务端写下来的结论：成功念 result 的两个提交号，失败念 failure.message，
// 页面自己不在这里编"更新完成"。
function reportFinishedJob(job) {
    const result = job.result || null;
    if (job.state === 'succeeded') {
        notify(updateDoneToast(result), 'success');
        if (job.restartError) {
            updateActionError = updateT('restartFailed', { reason: job.restartError });
            notify(updateActionError, 'error');
        } else if (result && result.needsRestart) notify(updateT('needsRestartToast'), 'info');
    } else {
        const reason = (job.failure && job.failure.message) ? job.failure.message : updateT('failureNoMessage');
        notify(updateT('jobFailedToast') + ': ' + reason, 'error');
    }
    resetUpdateCheck();
    // 二进制换过了，本机状态得重读一遍；读不到就照实说读不到。
    fetchUpdateStatus().then(renderUpdateConsole).catch(err => {
        notify(updateT(UPDATE_FAILURE_KEYS.load, { reason: err.message }), 'error');
    });
}

// ---------------------------------------------------------------------------
// 动作
// ---------------------------------------------------------------------------

async function checkForUpdates({ quiet = false } = {}) {
    await withUpdateBusy(async () => {
        updateChecking = true;
        renderUpdateConsole();
        let r;
        try {
            r = await runUpdateRequest('POST', '/api/system/update/check');
        } finally {
            // 请求本身炸掉（401、网络断了）也必须把"正在检查"放下来，否则按钮永远灰着。
            updateChecking = false;
        }
        if (!r.ok) {
            // HTTP 层的失败同样是"查不了"，不能留下一个看起来像"没有更新"的页面。
            updateCheck = { done: false, error: r.error };
            renderUpdateConsole();
            notify(updateT(UPDATE_FAILURE_KEYS.check, { reason: r.error }), 'error');
            return;
        }
        const status = r.data.status || {};
        if (updateConsoleState) updateConsoleState.status = status;
        updateCheck = { done: true, error: status.checkError || '' };
        renderUpdateConsole();
        if (updateCheck.error) {
            notify(updateT(UPDATE_FAILURE_KEYS.check, { reason: updateCheck.error }), 'error');
        } else if (quiet) {
            return;
        } else if (status.updateAvailable) {
            notify(updateT('behindCount', { n: updateBehindCount(status) }), 'info');
        } else {
            notify(updateT('upToDate', { commit: status.commit || '-' }), 'success');
        }
    }, 'check');
}

function updateBehindCount(status) {
    const listed = (status.incoming || []).length;
    const total = status.incomingTotal || status.behind || 0;
    return total > 0 ? total : listed;
}

// 按钮为什么点不动，得说得出原因；"灰着"不算解释。
function updateApplyBlockers(status, job) {
    const blockers = [];
    if (status.rollbackPending) blockers.push(updateT('rollbackPendingHint'));
    if (!status.installed) blockers.push(updateT('blockedNotGitTree'));
    if (job && job.state === 'running') blockers.push(updateT('blockedJobRunning'));
    if ((status.blockingChanges || []).length) {
        blockers.push(updateT('blockedLocalEdits', { n: status.blockingChanges.length }));
    }
    if (status.diverged) {
        blockers.push(updateT('blockedDiverged', {
            ahead: status.ahead || 0,
            behind: status.behind || 0,
        }));
    }
    if (!status.updateAvailable && !status.buildPending) blockers.push(updateT('blockedNoUpdate'));
    return blockers;
}

async function startUpdateApply() {
    if (updateBusy) return;
    const status = updateStatusOf();
    const blockers = updateApplyBlockers(status, updateJobOf());
    if (blockers.length) {
        // 浏览器不会把 disabled 按钮的点击送到这里，走到这儿说明状态已经和画面不一致了，
        // 那就照实说为什么不能点，一声不吭更像坏了。
        notify(blockers[0], 'error');
        return;
    }
    const restart = updateCanRestart();

    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/apply', { restart: restart });
        if (r.status === 409 && r.data.job) {
            // 另一次更新真的在跑：把它的进度接下来并接着看，这才是"当前状态"。
            if (updateConsoleState) updateConsoleState.job = r.data.job;
            updateRestartExpected = !!r.data.job.restartRequested;
            renderUpdateConsole();
            startUpdatePolling();
        }
        if (!r.ok) throw new Error(r.error);
        resetUpdateCheck();
        updateRestartExpected = restart;
        startUpdatePolling();
        await pollUpdateJob();
    }, 'apply');
}

// 「立即重启服务」：把磁盘上已经换好的二进制变成正在应答的那一个。服务端只允许在
// 确实有待生效的版本时放行——没有可生效的东西就点停服务，那不叫重启。
async function startRestartNow() {
    if (updateBusy) return;
    if (!window.confirm(updateT('restartNowConfirm'))) return;

    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/restart');
        if (!r.ok) throw new Error(r.error);
        stopUpdatePolling();
        updateRestartExpected = false;
        notify(updateT('restartRequestedToast'), 'info');
        startUpdateWatchdog();
    }, 'restart');
}

async function rollbackUpdate() {
    if (updateBusy) return;
    const status = updateStatusOf();
    if (!status.hasRollback) return;
    if (isUpdateJobRunning()) {
        notify(updateT('blockedJobRunning'), 'error');
        return;
    }
    if (!window.confirm(updateT('confirmRollback', { commit: status.rollbackTo || '-' }))) return;

    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/rollback');
        if (!r.ok) {
            // 服务端连"哪些文件没回滚"都给了，不能只报一句失败。
            const items = r.data && r.data.items && r.data.items.length
                ? ' · ' + updateT('rollbackItems') + ': ' + r.data.items.join(', ')
                : '';
            throw new Error(r.error + items);
        }
        const to = (r.data.result && r.data.result.toCommit) || status.rollbackTo || '-';
        resetUpdateCheck();
        updateRestartChoice = false;
        await fetchUpdateStatus();
        renderUpdateConsole();
        notify(updateT('rollbackDone', { commit: to }), 'success');
    }, 'rollback');
}

// 更新源：服务端负责校验与写入（config.yaml update 段的单个 repo 字段），这里只做搬运与说明。
// 留空提交 = 回到官方仓库，和"从没配过"是同一种状态。
async function saveUpdateSource() {
    if (updateBusy) return;
    const body = {
        repo: updateSourceRepo().trim(),
    };
    updateSourceError = '';
    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/source', body);
        if (!r.ok) {
            // 服务端的拒绝文案（地址不合法……）就是要展示的那句，不在这里另编。
            updateSourceError = updateT('sourceSaveFailed', { reason: r.error });
            renderUpdateConsole();
            notify(updateSourceError, 'error');
            return;
        }
        updateSourceDraft = null;
        if (updateConsoleState) updateConsoleState.source = r.data.source || updateConsoleState.source;
        resetUpdateCheck();
        await fetchUpdateStatus();
        renderUpdateConsole();
        notify(updateT('sourceSaveOk'), 'success');
    }, 'source');
}

// 预览无副作用（服务端在临时仓库里 fetch），所以可以先点、先看清单再决定。
async function previewAdoptSource() {
    if (updateBusy) return;
    updateSourceError = '';
    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/adopt', { confirm: false });
        if (!r.ok) {
            updateSourceError = updateT('adoptFailed', { reason: r.error });
            renderUpdateConsole();
            notify(updateSourceError, 'error');
            return;
        }
        updateAdoptPlan = r.data.plan || null;
        renderUpdateConsole();
    }, 'adopt');
}

async function confirmAdoptSource() {
    if (updateBusy || !updateAdoptPlan) return;
    const restart = updateCanRestart();
    const text = updateT('adoptConfirmText', {
        incoming: updateAdoptPlan.incoming,
        overwritten: updateAdoptPlan.overwrittenTotal,
        protectedFiles: updateAdoptPlan.protectedTotal,
    });
    if (!window.confirm(text)) return;

    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/adopt', { confirm: true, restart: restart });
        if (!r.ok) throw new Error(r.error);
        updateAdoptPlan = null;
        resetUpdateCheck();
        updateRestartExpected = restart;
        startUpdatePolling();
        notify(updateT('adoptAccepted', { id: r.data.job_id || '-' }), 'success');
        await pollUpdateJob();
    }, 'adopt');
}

// ---------------------------------------------------------------------------
// 渲染
// ---------------------------------------------------------------------------

function updateSection(title, body) {
    return '<section class="update-section"><h3 class="update-section-title">' +
        escapeHtml(title) + '</h3>' + body + '</section>';
}

// 值既进文本也进 title（长路径要靠 title 看全），所以属性那一份必须走 escapeAttr。
function updateFact(label, value) {
    const text = value == null || value === '' ? '-' : String(value);
    return '<div class="update-fact"><span>' + escapeHtml(label) + '</span>' +
        '<code title="' + escapeAttr(text) + '">' + escapeHtml(text) + '</code></div>';
}

function updateMark(ok, yesKey, noKey, reason) {
    const cls = ok ? 'update-chip update-chip-ok' : 'update-chip update-chip-warn';
    if (!reason) {
        return '<span class="' + cls + '">' + escapeHtml(ok ? updateT(yesKey) : updateT(noKey)) + '</span>';
    }
    return '<span class="' + cls + '" title="' + escapeAttr(String(reason)) + '">' +
        escapeHtml(ok ? updateT(yesKey) : updateT(noKey)) + '</span>';
}

function renderUpdateSourceBody(status) {
    const src = updateSourceOf();
    const defaultRepo = src.defaultRepo || '';
    const parts = [];
    parts.push('<div class="update-hint">' + escapeHtml(src.configured ? updateT('sourceFromConfig') : updateT('sourceIsDefault', { repo: defaultRepo })) + '</div>');
    if (status.repo) {
        parts.push('<div class="update-facts">' + updateFact(updateT('sourceUrl'), status.repo) + '</div>');
    }
    // 手输错误属于页面自己的状态，重绘（轮询）不能把它冲掉。
    const disabled = updateBusy ? ' disabled data-state-disabled="true"' : '';
    parts.push('<div class="update-source-form">' +
        '<label>' + escapeHtml(updateT('sourceUrlLabel')) +
        '<input type="text" class="update-source-input update-source-url" value="' + escapeAttr(updateSourceRepo()) +
        '" placeholder="' + escapeAttr(defaultRepo) + '" oninput="updateSourceRepoChanged(this.value)"></label>' +
        '<div class="update-hint">' + escapeHtml(updateT('sourceRepoHint', { repo: defaultRepo })) + '</div>' +
        '<div class="update-actions"><button class="btn-secondary update-source-save-btn"' + disabled +
        ' data-require-permission="update:apply" onclick="saveUpdateSource()">' +
        escapeHtml(updateT('sourceSaveBtn')) + '</button></div></div>');
    if (updateSourceError) {
        parts.push('<div class="update-error">' + escapeHtml(updateSourceError) + '</div>');
    }
    if (!status.installed) {
        parts.push(renderAdoptBody());
    }
    return parts.join('');
}

function renderAdoptBody() {
    const parts = [];
    parts.push('<div class="update-hint">' + escapeHtml(updateT('adoptHint')) + '</div>');
    const disabled = updateBusy ? ' disabled data-state-disabled="true"' : '';
    if (!updateAdoptPlan) {
        parts.push('<div class="update-actions"><button class="btn-primary update-adopt-preview-btn"' + disabled +
            ' data-require-permission="update:apply" onclick="previewAdoptSource()">' +
            escapeHtml(updateT('adoptPreviewBtn')) + '</button></div>');
        return parts.join('');
    }
    const plan = updateAdoptPlan;
    parts.push('<div class="update-facts">' +
        updateFact(updateT('adoptIncoming'), plan.incoming) +
        updateFact(updateT('adoptOverwrittenTotal'), plan.overwrittenTotal) +
        updateFact(updateT('adoptProtectedTotal'), plan.protectedTotal) +
        updateFact(updateT('sourceBranch'), plan.branch) +
        updateFact(updateT('adoptCommit'), plan.commit) + '</div>');
    const list = (items, title) => items && items.length
        ? '<h4 class="update-sub-title">' + escapeHtml(title) + '</h4><ul class="update-commit-list">' +
          items.map(p => '<li><code title="' + escapeAttr(String(p)) + '">' + escapeHtml(String(p)) + '</code></li>').join('') + '</ul>'
        : '';
    parts.push(list(plan.overwritten, updateT('adoptOverwrittenTitle')));
    parts.push(list(plan.protected, updateT('adoptProtectedTitle')));
    parts.push('<div class="update-hint">' + escapeHtml(updateT('adoptBackupNote')) + '</div>');
    parts.push(renderUpdateSwitchHint());
    parts.push('<div class="update-actions"><button class="btn-primary update-adopt-confirm-btn"' + disabled +
        ' data-require-permission="update:apply" onclick="confirmAdoptSource()">' +
        escapeHtml(updateT('adoptConfirmBtn')) + '</button></div>');
    return parts.join('');
}

function renderUpdateInstallBody(status) {
    const facts = [
        updateFact(updateT('root'), status.root),
        updateFact(updateT('branch'), status.branch),
        updateFact(updateT('commit'), status.commit),
        updateFact(updateT('committedAt'), status.committedAt),
        updateFact(updateT('subject'), status.subject),
    ].join('');
    const marks = [
        updateMark(!!status.installed, 'gitTree', 'notGitTree'),
        updateMark(!!status.canBuild, 'toolchainFound', 'toolchainMissing',
            status.canBuild ? (status.goToolchain || '') : (status.goToolchain || updateT('toolchainMissingHint'))),
        updateMark(!!status.hasBinary, 'binaryPresent', 'binaryMissing'),
        updateMark(!!status.hasRollback, 'rollbackPresent', 'rollbackMissing'),
    ];
    if (updateConsoleState && updateConsoleState.needsRestart) {
        marks.push('<span class="update-chip update-chip-warn">' + escapeHtml(updateT('restartNeeded')) + '</span>');
    }
    return '<div class="update-facts">' + facts + '</div><div class="update-marks">' + marks.join('') + '</div>';
}

function renderUpdateRemoteBody(status) {
    const failure = updateCheck.error || status.checkError || '';
    const parts = [];
    if (failure) {
        // 这条分支就是"不许说谎"的落点：有原因就只写原因，不提"已是最新"。
        parts.push('<div class="update-error update-check-failed">' +
            escapeHtml(updateT('checkFailed', { reason: failure })) + '</div>');
    } else if (!updateCheck.done) {
        parts.push('<div class="update-hint">' + escapeHtml(updateT('notChecked')) + '</div>');
    } else if (status.updateAvailable) {
        parts.push('<div class="update-behind">' +
            escapeHtml(updateT('behindCount', { n: updateBehindCount(status) })) + '</div>');
    } else {
        parts.push('<div class="update-ok">' +
            escapeHtml(updateT('upToDate', { commit: status.commit || '-' })) + '</div>');
    }

    if (status.ahead > 0) {
        parts.push('<div class="update-warn">' + escapeHtml(updateT('aheadCount', { n: status.ahead })) + '</div>');
    }
    if (status.targetBranch || status.remoteCommit) {
        parts.push('<div class="update-facts">' +
            updateFact(updateT('sourceBranch'), status.targetBranch) +
            updateFact(updateT('remoteHead'), status.remoteCommit) +
            updateFact(updateT('remoteSubject'), status.remoteSubject) + '</div>');
    }

    const incoming = status.incoming || [];
    if (incoming.length) {
        // incoming 是被截断过的清单，incomingTotal 才是真实数量，两个都得说。
        const total = status.incomingTotal || incoming.length;
        parts.push('<h4 class="update-sub-title">' + escapeHtml(updateT('incomingTitle')) + '</h4>');
        if (total > incoming.length) {
            parts.push('<div class="update-hint">' +
                escapeHtml(updateT('incomingTruncated', { total: total, shown: incoming.length })) + '</div>');
        }
        parts.push('<ul class="update-commit-list">' + incoming.map(c =>
            '<li><code title="' + escapeAttr(String(c.commit || '-')) + '">' + escapeHtml(String(c.commit || '-')) +
            '</code> <span class="update-commit-subject">' + escapeHtml(String(c.subject || '')) + '</span></li>'
        ).join('') + '</ul>');
    }
    return parts.join('');
}

function renderUpdateChangesTable(list) {
    // 行底色就是"这个文件会不会被更新覆盖"的分区：本机内容绿、产品源码红。
    const rows = (list || []).map(c => '<tr class="' + (c.protected ? 'update-row-kept' : 'update-row-blocking') + '">' +
        '<td><code title="' + escapeAttr(String(c.path || '')) + '">' + escapeHtml(String(c.path || '-')) + '</code></td>' +
        '<td>' + escapeHtml(String(c.status || '-')) + '</td>' +
        '<td>' + escapeHtml(c.protected ? updateT('keptFile') : updateT('productSource')) + '</td></tr>').join('');
    return '<table class="update-table"><thead><tr>' +
        '<th>' + escapeHtml(updateT('colPath')) + '</th>' +
        '<th>' + escapeHtml(updateT('colChangeStatus')) + '</th>' +
        '<th>' + escapeHtml(updateT('colOwner')) + '</th>' +
        '</tr></thead><tbody>' + rows + '</tbody></table>';
}

function renderUpdateSwitchHint() {
    return '<div class="update-hint">' +
        escapeHtml(updateT(updateCanRestart() ? 'confirmRestartYes' : 'restartNotWired')) + '</div>';
}

function renderUpdateApplyBody(status, job) {
    const blockers = updateApplyBlockers(status, job);
    const blocked = blockers.length > 0;
    const parts = [];

    if (job && job.state === 'running') {
        parts.push('<div class="update-warn">' + escapeHtml(updateT('jobRunningHint')) + '</div>');
    }
    if (!status.canBuild && status.installed) {
        parts.push('<div class="update-warn">' + escapeHtml(updateT('noToolchainWarn')) + '</div>');
    }
    // 源码已经在更新源尖端、只欠一次编译：这不是"没有可用更新"，按钮必须照常可点，而且
    // 得说清楚点它会去补编译——否则 no_toolchain 里那句"再点一次更新"在页面上根本没有落点。
    if (status.rollbackPending) {
        parts.push('<div class="update-warn">' + escapeHtml(updateT('rollbackPendingHint')) + '</div>');
    }
    if (status.buildPending && status.installed) {
        parts.push('<div class="update-warn">' + escapeHtml(updateT('buildPendingHint')) + '</div>');
    }

    // Both halves matter: `disabled` for the browser, `data-state-disabled` so the platform's
    // permission gate (which writes el.disabled for every [data-require-permission] element)
    // does not re-enable the button for a user who holds update:apply. In a real browser that
    // write produced an enabled 一键更新 directly under "有 53 个产品源码文件被本地改过".
    const stateDisabled = blocked ? ' disabled data-state-disabled="true"' : '';
    const checkingDisabled = updateChecking ? ' disabled data-state-disabled="true"' : '';
    parts.push('<div class="update-actions">' +
        '<button class="btn-primary update-apply-btn"' + stateDisabled +
        ' data-require-permission="update:apply" onclick="startUpdateApply()">' +
        escapeHtml(updateT('applyBtn')) + '</button>' +
        '<button class="btn-secondary update-check-btn"' + checkingDisabled +
        ' data-require-permission="update:apply" onclick="checkForUpdates()">' +
        escapeHtml(updateChecking ? updateT('checking') : updateT('checkBtn')) + '</button>' +
        '</div>');

    parts.push(renderUpdateSwitchHint());

    if (blocked) {
        parts.push('<ul class="update-blocker-list">' + blockers.map(b =>
            '<li class="update-error">' + escapeHtml(b) + '</li>').join('') + '</ul>');
    }

    const local = status.localChanges || [];
    const blocking = status.blockingChanges || [];
    // localChanges 通常是 blockingChanges 的超集，但两者万一不一致，阻塞更新的文件绝不能因此
    // 不列出来：上面那句"有 N 个文件被本地改过"点名的就是这些得人去处理的文件名。
    const seenPaths = Object.create(null);
    const listed = local.concat(blocking).filter(c => {
        const p = String((c && c.path) || '');
        if (!p || seenPaths[p]) return false;
        seenPaths[p] = true;
        return true;
    });
    if (listed.length) {
        parts.push('<h4 class="update-sub-title">' + escapeHtml(updateT('localChangesTitle')) + '</h4>');
        parts.push('<div class="update-hint">' + escapeHtml(updateT('localChangesHint')) + '</div>');
        parts.push(renderUpdateChangesTable(listed));
    } else {
        parts.push('<div class="update-hint">' + escapeHtml(updateT('noLocalChanges')) + '</div>');
    }
    return parts.join('');
}

function updateStateChip(state) {
    const known = { running: 'update-chip-running', succeeded: 'update-chip-ok', failed: 'update-chip-danger' };
    const cls = known[state] || 'update-chip';
    const label = state === 'running' ? updateT('stateRunning')
        : state === 'succeeded' ? updateT('stateSucceeded')
            : state === 'failed' ? updateT('stateFailed') : updateT('stateUnknown');
    return '<span class="' + cls + '">' + escapeHtml(label) + '</span>';
}

function renderUpdateStep(step) {
    return '<li class="update-step">' +
        '<code class="update-step-phase">' + escapeHtml(String(step.phase || '-')) + '</code>' +
        '<span class="update-step-message">' + escapeHtml(String(step.message || '')) + '</span>' +
        '<span class="update-step-at">' + escapeHtml(String(step.at || '')) + '</span></li>';
}

function renderUpdateResult(result) {
    if (!result) return '';
    const kept = result.keptContent || [];
    const facts = [
        updateFact(updateT('resultFrom'), result.fromCommit),
        updateFact(updateT('resultTo'), result.toCommit),
        updateFact(updateT('resultCommits'), result.commits),
        updateFact(updateT('resultFiles'), result.filesTouched),
        updateFact(updateT('resultDuration'), result.duration),
        updateFact(updateT('resultBackup'), result.backupDir),
    ].join('');
    const marks = [
        updateMark(!!result.binaryBuilt, 'binarySwapped', 'binaryNotSwapped', result.binaryPath || ''),
        updateMark(!!result.needsRestart, 'restartNeeded', 'restartNotNeeded'),
    ].join('');
    const keptList = kept.length
        ? '<ul class="update-kept-list">' + kept.map(p =>
            '<li><code title="' + escapeAttr(String(p)) + '">' + escapeHtml(String(p)) + '</code></li>').join('') + '</ul>'
        : '<div class="update-hint">' + escapeHtml(updateT('nothingKept')) + '</div>';
    const replaced = result.overwritten || [];
    const replacedList = replaced.length
        ? '<h4 class="update-sub-title">' + escapeHtml(updateT('resultOverwrittenTitle')) + '</h4>' +
          '<ul class="update-kept-list">' + replaced.map(p =>
              '<li><code title="' + escapeAttr(String(p)) + '">' + escapeHtml(String(p)) + '</code></li>').join('') + '</ul>'
        : '';
    const adoptedNote = result.adopted
        ? '<div class="update-hint">' + escapeHtml(updateT('adoptedNoPreviousCommit')) + '</div>'
        : '';
    return '<div class="update-result">' +
        '<h4 class="update-sub-title">' + escapeHtml(updateT('resultTitle')) + '</h4>' +
        '<div class="update-facts">' + facts + '</div><div class="update-marks">' + marks + '</div>' +
        adoptedNote + replacedList +
        '<h4 class="update-sub-title">' + escapeHtml(updateT('keptTitle')) + '</h4>' + keptList +
        '</div>';
}

function renderUpdateFailure(failure) {
    if (!failure) return '';
    const items = failure.items || [];
    const reason = failure.reason ? '（' + escapeHtml(String(failure.reason)) + '）' : '';
    return '<div class="update-error update-failure">' +
        '<div class="update-failure-title">' + escapeHtml(updateT('failureTitle')) + reason + '</div>' +
        '<div class="update-failure-message">' +
        escapeHtml(String(failure.message || updateT('failureNoMessage'))) + '</div>' +
        (items.length
            ? '<ul class="update-item-list">' + items.map(i =>
                '<li><code title="' + escapeAttr(String(i)) + '">' + escapeHtml(String(i)) + '</code></li>').join('') + '</ul>'
            : '') +
        '</div>';
}

function renderUpdateJobBody(job) {
    if (!job) return '<div class="empty-state">' + escapeHtml(updateT('noJob')) + '</div>';
    const parts = ['<div class="update-job-head">' + updateStateChip(job.state) +
        '<code title="' + escapeAttr(String(job.id || '-')) + '">' + escapeHtml(String(job.id || '-')) + '</code>' +
        (job.started ? '<span class="update-job-time">' + escapeHtml(String(job.started)) + '</span>' : '') +
        (job.restartRequested ? '<span class="update-chip">' + escapeHtml(updateT('restartRequested')) + '</span>' : '') +
        '</div>'];
    const steps = job.steps || [];
    parts.push(steps.length
        ? '<ol class="update-step-list">' + steps.map(renderUpdateStep).join('') + '</ol>'
        : '<div class="empty-state">' + escapeHtml(updateT('noSteps')) + '</div>');
    if (job.state === 'running') {
        parts.push('<div class="update-hint">' +
            escapeHtml(updateT('pollingHint', { sec: UPDATE_POLL_INTERVAL_MS / 1000 })) + '</div>');
    }
    if (job.result) parts.push(renderUpdateResult(job.result));
    if (job.failure) parts.push(renderUpdateFailure(job.failure));
    return parts.join('');
}

function renderUpdateRollbackBody(status) {
    if (!status.hasRollback) {
        return '<div class="empty-state">' + escapeHtml(updateT('noRollback')) + '</div>';
    }
    const running = isUpdateJobRunning();
    return '<p class="update-hint">' +
        escapeHtml(updateT('rollbackTarget', { commit: status.rollbackTo || '-' })) + '</p>' +
        '<div class="update-actions"><button class="btn-secondary update-rollback-btn"' +
        (running ? ' disabled data-state-disabled="true"' : '') + ' data-require-permission="update:apply" ' +
        'onclick="rollbackUpdate()">' + escapeHtml(updateT('rollbackBtn')) + '</button></div>';
}

// 待生效横幅：磁盘上的二进制已经不是这个进程了（更新/回滚跑了但没重启，或 CLI 换过），
// 这里给出一个"现在就让它生效"的出口——否则忘勾一次就只剩命令行一条路。
function formatBinaryBuiltAt() {
    const raw = (updateConsoleState && updateConsoleState.binaryBuiltAt) || '';
    if (!raw) return '-';
    const d = new Date(raw);
    return isNaN(d.getTime()) ? raw : d.toLocaleString();
}

function renderRestartBanner() {
    const status = updateStatusOf();
    const commit = status.commit || '';
    const text = updateT(commit ? 'restartBanner' : 'restartBannerNoCommit', {
        commit: commit || '-',
        builtAt: formatBinaryBuiltAt(),
    });
    return '<div class="update-restart-banner">' +
        '<div class="update-restart-banner-text">' + escapeHtml(text) + '</div>' +
        (updateCanRestart() ?
            '<button class="btn-primary update-restart-btn" data-require-permission="update:apply" onclick="startRestartNow()">' +
            escapeHtml(updateT('restartNowBtn')) + '</button>' : '') +
        '</div>';
}

// 自恢复视图：重启请求发出后控制台画的就是它，直到新进程应答触发整页刷新。
function renderUpdateWatchdog() {
    const el = document.getElementById('update-console');
    if (!el) return;
    const parts = ['<div class="update-overlay">',
        '<div class="update-overlay-spinner" aria-hidden="true"></div>',
        '<div class="update-overlay-title">' + escapeHtml(updateT('restartOverlayTitle')) + '</div>',
        '<div class="update-overlay-body">' + escapeHtml(updateT('restartOverlayBody')) + '</div>'];
    if (updateWatchdogSlow) {
        parts.push('<div class="update-overlay-slow">' + escapeHtml(updateT(
            updateWatchdogSlow === 'running' ? 'restartOverlaySlowRunning' : 'restartOverlaySlowDown')) + '</div>');
        parts.push('<div class="update-overlay-actions">' +
            '<button class="btn-secondary" type="button" onclick="refreshUpdatePage()">' +
            escapeHtml(updateT('restartOverlayRefresh')) + '</button></div>');
    }
    parts.push('</div>');
    el.innerHTML = parts.join('');
}

function refreshUpdatePage() {
    location.reload();
}

function renderUpdateConsole() {
    const el = document.getElementById('update-console');
    if (!el || !updateConsoleState) return;
    const status = updateStatusOf();
    const job = updateJobOf();

    const parts = [];
    // 动作失败的红字是状态而不是一次性注入：重绘（轮询、退出忙态的那一次）都不能把它擦掉。
    if (updateActionError) {
        parts.push('<div class="update-error">' + escapeHtml(updateActionError) + '</div>');
    }
    if (updateConsoleState.needsRestart) parts.push(renderRestartBanner());
    parts.push(updateSection(updateT('installTitle'), renderUpdateInstallBody(status)));
    parts.push(updateSection(updateT('sourceTitle'), renderUpdateSourceBody(status)));
    parts.push(updateSection(updateT('remoteTitle'), renderUpdateRemoteBody(status)));
    parts.push(updateSection(updateT('applyTitle'), renderUpdateApplyBody(status, job)));
    parts.push(updateSection(updateT('jobTitle'), renderUpdateJobBody(job)));
    parts.push(updateSection(updateT('rollbackTitle'), renderUpdateRollbackBody(status)));

    el.innerHTML = parts.join('');
    if (typeof window.applyRBACToUI === 'function') {
        window.applyRBACToUI(el);
    }
}

window.loadUpdateConsole = loadUpdateConsole;
window.checkForUpdates = checkForUpdates;
window.startUpdateApply = startUpdateApply;
window.startRestartNow = startRestartNow;
window.refreshUpdatePage = refreshUpdatePage;
window.rollbackUpdate = rollbackUpdate;
window.stopUpdatePolling = stopUpdatePolling;
window.renderUpdateConsole = renderUpdateConsole;
window.saveUpdateSource = saveUpdateSource;
window.previewAdoptSource = previewAdoptSource;
window.confirmAdoptSource = confirmAdoptSource;
window.updateSourceRepoChanged = updateSourceRepoChanged;
