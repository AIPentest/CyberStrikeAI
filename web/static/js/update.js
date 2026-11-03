// Console "one-click update": what this page acts on is the very installation tree that is currently serving requests.
// Three hard rules, each one a pitfall this repository has already hit once:
//   1) If it can't check, it must say it can't check. When checkError is non-empty, the page writes out the reason, never rendering as "already up to date"——
//      Portraying "not checked" as "not updated" means the operator only finds out at the next failure that the tree stopped moving long ago.
//   2) The hint only reports the real HTTP result, and only uses the platform's showNotification if it actually exists; if there isn't one, do nothing.
//   3) For strings coming from the server, always use escapeHtml when they go into HTML, and always use escapeAttr when they go into attributes.
const UPDATE_POLL_INTERVAL_MS = 1500;
// Restart guard side: the gap between the process exiting and the new process accepting clients, the pacing of page probing and the patience before giving up.
const UPDATE_WATCHDOG_INTERVAL_MS = 2000;
const UPDATE_WATCHDOG_TIMEOUT_MS = 90000;

// withUpdateBusy is the prefix for picking error message text based on the action; all keys must exist in the three language files.
const UPDATE_FAILURE_KEYS = {
    check: 'checkFailed',
    apply: 'applyFailed',
    rollback: 'rollbackFailed',
    load: 'loadFailed',
    source: 'sourceSaveFailed',
    adopt: 'adoptFailed',
    restart: 'restartFailed',
};

let updateConsoleState = null;   // The full package for GET /api/system/update: {status, job, canRestart, supervised, needsRestart}
let updateCheck = { done: false, error: '' };
let updateBusy = false;          // Action requests issued by the page itself (check / start update / rollback / restart)
let updateChecking = false;      // Only affects the wording of the "Check for Updates" button
let updatePollTimer = null;
let updateRestartChoice = true;  // The checkbox should still be checked after the polling redraw; the default value follows supervised
let updateRestartChoiceSet = false; // After the operator has manually changed it in this session, the default value no longer overrides his choice
let updateRestartPending = false;   // Restart requested: process exit / new process taking over clients is imminent
let updateWatchdogTimer = null;     // Restart the guard side's own timer, so it doesn't interfere with the task progress polling
let updateWatchdogDeadline = 0;
let updateWatchdogSlow = null;      // null=not timed out yet; 'running'=still answered by the old process; 'down'=service hasn't responded
let updateAutoCheckDone = false; // Only auto-query the remote once per session, don't pester it on every redraw
let updateSourceDraft = null;    // Repository address of the update source being edited; null=still uses the value saved on the server
let updateSourceError = '';      // The reason for the most recent save/connect failure, drawn in the block
let updateActionError = '';      // Reason for the most recent action's failure (check/update/rollback/restart), rendered at the top of the console
let updateAdoptPlan = null;      // Preview the returned access plan; clear it after confirmation

function updateT(key, opts) {
    const k = 'update.' + key;
    if (typeof window.t !== 'function') return k;
    // i18next by default also converts the / " ' inside interpolated values into HTML entities; the console result either goes into escapeHtml (rendering),
    // or goes into confirm (plain text), neither of the two destinations needs it to be escaped first — having it on would actually escape the / in dates and branch names
    // Displayed as &#x2F; (empirically verified: devtree/deploy/... is displayed exactly like this in the confirmation box).
    const merged = Object.assign({ interpolation: { escapeValue: false } }, opts || {});
    return window.t(k, merged);
}

function updateSourceOf() {
    return (updateConsoleState && updateConsoleState.source) || { repo: '', configured: false, defaultRepo: '' };
}

// Draft takes precedence: the redraw triggered by polling must not clobber the address currently being typed; once the save succeeds, discard the draft and revert to the server's value.
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
// failure is not retried here: the page says so plainly, and the operator presses "check for updates"
// when they want to ask again.
function scheduleUpdateAutoCheck() {
    if (updateAutoCheckDone) return false;
    const status = updateConsoleState && updateConsoleState.status;
    if (!status || !status.installed || isUpdateJobRunning()) return false;
    updateAutoCheckDone = true;
    setTimeout(() => {
        if (!isUpdateConsoleActive()) return;
        checkForUpdates();
    }, 1200);
    return true;
}

function isUpdateConsoleActive() {
    // The console lives in the "one-click update" section of the system settings page: the page being active isn't enough, the section also has to be the currently selected one.
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
    // A single GET can't fetch behind/updateAvailable (it doesn't go online), keep the previous done=true
    // would then render "not checked" as "already up to date".
    updateCheck = { done: false, error: '' };
}

// ---------------------------------------------------------------------------
// Request layer
// ---------------------------------------------------------------------------

// Each call needs to get the status code and the parsed body: the 409 from apply/rollback carries the task that's currently running,
// Throwing it away directly is equivalent to discarding the "one is already running, this is its progress" (information).
// ok is not the same as the status code: rollback, in the halfway case of "source rolled back, binary not", responds with 202 and carries
// of the error field (statusForError in the handler classifies no_toolchain as 202), only trust resp.ok
// will just report success for a rollback that was never completed.
async function runUpdateRequest(method, url, body) {
    const resp = await apiFetch(url, {
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

async function fetchUpdateStatus() {
    const r = await runUpdateRequest('GET', '/api/system/update');
    if (!r.ok) throw new Error(r.error);
    updateConsoleState = {
        status: r.data.status || {},
        job: r.data.job || null,
        canRestart: !!r.data.canRestart,
        supervised: !!r.data.supervised,
        needsRestart: !!r.data.needsRestart,
        binaryBuiltAt: r.data.binaryBuiltAt || '',
        source: r.data.source || { repo: '', configured: false, defaultRepo: '' },
    };
    noteRestartDefault();
    return updateConsoleState;
}

// Default value of the checkbox: checked by default only when a watchdog exists (the launchd/systemd start-up marker) -- unchecked when there is none
// Equals "one-click service point stop". After the operator has touched the checkbox himself, the server-side value no longer overrides his selection.
function noteRestartDefault() {
    if (updateRestartChoiceSet) return;
    updateRestartChoice = !!(updateConsoleState && updateConsoleState.supervised);
}

// notify only acts when the platform actually loads showNotification. It must never become the reason why "the operation appears to have failed":
// The request has already succeeded; in order to report a message, calling a function that doesn't exist on the page will blow the success into a failure.
function notify(message, type) {
    if (typeof showNotification === 'function') {
        try {
            showNotification(message, type);
        } catch (e) {
            console.warn('notify', e);
        }
    }
}

// The redraw during the action period must be symmetric from head to tail. When busy, draw "occupied" (button with disabled); when idle, redraw it back—
// The check process of this page (including the automatic check after opening the page) will re-render during the busy period, if it no longer re-renders at the end,
// That disabled stays on the DOM, permanently freezing the button: in actual testing, after opening the page and auto-checking once, the "Save update source"
// With disabled + data-state-disabled left on the page, clicking again never gets any response — not even a request gets sent out.
// The banner is changed to a state (updateActionError) rendered by renderUpdateConsole, so exiting the redraw won't erase it;
// When restarting the standby-side takeover screen (updateRestartPending), never redraw it again; don't overlay the self-recovery view back over the console.
async function withUpdateBusy(fn, label) {
    if (updateBusy) return;
    updateBusy = true;
    updateActionError = '';
    if (!updateRestartPending) renderUpdateConsole();
    try {
        await fn();
    } catch (err) {
        const reason = err && err.message ? err.message : String(err);
        // When the request blows up midway, the screen is still stuck on the "Checking update sources…" state, which is no longer valid,
        // Just adding a line of red text is useless: the banner redraws following the state, and the screen itself also needs to be drawn once according to the current state.
        updateActionError = updateT(UPDATE_FAILURE_KEYS[label] || 'loadFailed', { reason: reason });
        if (!updateRestartPending) renderUpdateConsole();
        // The inline banner gets wiped away by the redraw when the next action starts; the toast is the one that actually sticks around.
        notify(updateActionError, 'error');
    } finally {
        updateBusy = false;
        if (!updateRestartPending) renderUpdateConsole();
    }
}

// ---------------------------------------------------------------------------
// Entry point and polling
// ---------------------------------------------------------------------------

async function loadUpdateConsole() {
    stopUpdatePolling();
    // Restart has already been initiated; the console yields to the self-recovery view, no longer reading a service that is exiting.
    if (updateRestartPending) {
        startUpdateWatchdog();
        return;
    }
    resetUpdateCheck();
    updateRestartChoiceSet = false;
    updateRestartChoice = true; // Then corrected by noteRestartDefault according to supervised
    updateActionError = '';     // Re-entering the page = clean screen, the red text from the last action doesn't carry over
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
    // When the page opens there's already an update in flight (e.g., one triggered from another tab), keep watching it, don't let people think it's not moving.
    if (isUpdateJobRunning()) startUpdatePolling();
    scheduleUpdateAutoCheck();
}

// There is only one timer: it re-arms if not cleared, which is the source of "after the page is opened twice, two polls write into the same log".
function startUpdatePolling() {
    stopUpdatePolling();
    updatePollTimer = setInterval(pollUpdateJob, UPDATE_POLL_INTERVAL_MS);
}

function stopUpdatePolling() {
    if (updatePollTimer) clearInterval(updatePollTimer);
    updatePollTimer = null;
}

// ---------------------------------------------------------------------------
// Restart guard side: after requesting a restart, keep watching for "whether the new process has started accepting clients", and once it does, refresh the whole page.
// ---------------------------------------------------------------------------

// Only acknowledge the fact that a new process has already responded: the session only lives in the old process's memory, and the new process will only return 401 for old tokens.
// 200 means we're still talking to the old process (it hasn't exited yet), can't connect/502 means it's exiting or hasn't come up yet — keep waiting either way.
// Stop the timer silently once the console is left: don't drag users who've already moved on to other pages back to the update page.
let updateWatchdogProbing = false;

async function watchdogProbe() {
    if (!isUpdateConsoleActive()) {
        stopUpdateWatchdog();
        return;
    }
    if (updateWatchdogProbing) return;
    updateWatchdogProbing = true;
    let status = 0;
    try {
        const resp = await fetch('/api/system/update', {
            method: 'GET',
            credentials: 'same-origin',
            cache: 'no-store',
            headers: { Accept: 'application/json' },
        });
        status = resp.status;
    } catch (err) {
        status = 0;
    } finally {
        updateWatchdogProbing = false;
    }
    if (status === 401) {
        // The new process is already serving clients and doesn't recognize our session: a full page refresh lands on the new version, and with the hash it returns to this page.
        // Stop the timer first: the page is about to be completely replaced, so the timer shouldn't get a second action.
        stopUpdateWatchdog();
        location.replace(location.pathname + '?restarted=' + Date.now() + '#system-update');
        return;
    }
    if (updateWatchdogSlow === null && Date.now() > updateWatchdogDeadline) {
        updateWatchdogSlow = status === 200 ? 'running' : 'down';
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
        // The page has already switched away. router.js only calls initPage on entry; on exit, no one calls it,
        // So here it stops itself, without leaving a timer.
        stopUpdatePolling();
        return;
    }
    // Transport layer death (connection refused, process already exited) is a reject rather than a response with a status code: the two here
    // Unify — if a restart is in progress, it is just "process exited"; otherwise it is the read failure to be reported.
    let r;
    try {
        r = await runUpdateRequest('GET', '/api/system/update/job');
    } catch (err) {
        r = { ok: false, error: err && err.message ? err.message : String(err) };
    }
    if (!r.ok) {
        stopUpdatePolling();
        if (updateRestartPending) {
            // The process exited as requested: this is not a "failed to read progress", it's a restart entering the next act.
            startUpdateWatchdog();
            return;
        }
        notify(updateT('jobPollFailed', { reason: r.error }), 'error');
        return;
    }
    const job = r.data && r.data.job ? r.data.job : null;
    if (updateConsoleState) updateConsoleState.job = job;
    renderUpdateConsole();
    if (job && job.state !== 'running') {
        stopUpdatePolling();
        if (job.state === 'succeeded' && job.restartRequested) {
            // The process is about to make way for the new binary: first broadcast the conclusion, then enter self-recovery view; never look back
            // Goes to read a service that is about to disappear (reportFinishedJob will read it, skip it here).
            notify(updateDoneToast(job.result || null), 'success');
            startUpdateWatchdog();
            return;
        }
        updateRestartPending = false;
        reportFinishedJob(job);
    }
}

// This sentence at completion only mentions the conclusion written down by the server side, but a patch compile and a normal update are two kinds of completion: commits=0 yet changed
// In binary, reciting "a1b2c3d → a1b2c3d" is the same as saying nothing; you have to say "patch compilation complete".
function updateDoneToast(result) {
    if (result && result.binaryBuilt && result.fromCommit && result.fromCommit === result.toCommit) {
        return updateT('buildDoneToast');
    }
    const tail = result && result.toCommit ? ' ' + result.fromCommit + ' → ' + result.toCommit : '';
    return updateT('jobDoneToast') + tail;
}

// The broadcast only reads the conclusion written down by the server side: on success, read the two commit numbers of result, on failure, read failure.message,
// The page itself does not write "update complete" here.
function reportFinishedJob(job) {
    const result = job.result || null;
    if (job.state === 'succeeded') {
        notify(updateDoneToast(result), 'success');
        if (result && result.needsRestart) notify(updateT('needsRestartToast'), 'info');
    } else {
        const reason = (job.failure && job.failure.message) ? job.failure.message : updateT('failureNoMessage');
        notify(updateT('jobFailedToast') + ': ' + reason, 'error');
    }
    resetUpdateCheck();
    // The binary has changed, so the local machine state must be re-read; if it can't be read, say so honestly.
    fetchUpdateStatus().then(renderUpdateConsole).catch(err => {
        notify(updateT(UPDATE_FAILURE_KEYS.load, { reason: err.message }), 'error');
    });
}

// ---------------------------------------------------------------------------
// action
// ---------------------------------------------------------------------------

async function checkForUpdates() {
    await withUpdateBusy(async () => {
        updateChecking = true;
        renderUpdateConsole();
        let r;
        try {
            r = await runUpdateRequest('POST', '/api/system/update/check');
        } finally {
            // Even if the request itself blows up (401, network dropped), the "checking" state must also be taken down, otherwise the button stays gray forever.
            updateChecking = false;
        }
        if (!r.ok) {
            // A failure at the HTTP layer is likewise a "cannot query" error; we must not leave a page that looks like "no updates".
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

// If a button won't respond to clicks, you must be able to explain why; "grayed out" doesn't count as an explanation.
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

function updateRestartChoiceChanged(checked) {
    updateRestartChoice = !!checked;
    updateRestartChoiceSet = true; // Once the operator has made their own decision, the server-side default value no longer overrides it.
    renderUpdateConsole();
}

// The checkbox is the single source of truth; the copy in the module is just a memory used for polling redraws.
function updateRestartRequested() {
    const box = document.getElementById('update-restart-choice');
    if (box && typeof box.checked === 'boolean') return box.checked;
    return updateRestartChoice;
}

function updateConfirmText(status, restart) {
    // Back-compiling and "update N commits" are two different things: a confirmation box saying "0 new commits" would make one think nothing happened.
    const base = (!status.updateAvailable && status.buildPending)
        ? updateT('confirmBuildPending', { commit: status.commit || '-' })
        : updateT('confirmApply', {
            repo: status.repo || '-',
            target: status.remoteCommit || '-',
            commit: status.commit || '-',
            n: updateBehindCount(status),
        });
    return base + ' ' + (restart ? updateT('confirmRestartYes') : updateT('confirmRestartNo'));
}

async function startUpdateApply() {
    if (updateBusy) return;
    const status = updateStatusOf();
    const blockers = updateApplyBlockers(status, updateJobOf());
    if (blockers.length) {
        // The browser won't deliver a disabled button's click here, so reaching this point means the state is already out of sync with the view,
        // Then just say truthfully why it can't be clicked; staying silent makes it look more like it's broken.
        notify(blockers[0], 'error');
        return;
    }
    const restart = updateRestartRequested();
    if (!window.confirm(updateConfirmText(status, restart))) return;

    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/apply', { restart: restart });
        if (r.status === 409 && r.data.job) {
            // Another update is really running: continue with its progress and keep watching it, that is the "current state".
            if (updateConsoleState) updateConsoleState.job = r.data.job;
            renderUpdateConsole();
            startUpdatePolling();
        }
        if (!r.ok) throw new Error(r.error);
        resetUpdateCheck();
        if (restart) updateRestartPending = true;
        startUpdatePolling();
        notify(updateT('applyAccepted', { id: r.data.job_id || '-' }), 'success');
        await pollUpdateJob();
    }, 'apply');
}

// 「Immediately restart the service」: turn the binary that has already been swapped in on disk into the one that is currently answering. The server side only allows in
// Indeed, let it through when there's a version pending to take effect — if there's nothing that can take effect, then it's just stopping the service, which isn't called a restart.
async function startRestartNow() {
    if (updateBusy) return;
    if (!window.confirm(updateT('restartNowConfirm'))) return;

    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/restart');
        if (!r.ok) throw new Error(r.error);
        stopUpdatePolling();
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
            // The server even tells you "which files weren't rolled back", so we can't just report a single failure message.
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

// Update source: the server side is responsible for validation and writing (the single repo field in the update section of config.yaml), here we only do the plumbing and explanation.
// Leaving the commit empty = reverting to the official repository, which is the same state as "never configured".
async function saveUpdateSource() {
    if (updateBusy) return;
    const body = {
        repo: updateSourceRepo().trim(),
    };
    updateSourceError = '';
    await withUpdateBusy(async () => {
        const r = await runUpdateRequest('POST', '/api/system/update/source', body);
        if (!r.ok) {
            // The server-side rejection text (invalid address...) is exactly the message to display; don't compose a separate one here.
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

// Preview has no side effects (the server fetches in a temporary repo), so you can click first, view the list, and then decide.
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
    const restart = updateRestartRequested();
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
        if (restart) updateRestartPending = true;
        startUpdatePolling();
        notify(updateT('adoptAccepted', { id: r.data.job_id || '-' }), 'success');
        await pollUpdateJob();
    }, 'adopt');
}

// ---------------------------------------------------------------------------
// render
// ---------------------------------------------------------------------------

function updateSection(title, body) {
    return '<section class="update-section"><h3 class="update-section-title">' +
        escapeHtml(title) + '</h3>' + body + '</section>';
}

// The value goes into both the text and the title (long paths need the title to be seen fully), so the copy in the attribute must go through escapeAttr.
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
    // Manual input errors belong to the page's own state, and re-rendering (polling) cannot wipe them away.
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
    parts.push(renderUpdateRestartChoice());
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
        // This branch is where the "no lying" rule lands: if there's a reason, write only the reason, without mentioning "already up to date".
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
        // incoming is a truncated list, incomingTotal is the true count, both must be stated.
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
    // The line background color is the division of "whether this file will be overwritten by an update": local content is green, product source code is red.
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

function renderUpdateRestartChoice() {
    if (!updateCanRestart()) {
        // If the backend doesn't provide a restart hook, the checkbox shouldn't appear: offering an option that won't be honored is deceiving people.
        return '<div class="update-hint">' + escapeHtml(updateT('restartNotWired')) + '</div>';
    }
    const preview = updateRestartChoice ? updateT('confirmRestartYes') : updateT('confirmRestartNo');
    return '<label class="update-restart-choice">' +
        '<input type="checkbox" id="update-restart-choice"' +
        (updateRestartChoice ? ' checked' : '') +
        ' onchange="updateRestartChoiceChanged(this.checked)">' +
        '<span>' + escapeHtml(updateT('restartChoiceLabel')) + '</span></label>' +
        '<div class="update-hint update-restart-preview">' + escapeHtml(preview) + '</div>';
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
    // The source is already at the tip of the update source, just one compile away: this is not "no updates available", the button must remain clickable as usual, and
    // Need to make it clear that it will go and recompile — otherwise that "click update once more" phrase in no
    if (status.rollbackPending) {
        parts.push('<div class="update-warn">' + escapeHtml(updateT('rollbackPendingHint')) + '</div>');
    }
    if (status.buildPending && status.installed) {
        parts.push('<div class="update-warn">' + escapeHtml(updateT('buildPendingHint')) + '</div>');
    }

    // Both halves matter: `disabled` for the browser, `data-state-disabled` so the platform's
    // permission gate (which writes el.disabled for every [data-require-permission] element)
    // does not re-enable the button for a user who holds update:apply. In a real browser that
    // write produced an enabled one-click update directly under "There are 53 product source code files that have been locally modified".
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

    parts.push(renderUpdateRestartChoice());

    if (blocked) {
        parts.push('<ul class="update-blocker-list">' + blockers.map(b =>
            '<li class="update-error">' + escapeHtml(b) + '</li>').join('') + '</ul>');
    }

    const local = status.localChanges || [];
    const blocking = status.blockingChanges || [];
    // localChanges is usually a superset of blockingChanges, but in case the two are inconsistent, the files that block updates must never therefore
    // Not listed out: the file names that the above "N files have been locally modified" message specifically calls out are exactly the ones that need to be handled.
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

// Pending banner: the binary on disk is no longer the one this process is running (update/rollback ran but no restart, or the CLI was swapped),
// Here we provide a "make it take effect right now" escape hatch — otherwise, if you forget to check it even once, the command line is the only path left.
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
        '<button class="btn-primary update-restart-btn" data-require-permission="update:apply" onclick="startRestartNow()">' +
        escapeHtml(updateT('restartNowBtn')) + '</button></div>';
}

// Self-recovery view: after the restart request is issued, this is what the console renders, until the new process's response triggers a full page refresh.
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
    }
    parts.push('<div class="update-overlay-actions">' +
        '<button class="btn-secondary" type="button" onclick="refreshUpdatePage()">' +
        escapeHtml(updateT('restartOverlayRefresh')) + '</button></div>');
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
    // The red text of the action failure is a state rather than a one-shot injection: redraw (polling, the one that exits the busy state) cannot erase it.
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
window.updateRestartChoiceChanged = updateRestartChoiceChanged;
window.stopUpdatePolling = stopUpdatePolling;
window.renderUpdateConsole = renderUpdateConsole;
window.saveUpdateSource = saveUpdateSource;
window.previewAdoptSource = previewAdoptSource;
window.confirmAdoptSource = confirmAdoptSource;
window.updateSourceRepoChanged = updateSourceRepoChanged;
