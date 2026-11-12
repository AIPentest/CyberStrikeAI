const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');
const assert = require('node:assert/strict');

const source = fs.readFileSync('web/static/js/update.js', 'utf8');
const template = fs.readFileSync('web/templates/index.html', 'utf8');
const router = fs.readFileSync('web/static/js/router.js', 'utf8');
const settings = fs.readFileSync('web/static/js/settings.js', 'utf8');
const sheet = fs.readFileSync('web/static/css/style.css', 'utf8');
const zh = JSON.parse(fs.readFileSync('web/static/i18n/zh-CN.json', 'utf8'));
const en = JSON.parse(fs.readFileSync('web/static/i18n/en-US.json', 'utf8'));
const ru = JSON.parse(fs.readFileSync('web/static/i18n/ru-RU.json', 'utf8'));

// The only handlers this console is allowed to wire into markup.
const HANDLES = ['startUpdateApply', 'checkForUpdates', 'rollbackUpdate',
    'updateRestartChoiceChanged', 'loadUpdateConsole',
    'saveUpdateSource', 'previewAdoptSource', 'confirmAdoptSource',
    'startRestartNow', 'refreshUpdatePage'];

// The envelope shape GET /api/system/update answers with, plus what the source endpoints return.
const officialRepo = 'https://github.com/AIPentest/CyberStrikeAI.git';
const emptySource = { repo: '', configured: false, defaultRepo: officialRepo };
const configuredSource = { repo: 'https://github.com/Sycun/CyberStrikeAI.git', configured: true, defaultRepo: officialRepo };
const adoptPlan = {
    root: '/srv/csai',
    source: 'https://github.com/Sycun/CyberStrikeAI.git',
    branch: 'main',
    commit: 'abc1234',
    subject: 'target head',
    incoming: 42,
    overwrittenTotal: 2,
    overwritten: ['web/static/js/a.js', 'web/static/js/b.js'],
    protectedTotal: 1,
    protected: ['roles/我的角色.yaml'],
};

function flatKeys(obj, prefix, out) {
    Object.keys(obj).forEach(k => {
        const v = obj[k];
        const path = prefix ? prefix + '.' + k : k;
        if (v && typeof v === 'object') flatKeys(v, path, out);
        else out.add(path);
    });
    return out;
}

function resolve(dict, dotted) {
    let node = dict;
    for (const part of dotted.split('.')) {
        if (!node || typeof node !== 'object') return undefined;
        node = node[part];
    }
    return typeof node === 'string' ? node : undefined;
}

const baseStatus = {
    root: '/srv/csai',
    installed: true,
    branch: 'main',
    repo: officialRepo,
    repoConfigured: false,
    targetBranch: 'main',
    commit: 'a1b2c3d',
    subject: 'fix: 修好一个东西',
    committedAt: '2026-09-30 18:04:11 +0800',
    behind: 3,
    ahead: 0,
    diverged: false,
    updateAvailable: true,
    remoteCommit: 'f9e8d7c',
    remoteSubject: 'feat: 新东西',
    incoming: [{ commit: 'f9e8d7c', subject: 'feat: 新东西' }],
    incomingTotal: 3,
    localChanges: [],
    blockingChanges: [],
    goToolchain: 'go1.23.4',
    canBuild: true,
    hasBinary: true,
    hasRollback: true,
    rollbackTo: 'a1b2c3d',
    checkError: '',
};

function statusOf(patch) {
    return Object.assign({}, baseStatus, patch || {});
}

// The shape is the endpoint's own: {status, job, canRestart, supervised, needsRestart, source}.
// job is null until an update ran.
function envelope(status, job, canRestart, source, flags) {
    return {
        status: status || statusOf(),
        job: job === undefined ? null : job,
        canRestart: canRestart !== false,
        supervised: !!(flags && flags.supervised),
        needsRestart: !!(flags && flags.needsRestart),
        binaryBuiltAt: (flags && flags.binaryBuiltAt) || '',
        source: source || emptySource,
    };
}

const runningJob = {
    id: 'upd-1',
    state: 'running',
    started: '2026-10-01T04:00:00+08:00',
    steps: [{ phase: 'fetch', message: '开始拉取更新源（默认分支 main）', at: '2026-10-01T04:00:01+08:00' }],
    restartRequested: false,
};

const succeededJob = {
    id: 'upd-1',
    state: 'succeeded',
    started: '2026-10-01T04:00:00+08:00',
    finished: '2026-10-01T04:03:12+08:00',
    steps: [
        { phase: 'fetch', message: officialRepo + '（默认分支 main）有 3 个新提交：a1b2c3d → f9e8d7c', at: '04:00:01' },
        { phase: 'protect', message: '已暂存 2 个本地内容文件', at: '04:00:05' },
        { phase: 'build', message: '开始编译二进制', at: '04:00:09' },
        { phase: 'done', message: '已更新 3 个提交并换好二进制', at: '04:03:10' },
    ],
    result: {
        fromCommit: 'a1b2c3d',
        toCommit: 'f9e8d7c',
        commits: 3,
        filesTouched: 27,
        keptContent: ['roles/我的角色.md', 'tools/semgrep.yaml'],
        binaryPath: '/srv/csai/cyberstrike-ai',
        binaryBuilt: true,
        prevBinary: '/srv/csai/cyberstrike-ai.prev',
        backupDir: '/srv/csai/.update-backup/20261001_040005',
        needsRestart: true,
        duration: '3m12s',
    },
    restartRequested: true,
};

const failedJob = {
    id: 'upd-2',
    state: 'failed',
    started: '2026-10-01T05:00:00+08:00',
    finished: '2026-10-01T05:00:40+08:00',
    steps: [{ phase: 'build', message: '开始编译二进制（首次会下载依赖，可能需要几分钟）', at: '05:00:20' }],
    failure: {
        reason: 'build_failed',
        message: '编译失败，二进制保持原版本:\ninternal/foo.go:12: undefined: bar',
        items: ['roles/我的角色.md', 'internal/foo.go'],
    },
    restartRequested: false,
};

// harness() loads the page for real (loadUpdateConsole) before handing it back, so every test
// starts from the markup that would be on screen and can execute the handlers that markup emits.
function harness(options) {
    const opts = options || {};
    const nodes = new Map();
    const calls = [];
    const toasts = [];
    const confirms = [];
    const timers = [];
    let active = true;
    let confirmAnswer = opts.confirm === false ? false : true;
    let nextTimerId = 1;

    function makeEl(id) {
        const el = {
            id,
            innerHTML: '',
            checked: false,
            insertAdjacentHTML(_pos, html) { this.innerHTML = html + this.innerHTML; },
        };
        // 控制台住在系统设置页的「一键更新」分区里：页面和分区都得是 active，它才算在前台。
        let seeded = ['page'];
        if (id === 'page-settings') seeded = ['page'].concat(active ? ['active'] : []);
        if (id === 'settings-section-update') seeded = ['settings-section-content'].concat(active ? ['active'] : []);
        el.classList = {
            _set: new Set(seeded),
            contains(c) { return this._set.has(c); },
            add(c) { this._set.add(c); },
            remove(c) { this._set.delete(c); },
        };
        return el;
    }

    const responses = Object.assign({
        'GET /api/system/update': [{ status: 200, body: envelope(opts.status, opts.job, opts.canRestart, opts.source, opts.flags) }],
        'POST /api/system/update/check': [{ status: 200, body: { status: statusOf(opts.checkedStatus) } }],
        'POST /api/system/update/apply': [{ status: 202, body: { job_id: 'upd-1', state: 'running' } }],
        'GET /api/system/update/job': [{ status: 200, body: { job: opts.job || runningJob } }],
        'POST /api/system/update/rollback': [{ status: 200, body: { result: { fromCommit: 'f9e8d7c', toCommit: 'a1b2c3d' } } }],
        'POST /api/system/update/source': [{ status: 200, body: { source: opts.source || emptySource } }],
        'POST /api/system/update/adopt': [{ status: 200, body: { plan: adoptPlan } }],
    }, opts.responses || {});

    // The watchdog probes with plain fetch (the app's apiFetch turns a 401 into "logged out",
    // and the whole point here is to read that 401 as "the new process is up"). Tests script
    // the answers: 'ok' (old process still answering), 'unauthorized' (a new process without
    // our session), 'down' (nothing is listening), 'bad gateway'.
    const probeQueue = (opts.probes || ['ok']).slice();
    const fetchProbes = [];

    const sandbox = {
        document: {
            getElementById(id) {
                if (!nodes.has(id)) nodes.set(id, makeEl(id));
                return nodes.get(id);
            },
            createElement() {
                const el = { textContent: '', innerHTML: '' };
                Object.defineProperty(el, 'textContent', {
                    set(v) {
                        el._t = String(v == null ? '' : v);
                        // The browser's textContent -> innerHTML leaves quotes alone; a harness that
                        // escaped them would make the attribute-injection assertions test nothing.
                        el.innerHTML = el._t.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
                    },
                    get() { return el._t; },
                });
                return el;
            },
            querySelectorAll() { return []; },
        },
        console,
        Promise,
        JSON,
        Set,
        Object,
        String,
        Date,
        Math,
        escapeHtml(text) {
            const div = sandbox.document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        },
        escapeAttr(text) {
            return sandbox.escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
        },
        showNotification(msg, type) { toasts.push({ msg, type }); },
        setInterval(fn, ms) {
            const t = { id: nextTimerId++, fn, ms, cleared: false };
            timers.push(t);
            return t.id;
        },
        clearInterval(id) {
            const t = timers.find(x => x.id === id);
            if (t) t.cleared = true;
        },
        // The page schedules one deferred remote check; a harness without setTimeout would make
        // that code path untestable rather than absent, so the browser global is provided and the
        // timer is recorded instead of fired.
        setTimeout(fn, ms) {
            const t = { id: nextTimerId++, fn, ms, cleared: false, deferred: true };
            timers.push(t);
            return t.id;
        },
        clearTimeout(id) {
            const t = timers.find(x => x.id === id);
            if (t) t.cleared = true;
        },
        window: {
            // Resolved against the same zh-CN.json the browser loads, with {{var}} interpolated the
            // way i18next does. A key the dictionary does not carry comes back as the raw key, so a
            // missing translation fails the copy assertions instead of quietly painting a key name.
            // i18next escapes interpolated values by default (including / -> &#x2F;); a call that
            // asks for escapeValue:false gets the raw value, exactly like the browser would.
            t(key, o) {
                const found = resolve(zh, key);
                if (found === undefined) return key;
                const escape = !(o && o.interpolation && o.interpolation.escapeValue === false);
                const esc = (v) => escape ? String(v)
                    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
                    .replace(/"/g, '&quot;').replace(/'/g, '&#39;').replace(/\//g, '&#x2F;')
                    : String(v);
                return found.replace(/\{\{(\w+)\}\}/g, (m, name) => (o && name in o ? esc(o[name]) : m));
            },
            showNotification(msg, type) { toasts.push({ msg, type }); },
            confirm(message) { confirms.push(message); return confirmAnswer; },
            applyRBACToUI() {},
        },
        apiFetch(url, reqOpts = {}) {
            const method = reqOpts.method || 'GET';
            calls.push({ url, method, body: reqOpts.body });
            if ((opts.rejects || []).includes(method + ' ' + url)) {
                // Not a status code: the request itself dies (401 handling, a dropped socket),
                // which is the path that leaves a page stuck on a spinner if it is not unwound.
                return Promise.reject(new Error('network unreachable'));
            }
            const queue = responses[method + ' ' + url];
            const entry = queue && queue.length ? (queue.length > 1 ? queue.shift() : queue[0]) : null;
            if (!entry) {
                return Promise.resolve({
                    ok: false, status: 500,
                    json: () => Promise.resolve({ error: 'no fixture for ' + method + ' ' + url }),
                });
            }
            const status = entry.status === undefined ? 200 : entry.status;
            return Promise.resolve({
                ok: status >= 200 && status < 300,
                status,
                json: () => Promise.resolve(entry.body),
            });
        },
        fetch(url, reqOpts = {}) {
            fetchProbes.push({ url, opts: reqOpts });
            const answer = probeQueue.length > 1 ? probeQueue.shift() : (probeQueue[0] || 'ok');
            if (answer === 'down') {
                return Promise.reject(new TypeError('Failed to fetch'));
            }
            if (answer === 'unauthorized') {
                return Promise.resolve({ ok: false, status: 401, json: () => Promise.resolve({ error: 'unauthorized' }) });
            }
            if (answer === 'bad gateway') {
                return Promise.resolve({ ok: false, status: 502, json: () => Promise.resolve({}) });
            }
            return Promise.resolve({
                ok: true, status: 200,
                json: () => Promise.resolve(envelope(opts.status, opts.job, opts.canRestart, opts.source, opts.flags)),
            });
        },
        location: {
            pathname: '/',
            hash: '',
            replaced: [],
            reloads: 0,
            replace(target) { this.replaced.push(target); },
            reload() { this.reloads++; },
        },
    };
    sandbox.window.document = sandbox.document;
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);
    vm.runInContext(`
        this.api = {
            loadUpdateConsole, checkForUpdates, startUpdateApply, rollbackUpdate,
            updateRestartChoiceChanged, pollUpdateJob, renderUpdateConsole, stopUpdatePolling,
            startRestartNow
        };
        this.vars = {
            setBusy(v) { updateBusy = v; },
            getRestartChoice() { return updateRestartChoice; },
            getPollTimer() { return updatePollTimer; },
            getCheckDone() { return updateCheck.done; },
            autoCheckDone() { return updateAutoCheckDone; },
            getWatchdogTimer() { return updateWatchdogTimer; },
            getWatchdogSlow() { return updateWatchdogSlow; },
            restartPending() { return updateRestartPending; },
            // The deadline is wall-clock; tests move it into the past instead of sleeping 90s.
            expireWatchdog() { updateWatchdogDeadline = Date.now() - 1; }
        };
    `, sandbox);

    const flush = async () => { for (let i = 0; i < 14; i++) await new Promise(r => setImmediate(r)); };

    // Running the attribute text inside the page's own context is the point of this harness: a
    // handler wired to the wrong function, or reading the wrong element, still looks perfectly
    // fine when the attribute is only ever inspected as a string.
    function runHandler(text, thisArg) {
        const fn = vm.runInContext('(function(){' + text + '})', sandbox);
        return fn.call(thisArg || {});
    }

    function extractHandlers(html) {
        const found = { onclick: [], onchange: [] };
        const re = /on(click|change)="([^"]*)"/g;
        let m;
        while ((m = re.exec(html)) !== null) found['on' + m[1]].push(m[2]);
        return found;
    }

    const h = {
        sandbox, nodes, calls, toasts, confirms, timers, flush, runHandler, extractHandlers, responses,
        api: sandbox.api,
        vars: sandbox.vars,
        location: sandbox.location,
        probes: fetchProbes,
        // The deferred auto-check timer is inspected from this side: `timers` is the harness's own
        // array, not something the page's context can see.
        deferredTimers() { return timers.filter(t => t.deferred && !t.cleared).map(t => t.ms); },
        fireDeferred() { timers.filter(t => t.deferred && !t.cleared).forEach(t => { t.cleared = true; t.fn(); }); },
        html() { return nodes.get('update-console').innerHTML; },
        fire(name, thisArg) {
            const all = extractHandlers(h.html());
            const text = all.onclick.concat(all.onchange).find(v => v.startsWith(name + '('));
            if (!text) throw new Error('handler ' + name + ' is not present in the rendered markup');
            return runHandler(text, thisArg);
        },
        // Flipping the router's active classes (the settings page and its update section) is all
        // that leaving means for this console.
        setActive(v) {
            active = v;
            ['page-settings', 'settings-section-update'].forEach(id => {
                const node = nodes.get(id);
                if (!node) return;
                if (v) node.classList.add('active'); else node.classList.remove('active');
            });
        },
        setConfirm(v) { confirmAnswer = v; },
        liveTimers() { return timers.filter(t => !t.cleared && !t.deferred); },
        tick() {
            const live = h.liveTimers();
            if (!live.length) throw new Error('no poll timer is armed');
            return live[live.length - 1].fn();
        },
    };

    return Promise.resolve(sandbox.api.loadUpdateConsole()).then(flush).then(() => h);
}

test('the console lives inside the settings page as its own section', () => {
    // The entry is no longer a page of its own: it is one section of 系统设置, so the main sidebar
    // must not advertise a sibling item for it any more.
    assert.doesNotMatch(template, /data-page="system-update"/,
        'the standalone sidebar entry must be gone: the console is a settings section now');
    assert.doesNotMatch(template, /id="page-system-update"/,
        'the standalone page must be gone: its content moved into 系统设置');

    const settingsNavItem = template.indexOf('<div class="settings-nav-item" data-section="update" data-require-permission="update:read" onclick="switchSettingsSection(\'update\')">');
    assert.ok(settingsNavItem > -1, 'the settings menu must carry the 一键更新 entry');
    assert.match(template, /<span data-i18n="settings\.nav\.update">一键更新<\/span>/);
    const storageItem = template.indexOf('data-section="storage"');
    assert.ok(storageItem > -1 && storageItem < settingsNavItem, 'the entry must follow 存储清理 in the settings menu');

    // The console markup sits inside page-settings, wrapped in its own section.
    const pageSettings = template.indexOf('<div id="page-settings" class="page">');
    const section = template.indexOf('<div id="settings-section-update" class="settings-section-content" data-require-permission="update:read">');
    const nextPage = template.indexOf('<!-- 平台权限页面 -->');
    assert.ok(pageSettings > -1 && section > pageSettings && nextPage > section,
        'settings-section-update must live inside page-settings');
    assert.match(template, /id="settings-section-update"[\s\S]{0,700}?id="update-console"/);
    assert.match(template, /id="update-console"/);
    assert.match(template, /<script src="\/static\/js\/update\.js\?v=\{\{\.Version\}\}"><\/script>/);
    assert.match(template, /<div class="settings-section-header update-section-head">/);
    assert.match(template, /<h3 data-i18n="update\.title">/);

    // Both hash entry points carry the alias, and the retired page id is gone from the whitelists:
    // #system-update has to land on 系统设置 with its update section selected.
    const aliases = [...router.matchAll(/const settingsSection = pageId === 'system-update' \? 'update' : /g)];
    assert.equal(aliases.length, 2, 'both hash parsers must normalize #system-update into the settings section');
    const selects = [...router.matchAll(/if \(settingsSection && typeof switchSettingsSection === 'function'\) \{[\s\S]{0,80}?switchSettingsSection\(settingsSection\);/g)];
    assert.equal(selects.length, 2, 'both hash paths must open the update section after switching the page');
    const lists = router.split('\n').filter(l => l.includes("['dashboard'") && l.includes("'settings'"));
    assert.equal(lists.length, 2, 'settings must stay in both router page lists');
    assert.ok(lists.every(l => !l.includes("'system-update'")), 'no whitelist may still name the retired page id');
    assert.doesNotMatch(router, /case 'system-update':/, 'the page init case must be gone with the page');
    // Leaving the settings page stops the poller; switching sections within it is settings.js's job.
    assert.match(router, /if \(pageId !== 'settings' && typeof stopUpdatePolling === 'function'\)/);
    // Coming back to the settings page while the update section is still selected must re-read the
    // install state and re-arm the poller: leaving the page stopped it, and a frozen progress
    // read is exactly the "nothing is happening" picture an operator must never be shown.
    assert.match(router, /case 'settings':[\s\S]{0,450}?getElementById\('settings-section-update'\)[\s\S]{0,250}?loadUpdateConsole\(\)/);

    // Selecting the section runs the console; selecting anything else stops its poller.
    assert.match(settings, /if \(section === 'update'\) \{[\s\S]{0,160}?loadUpdateConsole\(\)/);
    assert.match(settings, /else if \(typeof stopUpdatePolling === 'function'\) \{[\s\S]{0,120}?stopUpdatePolling\(\);/);

    // And the console's own idea of "on screen" is the settings page plus that section.
    assert.match(source, /getElementById\('page-settings'\)/);
    assert.match(source, /getElementById\('settings-section-update'\)/);
});

test('the update console has its own stylesheet section', () => {
    const header = sheet.indexOf('一键更新控制台（系统设置页的 settings-section-update 分区）');
    assert.ok(header > -1, 'style.css must carry the update console section header');
    const block = sheet.slice(header);
    assert.ok(block.includes('.update-console'), 'the .update-* section must follow its own section header');
    ['.update-console', '.update-section-head', '.update-section-title', '.update-facts', '.update-chip-ok', '.update-chip-warn',
        '.update-chip-danger', '.update-error', '.update-check-failed', '.update-step-list', '.update-table',
        '.update-restart-choice', '.update-blocker-list', '.update-apply-btn:disabled'].forEach(sel => {
        assert.ok(sheet.includes(sel), sel + ' must be defined');
    });
    // Every class the console emits has to be styled or be an existing shared one: a hook name
    // that appears only in JS is a button nobody can see the state of.
    const emitted = [...new Set(source.match(/update-[a-z][a-z-]*/g) || [])]
        .filter(t => /^update-[a-z-]+$/.test(t));
    emitted.forEach(cls => {
        assert.ok(sheet.includes('.' + cls), cls + ' is emitted by the console but style.css never mentions it');
    });
    assert.ok(emitted.length >= 25, 'the class scan found too little to be a gate: ' + emitted.length);
});

test('all locales carry the same update keys, and everything the page asks for exists', () => {
    const zhKeys = flatKeys(zh.update, '', new Set());
    const enKeys = flatKeys(en.update, '', new Set());
    const ruKeys = flatKeys(ru.update, '', new Set());
    assert.deepEqual([...zhKeys].filter(k => !enKeys.has(k)), [], 'keys missing from en-US');
    assert.deepEqual([...enKeys].filter(k => !zhKeys.has(k)), [], 'keys missing from zh-CN');
    assert.deepEqual([...enKeys].filter(k => !ruKeys.has(k)), [], 'keys missing from ru-RU');
    assert.deepEqual([...ruKeys].filter(k => !enKeys.has(k)), [], 'ru-RU has keys other locales do not');
    assert.ok(zhKeys.size >= 60, 'the update namespace has gone thin: ' + zhKeys.size);
    // The settings menu is where this feature's entry lives now; the old nav key must be retired
    // everywhere rather than left behind as a second, drifting label.
    assert.ok(zh.settings.nav.update && en.settings.nav.update && ru.settings.nav.update,
        'settings.nav.update must exist in every locale');
    assert.equal(zh.settings.nav.update, '一键更新');
    assert.equal(en.settings.nav.update, 'One-Click Update');
    assert.equal(ru.settings.nav.update, 'Обновление в один клик');
    [['zh-CN', zh], ['en-US', en], ['ru-RU', ru]].forEach(([label, dict]) => {
        assert.ok(!dict.nav.systemUpdate, label + ' still carries the retired nav.systemUpdate key');
    });

    // The non-Chinese dictionaries must not be holding Chinese text.
    [...enKeys].forEach(k => {
        const value = resolve(en.update, k) || '';
        assert.ok(!/[一-龥]/.test(value), 'en-US still has Chinese in update.' + k + ': ' + value);
        assert.ok(value !== zh.update[k], 'en-US still has the Chinese string for update.' + k);
    });
    [...ruKeys].forEach(k => {
        const value = resolve(ru.update, k) || '';
        assert.ok(!/[一-龥]/.test(value), 'ru-RU still has Chinese in update.' + k + ': ' + value);
        assert.ok(value !== zh.update[k], 'ru-RU still has the Chinese string for update.' + k);
    });

    const htmlKeys = [...template.matchAll(/data-i18n="([^"]+)"/g)].map(m => m[1])
        .filter(k => k.startsWith('update.') || k === 'settings.nav.update');
    assert.ok(htmlKeys.length >= 3, 'the page must carry its own translated titles: ' + htmlKeys);
    htmlKeys.forEach(k => {
        assert.ok(resolve(zh, k), 'zh-CN missing ' + k);
        assert.ok(resolve(en, k), 'en-US missing ' + k);
        assert.ok(resolve(ru, k), 'ru-RU missing ' + k);
    });

    const jsKeys = [...source.matchAll(/updateT\('([^']+)'/g)].map(m => m[1]);
    assert.ok(jsKeys.length >= 40, 'the console should be fully translated, got ' + jsKeys.length);
    jsKeys.forEach(k => {
        assert.ok(resolve(zh, 'update.' + k), 'zh-CN missing update.' + k);
        assert.ok(resolve(en, 'update.' + k), 'en-US missing update.' + k);
        assert.ok(resolve(ru, 'update.' + k), 'ru-RU missing update.' + k);
    });

    // withUpdateBusy picks its error prefix out of a map, so those keys are referenced dynamically
    // and a typo would survive as a painted key name instead of failing here.
    const mapBody = (source.match(/const UPDATE_FAILURE_KEYS = \{([\s\S]*?)\n\};/) || [])[1] || '';
    const mapped = [...mapBody.matchAll(/'([a-zA-Z]+)'/g)].map(m => m[1]);
    assert.ok(mapped.length >= 4, 'the failure-key map was not found in update.js');
    mapped.forEach(k => {
        assert.ok(resolve(zh, 'update.' + k), 'zh-CN missing update.' + k);
        assert.ok(resolve(en, 'update.' + k), 'en-US missing update.' + k);
        assert.ok(resolve(ru, 'update.' + k), 'ru-RU missing update.' + k);
    });

    // No key may be dead weight: every entry has to be reachable from the page or the console.
    [...zhKeys].forEach(k => {
        const used = source.includes("'" + k.split('.').pop() + "'") || template.includes('update.' + k);
        assert.ok(used, 'update.' + k + ' is defined but nothing asks for it');
    });
});

test('opening the page reads the install tree and claims nothing about the remote', async () => {
    const h = await harness({ status: statusOf({ updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0 }) });
    assert.deepEqual(h.calls.map(c => c.method + ' ' + c.url), ['GET /api/system/update'],
        'opening the page must not fetch the remote and must not start polling');

    const html = h.html();
    assert.match(html, /\/srv\/csai/);
    assert.match(html, /main/);
    assert.match(html, /a1b2c3d/);
    assert.match(html, /2026-09-30 18:04:11/);
    assert.match(html, /git 工作树/);
    assert.match(html, /go1\.23\.4/);
    assert.match(html, /有可回滚点/);
    assert.match(html, /待并入|本机安装/, 'the console must be laid out in labelled sections');
    // behind is 0 only because nobody looked yet: that is "not checked", never "up to date".
    assert.match(html, /还没查过更新源/);
    assert.doesNotMatch(html, /已是最新/, 'a page that did not check must not say 已是最新');
    assert.doesNotMatch(html, /update\.[a-zA-Z]/, 'a raw i18n key leaked into the markup');
    assert.doesNotMatch(html, /\{\{/, 'an un-interpolated placeholder leaked into the markup');
});

test('a check that could not run is reported as a failure, never as "already up to date"', async () => {
    const h = await harness({
        status: statusOf({ updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0 }),
        responses: {
            'POST /api/system/update/check': [{
                status: 200,
                body: {
                    status: statusOf({
                        checkError: 'fatal: unable to access github: Could not resolve host',
                        updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0,
                    }),
                },
            }],
        },
    });
    h.calls.length = 0;
    h.toasts.length = 0;
    await h.api.checkForUpdates();
    await h.flush();

    assert.deepEqual(h.calls.map(c => c.method + ' ' + c.url), ['POST /api/system/update/check'],
        'a check must not re-read the tree and must not start a poller');
    const html = h.html();
    assert.match(html, /检查失败：/, 'the reason must be stated as a check failure');
    assert.match(html, /Could not resolve host/);
    assert.doesNotMatch(html, /已是最新/, 'a failed check must never be painted as up to date');
    assert.equal(h.toasts.length, 1);
    assert.match(h.toasts[0].msg, /检查失败：/);
    assert.equal(h.toasts[0].type, 'error', 'a check that failed must not toast success');
    assert.equal(h.liveTimers().length, 0, 'a check is not a job: nothing may be polling');
});

test('a transport failure of the check endpoint is still "查不了"', async () => {
    const h = await harness({
        responses: { 'POST /api/system/update/check': [{ status: 500, body: { error: 'git exited 128' } }] },
    });
    h.toasts.length = 0;
    await h.api.checkForUpdates();
    await h.flush();
    const html = h.html();
    assert.match(html, /检查失败：/);
    assert.match(html, /git exited 128/);
    assert.doesNotMatch(html, /已是最新/);
    assert.match(h.toasts[h.toasts.length - 1].msg, /检查失败：git exited 128/);
    assert.equal(h.toasts[h.toasts.length - 1].type, 'error');
});

test('a check whose request dies mid-flight releases the button and says so', async () => {
    const h = await harness({ rejects: ['POST /api/system/update/check'] });
    h.toasts.length = 0;
    await h.api.checkForUpdates();
    await h.flush();
    const html = h.html();
    assert.match(html, /检查失败：/, 'a dead request is a failure, and it has to be called one');
    assert.match(html, /network unreachable/);
    assert.doesNotMatch(html, /正在检查更新源/, 'the button must not stay frozen on "checking"');
    assert.doesNotMatch(html, /update-check-btn" disabled/, 'and it must be clickable again');
    assert.doesNotMatch(html, /已是最新/, 'a request that went nowhere is not an answer about the remote');
    assert.match(html, /还没查过更新源/, 'the page falls back to the honest "nobody looked yet"');
    assert.equal(h.toasts.length, 1);
    assert.equal(h.toasts[0].type, 'error');
    assert.equal(h.liveTimers().length, 0);
});

test('behind is counted from incomingTotal and a truncated list says so', async () => {
    const many = [];
    for (let i = 0; i < 20; i++) many.push({ commit: 'c' + i, subject: '提交 ' + i });
    const h = await harness({
        status: statusOf({ updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0 }),
        responses: {
            'POST /api/system/update/check': [{
                status: 200,
                body: { status: statusOf({ updateAvailable: true, behind: 42, incomingTotal: 42, incoming: many }) },
            }],
        },
    });
    await h.api.checkForUpdates();
    await h.flush();
    const html = h.html();
    assert.match(html, /落后 42 个提交/, 'the headline number is the total, not the length of the page');
    assert.match(html, /共 42 个新提交/);
    assert.match(html, /只列出前 20 个/);
    assert.match(html, /提交 19/);
});

test('local edits, a diverged tree and a clean-but-unchecked tree each block the button', async () => {
    const cases = [
        {
            patch: { blockingChanges: [{ path: 'internal/foo.go', status: 'modified', protected: false }] },
            copy: /有 1 个产品源码文件被本地改过/,
            path: /internal\/foo\.go/,
        },
        {
            patch: { diverged: true, ahead: 4, behind: 2, blockingChanges: [] },
            copy: /本地领先 4 个提交/,
            path: null,
        },
        {
            patch: { updateAvailable: false, behind: 0, blockingChanges: [] },
            copy: /更新源没有比本机更新的提交/,
            path: null,
        },
    ];
    for (const c of cases) {
        const h = await harness({ status: statusOf(c.patch) });
        const html = h.html();
        assert.match(html, /<button class="btn-primary update-apply-btn" disabled[^>]*onclick="startUpdateApply\(\)">/,
            'the apply button must be disabled for: ' + c.copy);
        assert.match(html, c.copy, 'a disabled button must say why');
        assert.match(html, /data-require-permission="update:apply"/);
        if (c.path) assert.match(html, c.path, 'the blocking file names must be listed, not just counted');
        // Disabled in the markup and refused in code: the two must agree.
        h.calls.length = 0;
        h.toasts.length = 0;
        h.fire('startUpdateApply');
        await h.flush();
        assert.deepEqual(h.calls, [], 'a blocked apply must never reach the server: ' + c.copy);
        assert.equal(h.toasts.length, 1, 'the refusal must be said out loud instead of failing silent');
        assert.equal(h.toasts[0].type, 'error');
    }

    const busy = await harness({ status: statusOf(), job: runningJob });
    assert.match(busy.html(), /<button class="btn-primary update-apply-btn" disabled/);
    assert.match(busy.html(), /已有更新在进行中/);
    assert.match(busy.html(), /<button class="btn-secondary update-rollback-btn" disabled/);
    assert.deepEqual(busy.calls.map(c => c.method + ' ' + c.url), ['GET /api/system/update']);
    assert.equal(busy.liveTimers().length, 1, 'opening onto a running job resumes the progress read');
});

test('a tree that only owes a build still offers the button, and says it is a build', async () => {
    // Source already at the update source's tip, binary stale: the retry the no_toolchain
    // message talks about has to be reachable from this page. "0 new commits" is not a
    // reason to disable the button, and the confirm dialog must not claim nothing will
    // happen.
    const builtJob = {
        id: 'upd-9',
        state: 'succeeded',
        started: '2026-10-08T10:00:00+08:00',
        finished: '2026-10-08T10:00:40+08:00',
        steps: [{ phase: 'build', message: '开始编译二进制（首次会下载依赖，可能需要几分钟）', at: '10:00:01' }],
        result: {
            fromCommit: 'a1b2c3d', toCommit: 'a1b2c3d',
            commits: 0, filesTouched: 0, keptContent: [],
            binaryPath: '/srv/csai/cyberstrike-ai',
            binaryBuilt: true, prevBinary: '/srv/csai/cyberstrike-ai.prev',
            backupDir: '/srv/csai/.update-backup/20261008_100000',
            needsRestart: true, duration: '40s',
        },
        restartRequested: false,
    };
    const pendingStatus = statusOf({
        updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0, buildPending: true,
    });
    const h = await harness({
        status: pendingStatus,
        responses: {
            'GET /api/system/update/job': [{ status: 200, body: { job: builtJob } }],
            'GET /api/system/update': [
                { status: 200, body: envelope(pendingStatus, null, true) },
                { status: 200, body: envelope(statusOf({ updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0, commit: 'a1b2c3d' }), builtJob, true) },
            ],
        },
    });
    const html = h.html();
    assert.match(html, /<button class="btn-primary update-apply-btn" data-require-permission="update:apply" onclick="startUpdateApply\(\)">/,
        'a pending build must not read as "nothing to update" and disable the button');
    assert.match(html, /补上这次编译/, 'the page must say what pressing update will do');
    assert.doesNotMatch(html, /更新源没有比本机更新的提交/);

    h.calls.length = 0;
    h.toasts.length = 0;
    h.fire('startUpdateApply');
    await h.flush();
    const apply = h.calls.find(c => c.method === 'POST' && c.url === '/api/system/update/apply');
    assert.ok(apply, 'the retry must reach the server: ' + h.calls.map(c => c.method + ' ' + c.url));
    assert.match(h.confirms[h.confirms.length - 1], /补上二进制/,
        'the confirm dialog describes a build, not "0 new commits"');
    const success = h.toasts.filter(t => t.type === 'success' && /补编译完成/.test(t.msg)).pop();
    assert.ok(success, 'a finished build retry is announced as a build: ' + JSON.stringify(h.toasts));
    assert.deepEqual(h.toasts.filter(t => t.type === 'success' && /a1b2c3d → a1b2c3d/.test(t.msg)), [],
        'a build-only retry must not be announced as "update a1b2c3d → a1b2c3d"');
});

test('a pending build with no toolchain fails with the server sentence, never a success', async () => {
    const failed = {
        id: 'upd-3',
        state: 'failed',
        started: '2026-10-08T11:00:00+08:00',
        finished: '2026-10-08T11:00:01+08:00',
        steps: [],
        failure: {
            reason: 'no_toolchain',
            message: '源码已经是更新源最新（a1b2c3d），但二进制还是旧的：本机没有 Go 工具链，无法补编译。装好 go 后再点一次更新即可补上二进制。',
        },
        restartRequested: false,
    };
    const h = await harness({
        status: statusOf({
            updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0,
            buildPending: true, canBuild: false, goToolchain: '',
        }),
        responses: {
            'GET /api/system/update/job': [{ status: 200, body: { job: failed } }],
        },
    });
    // 按钮仍可点：缺工具链是环境事实，让操作员看到服务端那句"装好 go 再来"才是出路。
    assert.match(h.html(), /<button class="btn-primary update-apply-btn" data-require-permission="update:apply" onclick="startUpdateApply\(\)">/);
    await h.api.startUpdateApply();
    await h.flush();
    assert.deepEqual(h.toasts.filter(t => t.type === 'success' && /更新完成|补编译完成/.test(t.msg)), [],
        'a build that did not happen must never be announced as done: ' + JSON.stringify(h.toasts));
    assert.ok(h.toasts.some(t => t.type === 'error' && /Go 工具链/.test(t.msg)),
        'the server sentence must be shown: ' + JSON.stringify(h.toasts));
});

test('applying posts {"restart":false} and then reads the job every 1.5 seconds', async () => {
    // This test is about the poll cadence and the final re-read, so the job finishes without
    // having asked for a restart; the restart hand-off has its own test below.
    const plainSucceededJob = Object.assign({}, succeededJob, { restartRequested: false });
    const h = await harness({
        responses: {
            'POST /api/system/update/apply': [{ status: 202, body: { job_id: 'upd-1', state: 'running' } }],
            'GET /api/system/update/job': [
                { status: 200, body: { job: runningJob } },
                { status: 200, body: { job: runningJob } },
                { status: 200, body: { job: plainSucceededJob } },
            ],
            'GET /api/system/update': [
                { status: 200, body: envelope(statusOf(), null, true) },
                {
                    status: 200,
                    body: envelope(statusOf({
                        updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0,
                        commit: 'f9e8d7c',
                    }), plainSucceededJob, true),
                },
            ],
        },
    });
    assert.match(h.html(), /<button class="btn-primary update-apply-btn" data-require-permission="update:apply" onclick="startUpdateApply\(\)">/,
        'a clean tree that is behind must offer the button, undimmed');

    h.calls.length = 0;
    h.fire('startUpdateApply');
    await h.flush();

    const apply = h.calls.find(c => c.method === 'POST' && c.url === '/api/system/update/apply');
    assert.ok(apply, 'no apply was posted: ' + h.calls.map(c => c.method + ' ' + c.url));
    assert.deepEqual(JSON.parse(apply.body), { restart: false },
        'the body is one object carrying a restart boolean, not a positional bag');
    assert.equal(h.liveTimers().length, 1, 'exactly one poller: ' + JSON.stringify(h.timers.map(t => t.ms)));
    assert.equal(h.liveTimers()[0].ms, 1500, 'progress must be read every 1.5 seconds');

    // In the browser the poller runs off the timer; here the test drives the armed callback so the
    // whole progress path is exercised without waiting on wall-clock time.
    h.calls.length = 0;
    await h.tick();
    await h.flush();
    assert.deepEqual(h.calls.map(c => c.method + ' ' + c.url), ['GET /api/system/update/job']);
    assert.match(h.html(), /开始拉取更新源/, 'each step line must be rendered as it arrives');
    assert.match(h.html(), /进行中/);
    assert.equal(h.liveTimers().length, 1);

    h.calls.length = 0;
    await h.tick();
    await h.flush();
    assert.equal(h.liveTimers().length, 0, 'the timer must be cleared once the job reaches a final state');
    assert.equal(h.vars.getPollTimer(), null);
    assert.deepEqual(h.calls.map(c => c.method + ' ' + c.url),
        ['GET /api/system/update/job', 'GET /api/system/update'],
        'once the job ends the install tree is re-read: the binary on disk has changed');

    const html = h.html();
    assert.match(html, /a1b2c3d/);
    assert.match(html, /f9e8d7c/);
    assert.match(html, /并入提交数[\s\S]{0,160}?>3</);
    assert.match(html, /改动文件数[\s\S]{0,160}?>27</);
    assert.match(html, /roles\/我的角色\.md/, 'keptContent must be listed by name');
    assert.match(html, /tools\/semgrep\.yaml/);
    assert.match(html, /二进制已换新/);
    assert.match(html, /需要重启才会运行新版本/);
    assert.match(html, /3m12s/);
    const success = h.toasts.filter(t => t.type === 'success' && /更新完成/.test(t.msg)).pop();
    assert.ok(success, 'a finished update must be announced: ' + JSON.stringify(h.toasts));
    assert.match(success.msg, /f9e8d7c/, 'the toast names the commit the tree is on now');
});

test('a failing job shows the server message with its item list, and never claims success', async () => {
    const h = await harness({
        responses: {
            'GET /api/system/update/job': [{ status: 200, body: { job: failedJob } }],
            // GET /api/system/update also carries h.latest(), so the re-read after the job ends
            // still has the finished job in it: the console must not lose the failure.
            'GET /api/system/update': [
                { status: 200, body: envelope(statusOf(), null, true) },
                { status: 200, body: envelope(statusOf({ updateAvailable: false, behind: 0, incoming: [], incomingTotal: 0 }), failedJob, true) },
            ],
        },
    });
    await h.api.startUpdateApply();
    await h.flush();

    const html = h.html();
    assert.match(html, /编译失败，二进制保持原版本/);
    assert.match(html, /build_failed/);
    assert.match(html, /internal\/foo\.go/, 'the failure item list must be shown');
    assert.equal(h.liveTimers().length, 0, 'a finished job must not keep polling');
    assert.deepEqual(h.toasts.filter(t => t.type === 'success' && /更新完成/.test(t.msg)), [],
        'a failed update must not be announced as finished: ' + JSON.stringify(h.toasts));
    assert.match(h.toasts[h.toasts.length - 1].msg, /编译失败/);
    assert.equal(h.toasts[h.toasts.length - 1].type, 'error');
});

test('the restart tick only appears when the process can actually be restarted', async () => {
    const off = await harness({ canRestart: false });
    assert.doesNotMatch(off.html(), /id="update-restart-choice"/,
        'offering a promise the platform cannot keep is a lie');
    assert.match(off.html(), /本次启动没有提供重启钩子/);

    const on = await harness({
        canRestart: true,
        // No job to adopt: a running job would (correctly) block the second click, and this test
        // is about what the checkbox does to the body, not about the block.
        responses: { 'GET /api/system/update/job': [{ status: 200, body: { job: null } }] },
    });
    assert.match(on.html(), /<input type="checkbox" id="update-restart-choice"[^>]*onchange="updateRestartChoiceChanged\(this\.checked\)">/);
    assert.ok(!/id="update-restart-choice" checked/.test(on.html()),
        'with nothing supervising the process, the tick must default to off: ticking it would just stop the platform');

    // The default follows the environment: launchd/systemd markers mean a restart is a
    // restart, so one click should be the whole update.
    const supervised = await harness({
        canRestart: true,
        flags: { supervised: true },
        responses: { 'GET /api/system/update/job': [{ status: 200, body: { job: null } }] },
    });
    assert.match(supervised.html(), /id="update-restart-choice" checked/,
        'detected supervision must pre-tick the box');
    // Once the operator decides for themselves, the default stops second-guessing them.
    supervised.sandbox.document.getElementById('update-restart-choice').checked = false;
    supervised.fire('updateRestartChoiceChanged', { checked: false });
    assert.ok(!/id="update-restart-choice" checked/.test(supervised.html()));

    on.calls.length = 0;
    on.fire('startUpdateApply');
    await on.flush();
    assert.deepEqual(JSON.parse(on.calls.find(c => c.url === '/api/system/update/apply').body), { restart: false });

    // Tick it through the rendered onchange, exactly as a browser does (the element is already
    // checked when change fires), then apply again.
    on.vars.setBusy(false);
    on.nodes.get('update-restart-choice').checked = true;
    on.fire('updateRestartChoiceChanged', { checked: true });
    assert.equal(on.vars.getRestartChoice(), true);
    assert.match(on.html(), /id="update-restart-choice" checked/, 'the tick must survive the repaint');

    on.calls.length = 0;
    on.vars.setBusy(false);
    on.fire('startUpdateApply');
    await on.flush();
    assert.deepEqual(JSON.parse(on.calls.find(c => c.url === '/api/system/update/apply').body), { restart: true },
        'the restart flag that goes out is the checkbox, not a guess');
});

test('the confirm dialog says what the update will do, and cancelling posts nothing', async () => {
    const h = await harness();
    h.setConfirm(false);
    h.calls.length = 0;
    h.toasts.length = 0;
    h.fire('startUpdateApply');
    await h.flush();
    assert.equal(h.confirms.length, 1, 'the operator must be asked before the tree moves');
    assert.match(h.confirms[0], /重新编译/, 'the confirm must say the binary gets rebuilt: ' + h.confirms[0]);
    assert.match(h.confirms[0], /二进制/);
    assert.match(h.confirms[0], /github\.com\/AIPentest\/CyberStrikeAI/, 'the confirm must name the repository being fetched');
    assert.match(h.confirms[0], /a1b2c3d/, 'the confirm must name where this machine is now');
    assert.doesNotMatch(h.confirms[0], /\{\{/, 'the confirm went out un-interpolated: ' + h.confirms[0]);
    assert.deepEqual(h.calls.map(c => c.method + ' ' + c.url), [], 'a cancelled update must not POST');
    assert.deepEqual(h.toasts, [], 'a cancelled update must not be reported as anything');
});

test('a second apply is refused with 409 and the running job is adopted', async () => {
    const h = await harness({
        responses: {
            'POST /api/system/update/apply': [{
                status: 409,
                body: {
                    error: '已有一次更新在进行中',
                    job: { id: 'upd-7', state: 'running', started: 'now', steps: [{ phase: 'build', message: '正在编译二进制', at: 'now' }] },
                },
            }],
        },
    });
    await h.api.startUpdateApply();
    await h.flush();
    const apply = h.calls.find(c => c.url === '/api/system/update/apply');
    assert.equal(apply.method, 'POST');
    assert.deepEqual(JSON.parse(apply.body), { restart: false });
    assert.match(h.html(), /正在编译二进制/, 'the job the server named must be shown, not swallowed');
    assert.match(h.html(), /upd-7/);
    assert.match(h.toasts[h.toasts.length - 1].msg, /已有一次更新在进行中/);
    assert.equal(h.toasts[h.toasts.length - 1].type, 'error');
    assert.deepEqual(h.toasts.filter(t => t.type === 'success'), [], 'a refusal must not toast success');
    assert.equal(h.liveTimers().length, 1, 'the adopted job must be watched');
});

test('rollback only shows with a rollback point, asks first, and re-reads the tree', async () => {
    const none = await harness({ status: statusOf({ hasRollback: false, rollbackTo: '' }) });
    assert.doesNotMatch(none.html(), /update-rollback-btn/);
    assert.match(none.html(), /没有可回滚点/);

    const h = await harness({ status: statusOf() });
    assert.match(h.html(), /<button class="btn-secondary update-rollback-btn" data-require-permission="update:apply" onclick="rollbackUpdate\(\)">/);
    assert.match(h.html(), /回滚目标：a1b2c3d/);

    h.calls.length = 0;
    h.toasts.length = 0;
    h.setConfirm(false);
    h.fire('rollbackUpdate');
    await h.flush();
    assert.deepEqual(h.calls, [], 'a cancelled rollback must not POST');
    assert.equal(h.confirms.length, 1);
    assert.match(h.confirms[0], /a1b2c3d/);

    h.setConfirm(true);
    h.calls.length = 0;
    h.vars.setBusy(false);
    h.fire('rollbackUpdate');
    await h.flush();
    assert.deepEqual(h.calls.map(c => c.method + ' ' + c.url),
        ['POST /api/system/update/rollback', 'GET /api/system/update'],
        'a rollback must be followed by a fresh read of the install tree');
    assert.equal(h.calls[0].body, undefined, 'rollback takes no body');
    assert.match(h.toasts[h.toasts.length - 1].msg, /已回滚到 a1b2c3d/);
    assert.equal(h.toasts[h.toasts.length - 1].type, 'success');
    assert.doesNotMatch(h.html(), /已是最新/, 'after a rollback the page has not looked at the remote');
});

test('a refused rollback repeats the server reason and its file list', async () => {
    const h = await harness({
        responses: {
            'POST /api/system/update/rollback': [{
                status: 409,
                body: { error: 'HEAD 已经移动，不是那次更新留下的位置', reason: 'moved_since_update', items: ['roles/我的角色.md'] },
            }],
        },
    });
    await h.api.rollbackUpdate();
    await h.flush();
    assert.match(h.html(), /回滚失败/);
    assert.match(h.html(), /HEAD 已经移动/);
    assert.match(h.html(), /roles\/我的角色\.md/);
    assert.equal(h.toasts[h.toasts.length - 1].type, 'error');
    assert.match(h.toasts[h.toasts.length - 1].msg, /HEAD 已经移动/);
    assert.deepEqual(h.toasts.filter(t => t.type === 'success'), []);
});

test('a 202 that carries an error is not mistaken for success', async () => {
    // The handler answers "the source went back but the binary could not be rebuilt" with HTTP
    // 202 plus an error body (statusForError maps no_toolchain onto 202). Trusting resp.ok alone
    // toasts 已回滚 over a rollback that only half happened.
    const h = await harness({
        responses: {
            'POST /api/system/update/rollback': [{
                status: 202,
                body: { error: '源码已回到更新前，但本机没有 Go 工具链，二进制没换', reason: 'no_toolchain' },
            }],
        },
    });
    h.toasts.length = 0;
    h.calls.length = 0;
    await h.api.rollbackUpdate();
    await h.flush();
    assert.match(h.html(), /回滚失败/);
    assert.match(h.html(), /二进制没换/);
    assert.deepEqual(h.toasts.filter(t => t.type === 'success'), [],
        'a 2xx carrying an error must never be toasted as success: ' + JSON.stringify(h.toasts));
    assert.equal(h.toasts[h.toasts.length - 1].type, 'error');
    assert.match(h.toasts[h.toasts.length - 1].msg, /二进制没换/);
    assert.deepEqual(h.calls.map(c => c.method + ' ' + c.url), ['POST /api/system/update/rollback'],
        'a half-finished rollback must not be followed by a victory lap through the status endpoint');
});

test('leaving the page kills the poller, and reopening arms exactly one', async () => {
    const h = await harness();
    h.fire('startUpdateApply');
    await h.flush();
    assert.equal(h.liveTimers().length, 1);

    h.calls.length = 0;
    h.setActive(false);
    await h.tick();
    await h.flush();
    assert.equal(h.liveTimers().length, 0, 'switching pages must clear the interval');
    assert.deepEqual(h.calls, [], 'a page that is not on screen must not keep asking');
    assert.equal(h.vars.getPollTimer(), null);

    h.setActive(true);
    await h.api.loadUpdateConsole();
    assert.equal(h.liveTimers().length, 0, 'a clean open arms nothing');

    const busy = await harness({ job: { id: 'upd-3', state: 'running', started: 'x', steps: [] } });
    assert.equal(busy.liveTimers().length, 1, 'opening onto a running job arms the poller');
    await busy.api.loadUpdateConsole();
    assert.equal(busy.liveTimers().length, 1, 'reopening must not stack a second poller');
    assert.equal(busy.timers.length, 2, 'the old timer was cleared rather than leaked: ' + JSON.stringify(busy.timers.map(t => t.cleared)));
});

test('nothing invented: the only toast channel this page may use is showNotification', () => {
    assert.doesNotMatch(source, /showToast/, 'window.showToast does not exist on this page');
    assert.match(source, /typeof showNotification === 'function'/);
    assert.match(source, /window\.applyRBACToUI === 'function'/,
        'optional globals must be probed before they are called');
});

test('server strings cannot become markup, in text or in an attribute', async () => {
    const hostile = '危险 <script>alert(1)</script> "quoted" &more';
    const h = await harness({
        status: statusOf({
            root: '/srv/"><img src=x onerror=alert(1)>/x',
            subject: hostile,
            remoteSubject: hostile,
            blockingChanges: [{ path: 'a"><script>steal()</script>.go', status: 'modified "m"', protected: false }],
            localChanges: [{ path: 'roles/"><svg onload=alert(1)>.md', status: 'modified', protected: true }],
            incoming: [{ commit: 'c"><i>', subject: hostile }],
            incomingTotal: 1,
            checkError: 'fetch failed on <b>remote</b> "origin"',
        }),
    });
    const html = h.html();
    assert.doesNotMatch(html, /<script/, 'a commit subject became a script tag');
    assert.doesNotMatch(html, /<img\b|<svg\b|<b>/, 'markup from server data reached the DOM');
    assert.match(html, /危险 &lt;script&gt;alert\(1\)&lt;\/script&gt; "quoted"/,
        'escapeHtml leaves quotes alone inside text, which is correct and must not be double escaped');
    assert.match(html, /&amp;more/, 'the ampersand must be escaped exactly once');

    // Attribute integrity: one raw double quote inside a value would close it early and let the
    // rest of the string become attributes of the element.
    ['title', 'class', 'onclick', 'onchange', 'data-require-permission'].forEach(attr => {
        const opened = (html.match(new RegExp(attr + '="', 'g')) || []).length;
        const closed = (html.match(new RegExp(attr + '="[^"]*"', 'g')) || []).length;
        assert.equal(opened, closed, 'a ' + attr + ' attribute was closed by an unescaped quote');
    });
    // Counting matched attribute pairs is not enough on its own: a value that ends early still
    // leaves the open and close counts equal. Two hostile strings have to appear fully escaped
    // inside their title attribute - that is the proof escapeAttr, not just escapeHtml, guarded it.
    assert.match(html, /title="a&quot;&gt;&lt;script&gt;steal\(\)&lt;\/script&gt;\.go"/,
        'the blocking file path went into an attribute without quote escaping');
    assert.match(html, /title="\/srv\/&quot;&gt;&lt;img src=x onerror=alert\(1\)&gt;\/x"/,
        'the install root went into an attribute without quote escaping');
    assert.doesNotMatch(html, /title="[^"]*"[^ >]/, 'a title attribute was closed early by server data');
    const titles = html.match(/title="[^"]*"/g) || [];
    assert.ok(titles.length >= 8, 'these assertions rely on title attributes being emitted: ' + titles.length);
    titles.forEach(t => assert.ok(!/<|>/.test(t), 'a title value still contains markup: ' + t));

    // Every emitted handler must still be executable as written: an injection into an attribute
    // would break the statement instead.
    const found = h.extractHandlers(html);
    assert.ok(found.onclick.length && found.onchange.length, 'handlers must still render under hostile data');
    found.onclick.concat(found.onchange).forEach(text => {
        assert.doesNotMatch(text, /"/, 'handler text carries a raw quote: ' + text);
        assert.equal(typeof vm.runInContext('(function(){' + text + '})', h.sandbox), 'function',
            'the emitted handler is not valid JS: ' + text);
    });
    ['startUpdateApply', 'checkForUpdates', 'rollbackUpdate', 'updateRestartChoiceChanged']
        .forEach(name => assert.ok(found.onclick.concat(found.onchange).some(t => t.startsWith(name + '(')),
            name + ' must stay wired'));
});

test('the rendered handlers are executed, and they only hit the documented endpoints', async () => {
    const h = await harness();
    const found = h.extractHandlers(h.html());
    const texts = found.onchange.concat(found.onclick);
    assert.ok(texts.length >= 4, 'expected the console to emit handlers, got ' + texts.length);
    texts.forEach(t => assert.ok(HANDLES.includes(t.split('(')[0]), 'unknown handler wired into the page: ' + t));

    h.calls.length = 0;
    h.fire('checkForUpdates');
    await h.flush();
    assert.equal(h.calls[0].method, 'POST');
    assert.equal(h.calls[0].url, '/api/system/update/check');
    assert.equal(h.calls[0].body, undefined, 'the check endpoint takes no body');

    h.vars.setBusy(false);
    h.calls.length = 0;
    h.fire('startUpdateApply');
    await h.flush();
    assert.deepEqual(h.calls.map(c => c.url),
        ['/api/system/update/apply', '/api/system/update/job'],
        'apply posts once, then the progress read goes to the job endpoint');
    assert.equal(h.calls[0].method, 'POST');
    assert.equal(h.calls[1].method, 'GET');
});

// The page now asks the remote by itself, once, so the operator arrives at an answer rather
// than at a button. These pin the politeness of that: one check per session, only for a tree
// that can be updated, never on top of a running job - and the copy must describe what the
// code actually does.
test('opening the page schedules exactly one deferred remote check', async () => {
    const h = await harness({
        status: statusOf({ installed: true }),
        checkedStatus: statusOf({ behind: 3, incomingTotal: 3, updateAvailable: true }),
    });
    assert.deepStrictEqual(h.deferredTimers(), [1200], 'one deferred check must be queued');
    h.fireDeferred();
    await h.flush();
    const checks = h.calls.filter(c => c.method === 'POST' && c.url === '/api/system/update/check');
    assert.strictEqual(checks.length, 1, 'the deferred timer must ask the remote exactly once');
    assert.match(h.html(), /3/, 'the answer must be on screen without another click');

    // Re-entering the page in the same session does not go back to the remote again.
    h.api.loadUpdateConsole();
    await h.flush();
    assert.deepStrictEqual(h.deferredTimers(), [], 'the auto check is once per session, not per render');
    assert.strictEqual(h.vars.autoCheckDone(), true);
});

test('a tree that cannot be updated is never phoned home for', async () => {
    const h = await harness({ status: statusOf({ installed: false }) });
    assert.deepStrictEqual(h.deferredTimers(), [], 'no auto check when this is not a git installation');
    assert.strictEqual(h.calls.filter(c => c.url === '/api/system/update/check').length, 0);
});

test('an update already running is not disturbed by an auto check', async () => {
    const h = await harness({ status: statusOf({ installed: true }), job: runningJob });
    assert.deepStrictEqual(h.deferredTimers(), [], 'a running job is already the story on screen');
    assert.strictEqual(h.vars.autoCheckDone(), false, 'a skipped check must not spend the session quota');
});

test('the hint describes the automatic check instead of denying any network use', () => {
    for (const [label, dict] of [['zh-CN', zh], ['en-US', en], ['ru-RU', ru]]) {
        const text = dict.update.notChecked;
        assert.ok(text, label + ' lost the notChecked copy');
        assert.doesNotMatch(text, /不会联网|makes no network call|no network call|не выходит в сеть|без обращения к сети/,
            label + ' still promises the page never touches the network');
        assert.match(text, /自动|automatic|automatically|автоматическ/, label + ' must mention the automatic check');
    }
});

test('the source section shows what is in effect, and saving posts the one address', async () => {
    const h = await harness({ status: statusOf({ installed: true }), source: configuredSource });
    assert.match(h.html(), /https:\/\/github\.com\/Sycun\/CyberStrikeAI\.git/,
        'the effective address must be visible');
    assert.match(h.html(), /update-source-url/, 'the editable address field must be there');
    assert.match(h.html(), /update-source-save-btn/);

    h.runHandler("updateSourceRepoChanged('https://gitlab.com/mirror/csai.git')");
    await h.fire('saveUpdateSource');
    await h.flush();
    const save = h.calls.find(c => c.url === '/api/system/update/source');
    assert.ok(save, 'saving must hit the source endpoint');
    assert.deepEqual(JSON.parse(save.body), { repo: 'https://gitlab.com/mirror/csai.git' },
        'the save carries exactly the one address, nothing else');
    assert.ok(h.toasts.some(t => t.type === 'success'), 'a saved source must be confirmed');
});

test('a finished check must not leave the source form frozen (the button stays clickable)', async () => {
    // 线上实测过的形态：打开页面 → 自动检查在忙期里重绘 → 检查结束后不再重绘，
    // 「保存更新源」带着 disabled + data-state-disabled 留在 DOM 上——之后点击永远没有反应，
    // 服务端连请求都收不到。这里把用户的整条动作链原样跑一遍。
    const h = await harness({ status: statusOf({ installed: true }), source: configuredSource });

    h.fireDeferred(); // 打开页面 1.2 秒后的那次自动检查
    assert.match(h.html(), /update-source-save-btn" disabled/,
        'the form must visibly lock while a check is in flight');
    await h.flush();
    assert.match(h.html(), /已是最新|落后/, 'precondition: the automatic check finished');
    assert.doesNotMatch(h.html(), /update-source-save-btn" disabled/,
        'a finished check must re-render the form with the button enabled again');

    // 用户的下一步：清空地址 → 点保存 → 请求必须真的发出去
    h.runHandler("updateSourceRepoChanged('')");
    await h.fire('saveUpdateSource');
    await h.flush();
    const save = h.calls.find(c => c.url === '/api/system/update/source');
    assert.ok(save, 'saving right after a check must reach the server');
    assert.deepEqual(JSON.parse(save.body), { repo: '' });
    assert.doesNotMatch(h.html(), /update-source-save-btn" disabled/,
        'after saving the button must be ready for the next action, not frozen');
});

test('a second save is not swallowed by a stale busy flag', async () => {
    const h = await harness({ status: statusOf({ installed: true }), source: configuredSource });
    h.runHandler("updateSourceRepoChanged('https://gitlab.com/mirror/csai.git')");
    await h.fire('saveUpdateSource');
    await h.flush();
    await h.fire('saveUpdateSource');
    await h.flush();
    const saves = h.calls.filter(c => c.url === '/api/system/update/source');
    assert.equal(saves.length, 2, 'the second save must also reach the server');
});

test('clearing the address is the way back to the official repository', async () => {
    const h = await harness({ status: statusOf({ installed: true }), source: configuredSource });
    h.runHandler('updateSourceRepoChanged("")');
    await h.fire('saveUpdateSource');
    await h.flush();
    const save = h.calls.find(c => c.url === '/api/system/update/source');
    assert.deepEqual(JSON.parse(save.body), { repo: '' },
        'an empty address means \"use the official repository again\", not an error');
});

test('a refused save shows the server reason instead of a generic failure', async () => {
    const refusal = '仓库地址不合法（只允许 https/http/ssh/git/file:// 或本机绝对路径）';
    const h = await harness({
        status: statusOf({ installed: true }),
        source: configuredSource,
        responses: {
            'POST /api/system/update/source': [{ status: 400, body: { error: refusal } }],
        },
    });
    h.runHandler("updateSourceRepoChanged('ext::sh -c true')");
    await h.fire('saveUpdateSource');
    await h.flush();
    assert.match(h.html(), /不合法/, 'the refusal must be on screen');
    assert.ok(h.toasts.some(t => t.type === 'error' && t.msg.includes('不合法')), 'and in a toast');
    assert.match(h.html(), /ext::sh -c true/, 'the typed value must survive the failed save');
});

test('the adopt entry appears only for a directory that is not a git installation', async () => {
    // No source configured either: the official repository is the default, so connecting is
    // always available for a non-git directory.
    const notGit = await harness({ status: statusOf({ installed: false }), source: emptySource });
    assert.match(notGit.html(), /previewAdoptSource\(\)/, 'a non-git tree must offer connecting even with the default source');

    const gitTree = await harness({ status: statusOf({ installed: true }), source: emptySource });
    assert.doesNotMatch(gitTree.html(), /previewAdoptSource\(\)/, 'a git installation updates, it does not adopt');
});

test('adopt previews first, and confirming carries the counts into the dialog', async () => {
    const h = await harness({ status: statusOf({ installed: false }), source: configuredSource });
    await h.fire('previewAdoptSource');
    await h.flush();
    assert.match(h.html(), /abc1234/, 'the target commit must be on screen');
    assert.match(h.html(), /web\/static\/js\/a\.js/, 'the files to be replaced must be listed');
    assert.match(h.html(), /roles\/我的角色\.yaml/, 'and the operator content that is kept');

    await h.fire('confirmAdoptSource');
    await h.flush();
    assert.strictEqual(h.confirms.length, 1, 'connecting must ask first');
    assert.match(h.confirms[0], /42/, 'the dialog counts what will happen');
    const adopt = h.calls.find(c => c.url === '/api/system/update/adopt' && c.method === 'POST' && c.body && c.body.includes('"confirm":true'));
    assert.ok(adopt, 'confirming must post to adopt');
    assert.deepEqual(JSON.parse(adopt.body), { confirm: true, restart: false });
    assert.ok(h.vars.getPollTimer(), 'the job it started must be polled');
});

// ---------------------------------------------------------------------------
// 待生效的二进制：常驻横幅 + 立即重启 + 守侧重连。这一组是"忘勾重启也有救、重启后页面
// 自己回来"的直接门禁——任何一处退化成"界面没反应"或"要手动刷新"，这里都会红。
// ---------------------------------------------------------------------------

test('a binary swap that never got a restart is announced with a way to activate it', async () => {
    const h = await harness({ flags: { needsRestart: true, binaryBuiltAt: '2026-10-07T22:23:04+08:00' } });
    assert.match(h.html(), /update-restart-banner/,
        'the tree already says the new commit while the process still runs the old build; that must be impossible to miss');
    assert.match(h.html(), /立即重启服务/);
    assert.match(h.html(), /data-require-permission="update:apply"/);
    assert.match(h.html(), /2026/, 'the banner names when the binary on disk was built');
    assert.match(h.html(), /\d{1,2}\/\d{1,2}\/\d{4}/,
        'the build time must render as a date: i18next escapes / unless the console turns that off');
    assert.doesNotMatch(h.html(), /&#x2F;/, 'an interpolated value must not arrive pre-escaped');
    assert.match(h.html(), /需要重启才会运行新版本/);
    assert.match(source, /interpolation: \{ escapeValue: false \}/,
        'the console has to stop i18next from HTML-escaping interpolated values before escapeHtml runs');

    // A tree without a commit (not a git install) must still read like a sentence.
    const noCommit = await harness({
        flags: { needsRestart: true, binaryBuiltAt: '2026-10-07T22:23:04+08:00' },
        status: statusOf({ installed: false, commit: '' }),
    });
    assert.doesNotMatch(noCommit.html(), /已更新到 -/, 'no commit must not read as "updated to -"');
    assert.match(noCommit.html(), /磁盘上的二进制已换新/, 'the no-commit sentence must stand on its own');

    const clean = await harness({});
    assert.doesNotMatch(clean.html(), /update-restart-banner/,
        'a process running the binary that is on disk has nothing to restart for');
});

test('restart now asks first, posts once, and hands the page to the watchdog', async () => {
    const h = await harness({
        flags: { needsRestart: true },
        responses: { 'POST /api/system/update/restart': [{ status: 202, body: { restarting: true } }] },
    });
    h.setConfirm(false);
    h.calls.length = 0;
    h.fire('startRestartNow');
    await h.flush();
    assert.equal(h.confirms.length, 1, 'standing the service down is confirmed, never implied');
    assert.match(h.confirms[0], /守护/, 'the confirm has to say what brings it back: ' + h.confirms[0]);
    assert.deepEqual(h.calls, [], 'a cancelled restart must not post');
    assert.equal(h.vars.getWatchdogTimer(), null, 'nothing may be watched for a restart nobody asked for');

    h.setConfirm(true);
    h.vars.setBusy(false);
    h.calls.length = 0;
    h.fire('startRestartNow');
    await h.flush();
    assert.deepEqual(h.calls.map(c => c.method + ' ' + c.url), ['POST /api/system/update/restart']);
    assert.match(h.html(), /正在重启服务/, 'the console becomes the recovery view');
    assert.ok(h.vars.getWatchdogTimer(), 'the watchdog must be armed once the stand-down is accepted');
    // Exactly one watcher: the console is replaced by the recovery view, so there is no second
    // button to press - and re-arming must not stack timers anyway.
    assert.equal(h.timers.filter(t => !t.cleared && t.ms === 2000).length, 1);
});

test('a refused restart is reported with the server sentence and arms nothing', async () => {
    const refusal = '磁盘上的二进制与当前进程一致，没有待生效的版本';
    const h = await harness({
        flags: { needsRestart: true },
        responses: { 'POST /api/system/update/restart': [{ status: 409, body: { error: refusal } }] },
    });
    await h.api.startRestartNow();
    await h.flush();
    assert.match(h.toasts[h.toasts.length - 1].msg, /没有待生效的版本/, 'the server sentence is the one to show');
    assert.equal(h.toasts[h.toasts.length - 1].type, 'error');
    assert.equal(h.vars.getWatchdogTimer(), null, 'a refusal must not start watching for a boot that is not coming');
    assert.equal(h.location.replaced.length, 0);
});

test('the watchdog reloads only when the NEW process answers, never while the old one still does', async () => {
    const h = await harness({
        flags: { needsRestart: true },
        probes: ['ok', 'down', 'down', 'unauthorized'],
        responses: { 'POST /api/system/update/restart': [{ status: 202, body: { restarting: true } }] },
    });
    await h.api.startRestartNow();
    await h.flush();
    assert.equal(h.location.replaced.length, 0);

    await h.tick();
    await h.flush();
    assert.equal(h.location.replaced.length, 0, 'a 200 from the old process is not "it is back"');

    await h.tick();
    await h.flush();
    await h.tick();
    await h.flush();
    assert.equal(h.location.replaced.length, 0, 'a dead service is not a recovered one');

    await h.tick();
    await h.flush();
    assert.equal(h.location.replaced.length, 1,
        'a 401 is the proof: only a new process can fail to know our session');
    assert.match(h.location.replaced[0], /\?restarted=\d+/);
    assert.match(h.location.replaced[0], /#system-update$/);
    assert.equal(h.vars.getWatchdogTimer(), null, 'the page is leaving: the timer must be down');
});

test('a restart that does not come back says so instead of spinning forever', async () => {
    const downCase = await harness({
        flags: { needsRestart: true },
        probes: ['down'],
        responses: { 'POST /api/system/update/restart': [{ status: 202, body: { restarting: true } }] },
    });
    await downCase.api.startRestartNow();
    await downCase.flush();
    downCase.vars.expireWatchdog();
    await downCase.tick();
    await downCase.flush();
    assert.match(downCase.html(), /没有守护进程|不会自己启动/, 'the down advice must name the supervision question');

    const aliveCase = await harness({
        flags: { needsRestart: true },
        probes: ['ok'],
        responses: { 'POST /api/system/update/restart': [{ status: 202, body: { restarting: true } }] },
    });
    await aliveCase.api.startRestartNow();
    await aliveCase.flush();
    aliveCase.vars.expireWatchdog();
    await aliveCase.tick();
    await aliveCase.flush();
    assert.equal(aliveCase.vars.getWatchdogSlow(), 'running');
    assert.match(aliveCase.html(), /仍在以旧进程应答|重启似乎没有发生/);
});

test('an update that asked for a restart hands the page to the watchdog instead of reporting a failed poll', async () => {
    const h = await harness({
        flags: { supervised: true },
        responses: {
            'GET /api/system/update/job': [
                { status: 200, body: { job: runningJob } },
                { status: 200, body: { job: succeededJob } },
            ],
        },
        probes: ['unauthorized'],
    });
    // In a real browser the pre-ticked box makes this true; the harness reads the element.
    h.sandbox.document.getElementById('update-restart-choice').checked = true;
    h.fire('startUpdateApply');
    await h.flush();
    const apply = h.calls.find(c => c.url === '/api/system/update/apply');
    assert.deepEqual(JSON.parse(apply.body), { restart: true },
        'with launchd/systemd detected, one click is the whole update, restart included');

    // The apply already polled once (running); this tick reads the finished job, which asked
    // for the restart - that is the hand-off point.
    await h.tick();
    await h.flush();
    assert.match(h.html(), /正在重启服务/, 'the succeeded-with-restart job hands over to the recovery view');
    assert.deepEqual(h.toasts.filter(t => t.type === 'error'), [],
        'the process going away by request is not a polling failure to complain about');
    assert.ok(h.vars.restartPending());

    await h.tick();
    await h.flush();
    assert.equal(h.location.replaced.length, 1);
});

test('a job poll that dies while the restart is in flight is not a failure', async () => {
    // The realistic race: the process exits (and stops answering the job poll) before the
    // page's next poll lands. That is the restart happening, not a broken console.
    const h = await harness({
        flags: { supervised: true },
        rejects: ['GET /api/system/update/job'],
        probes: ['unauthorized'],
    });
    h.sandbox.document.getElementById('update-restart-choice').checked = true;
    h.fire('startUpdateApply');
    await h.flush();
    assert.match(h.html(), /正在重启服务/, 'a dead poll while a restart is requested means the restart is in flight');
    assert.deepEqual(h.toasts.filter(t => t.type === 'error'), [], 'and it must not be toasted as a failure');

    await h.tick();
    await h.flush();
    assert.equal(h.location.replaced.length, 1);
});

test('an interrupted rollback offers recovery and blocks a new update', async () => {
    const h = await harness({ status: statusOf({ rollbackPending: true, hasRollback: true }) });
    assert.match(h.html(), /上一次回滚尚未完成/);
    assert.match(h.html(), /update-rollback-btn" data-require-permission="update:apply"/);
    assert.match(h.html(), /update-apply-btn" disabled data-state-disabled="true"/);
    h.calls.length = 0;
    h.fire('startUpdateApply');
    await h.flush();
    assert.equal(h.calls.some(c => c.url === '/api/system/update/apply'), false);
});
