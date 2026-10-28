/* global chrome, loadConfig, saveConfig, baseUrlFrom, ensureHostPermission,
   loginAndValidate, validateTokenSession, fetchCatalogCached, invalidateCatalogCache, streamTest,
   cancelByConversationId, extractConversationId, AGENT_MODES, toPrompt,
   defaultInstruction, markdownToHtml, CSAI_LIMITS, isExtensionContextError */

let config = {};
let entries = [];
let runs = [];
let selectedEntryId = null;
let selectedRunId = null;
let inspectedTabId = null;
let token = '';
let tokenExpiresAt = '';
let bgPort = null;
let activeAbort = null;
let activeRunId = null;
let validateAbort = null;
let validating = false;
let captureEnabled = true;
let searchDebounceTimer = null;
let connectRetryTimer = null;
let tokenCheckTimer = null;
let probeInFlight = null;
let lastProbeAt = 0;

const TOKEN_TICK_INTERVAL_MS = 30 * 1000;
const PROBE_MIN_GAP_MS = 5000;
let outputFlushScheduled = false;
let pendingProgressRunId = null;
let pendingFinalRunId = null;
let finalRenderedLen = 0;
let markdownRenderToken = 0;

const $ = (id) => document.getElementById(id);

function extensionAlive() {
  try {
    return !!(typeof chrome !== 'undefined' && chrome.runtime && chrome.runtime.id);
  } catch (_) {
    return false;
  }
}

function stopOnContextLoss() {
  if (tokenCheckTimer) {
    clearInterval(tokenCheckTimer);
    tokenCheckTimer = null;
  }
  if (connectRetryTimer) {
    clearTimeout(connectRetryTimer);
    connectRetryTimer = null;
  }
  bgPort = null;
  showContextBanner(true);
}

function onContextLoss(err) {
  if (!isExtensionContextError(err)) return false;
  stopOnContextLoss();
  return true;
}

function showContextBanner(show) {
  const el = $('ctx-banner');
  if (el) el.classList.toggle('hidden', !show);
}

function scheduleConnect(delayMs) {
  if (connectRetryTimer) clearTimeout(connectRetryTimer);
  connectRetryTimer = setTimeout(connectBackground, delayMs);
}

function runtimeSendMessage(msg) {
  if (!extensionAlive()) {
    showContextBanner(true);
    return Promise.reject(new Error('Extension context lost'));
  }
  return chrome.runtime.sendMessage(msg);
}

function setStatus(text, kind) {
  const el = $('status');
  el.textContent = text;
  el.className = 'status' + (kind ? ' status-' + kind : '');
  if (kind !== 'error') updateConnSummary();
}

function connectionEndpointLabel() {
  const scheme = $('https').checked ? 'https' : 'http';
  const host = ($('host').value || '').trim() || '127.0.0.1';
  const port = ($('port').value || '').trim() || '8080';
  return `${scheme}://${host}:${port}`;
}

function setConnExpanded(expanded) {
  const toolbar = $('toolbar');
  if (!toolbar) return;
  toolbar.classList.toggle('toolbar--conn-collapsed', !expanded);
  const btn = $('btn-conn-toggle');
  if (btn) {
    btn.setAttribute('aria-expanded', expanded ? 'true' : 'false');
    btn.textContent = expanded ? 'Collapse' : 'Connection';
  }
}

function updateConnSummary() {
  const summary = $('conn-summary');
  if (!summary) return;
  summary.textContent = connectionEndpointLabel();
  summary.classList.toggle('conn-summary--ok', !!token && !isTokenExpiredByTime(tokenExpiresAt));
}

function refreshAuthStatus() {
  if (!token) {
    setStatus('Not validated', '');
    updateConnSummary();
    return;
  }
  if (isTokenExpiredByTime(tokenExpiresAt)) {
    setStatus('Session expired — please Validate again', 'error');
    updateConnSummary();
    return;
  }
  const hint = formatTokenExpiryHint(tokenExpiresAt);
  const kind = tokenExpiresWithin(tokenExpiresAt, TOKEN_WARN_BEFORE_MS) ? 'warn' : 'ok';
  setStatus(hint, kind);
}

async function clearAuthSession(message) {
  token = '';
  tokenExpiresAt = '';
  try {
    await saveConfig({ token: '', tokenExpiresAt: '' });
  } catch (err) {
    if (onContextLoss(err)) return;
    throw err;
  }
  invalidateCatalogCache();
  setStatus(message || 'Not validated', message ? 'error' : '');
  setConnExpanded(true);
  updateConnSummary();
  updateSendButtons();
}

async function handleAuthFailure(err, fallbackMessage) {
  if (err && isAuthHttpStatus(err.status)) {
    await clearAuthSession(fallbackMessage || 'Token invalid or expired — please Validate again');
    return true;
  }
  return false;
}

async function ensureAuthReady() {
  if (!token) {
    setStatus('Validate first', 'warn');
    setConnExpanded(true);
    return false;
  }
  if (isTokenExpiredByTime(tokenExpiresAt)) {
    await clearAuthSession('Session expired — please Validate again');
    return false;
  }
  try {
    await probeTokenOnServer(true);
    return !!token;
  } catch (_) {
    return false;
  }
}

function startTokenWatch() {
  if (tokenCheckTimer) clearInterval(tokenCheckTimer);
  tokenCheckTimer = setInterval(async () => {
    if (!extensionAlive()) {
      stopOnContextLoss();
      return;
    }
    if (!token) return;
    if (isTokenExpiredByTime(tokenExpiresAt)) {
      await clearAuthSession('Session expired — please Validate again');
      return;
    }
    await probeTokenOnServer();
  }, TOKEN_TICK_INTERVAL_MS);
}

async function probeTokenOnServer(force) {
  if (!extensionAlive()) {
    stopOnContextLoss();
    return;
  }
  if (!token || isTokenExpiredByTime(tokenExpiresAt)) return;
  const now = Date.now();
  if (!force && now - lastProbeAt < PROBE_MIN_GAP_MS) return;
  if (probeInFlight) return probeInFlight;

  probeInFlight = (async () => {
    try {
      const baseUrl = baseUrlFrom(await loadConfig());
      await validateTokenSession(baseUrl, token);
      lastProbeAt = Date.now();
      refreshAuthStatus();
    } catch (err) {
      if (onContextLoss(err)) return;
      lastProbeAt = Date.now();
      if (await handleAuthFailure(err, 'Server restarted or token invalid — please Validate again')) {
        return;
      }
      if (isNetworkFetchError(err)) {
        setStatus('Cannot reach server — confirm CyberStrikeAI is running', 'warn');
        updateConnSummary();
        return;
      }
      setStatus('Validation failed: ' + err.message, 'warn');
      updateConnSummary();
    } finally {
      probeInFlight = null;
    }
  })();

  return probeInFlight;
}

function setupAuthProbeHooks() {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && token) {
      probeTokenOnServer(true).catch(() => {});
    }
  });
  window.addEventListener('focus', () => {
    if (token) probeTokenOnServer(true).catch(() => {});
  });
}

function onConnToggle() {
  const toolbar = $('toolbar');
  const collapsed = toolbar && toolbar.classList.contains('toolbar--conn-collapsed');
  setConnExpanded(!!collapsed);
}

function updateCaptureToggleUi() {
  const btn = $('btn-capture-toggle');
  if (btn) {
    btn.classList.toggle('btn--capture-on', captureEnabled);
    btn.classList.toggle('btn--capture-off', !captureEnabled);
    btn.setAttribute('aria-pressed', captureEnabled ? 'true' : 'false');
    btn.textContent = captureEnabled ? '● Capturing' : '○ Paused';
  }
  const sidebar = $('sidebar');
  if (sidebar) sidebar.classList.toggle('capture-paused', !captureEnabled);
  const hint = $('capture-paused-hint');
  if (hint) hint.classList.toggle('hidden', captureEnabled);
}

async function syncCaptureEnabled() {
  try {
    await saveConfig({ captureEnabled });
  } catch (err) {
    if (onContextLoss(err)) return;
    throw err;
  }
  try {
    await runtimeSendMessage({ type: 'set-capture-enabled', enabled: captureEnabled });
  } catch (_) {}
  updateCaptureToggleUi();
}

async function onCaptureToggle() {
  captureEnabled = !captureEnabled;
  await syncCaptureEnabled();
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

function truncateOutput(s, max) {
  if (!s || s.length <= max) return s || '';
  return s.slice(0, max) + '\n\n… [output truncated at ' + max + ' chars]';
}

function selectedEntry() {
  return entries.find((e) => e.id === selectedEntryId) || null;
}

function selectedRun() {
  return runs.find((r) => r.id === selectedRunId) || null;
}

function statusDotClass(status) {
  switch (status) {
    case 'running': return 'dot-running';
    case 'done': return 'dot-done';
    case 'error': return 'dot-error';
    case 'cancelled':
    case 'cancelling': return 'dot-cancelled';
    default: return '';
  }
}

function createRun(entry, meta) {
  const run = {
    id: 'run_' + Date.now() + '_' + Math.random().toString(36).slice(2, 6),
    title: entry ? entry.title : 'Unknown',
    status: 'running',
    meta: meta || '',
    progress: '',
    final: '',
    entry: entry ? { ...entry } : null,
    conversationId: '',
  };
  runs.unshift(run);
  if (runs.length > CSAI_LIMITS.MAX_RUNS) runs.length = CSAI_LIMITS.MAX_RUNS;
  selectedRunId = run.id;
  return run;
}

function buildRunLi(r) {
  const li = document.createElement('li');
  li.dataset.id = r.id;
  if (r.id === selectedRunId) li.classList.add('selected');
  li.innerHTML =
    `<span class="dot ${statusDotClass(r.status)}"></span>` +
    `<div class="run-body">` +
    `<div class="run-title">${escapeHtml(r.title)}</div>` +
    `<div class="meta">${escapeHtml(r.meta)} · ${escapeHtml(r.status)}</div>` +
    `</div>`;
  li.addEventListener('click', () => selectRun(r.id));
  return li;
}

function renderRunList() {
  const ul = $('run-list');
  ul.innerHTML = '';
  for (const r of runs) {
    ul.appendChild(buildRunLi(r));
  }
}

function prependRunItem(run) {
  const ul = $('run-list');
  ul.insertBefore(buildRunLi(run), ul.firstChild);
  while (ul.children.length > runs.length) {
    ul.removeChild(ul.lastChild);
  }
}

function updateRunListItem(runId) {
  const r = runs.find((x) => x.id === runId);
  const ul = $('run-list');
  const li = ul.querySelector(`li[data-id="${CSS.escape(runId)}"]`);
  if (!r || !li) {
    renderRunList();
    return;
  }
  const selected = r.id === selectedRunId;
  li.className = selected ? 'selected' : '';
  li.innerHTML =
    `<span class="dot ${statusDotClass(r.status)}"></span>` +
    `<div class="run-body">` +
    `<div class="run-title">${escapeHtml(r.title)}</div>` +
    `<div class="meta">${escapeHtml(r.meta)} · ${escapeHtml(r.status)}</div>` +
    `</div>`;
  li.onclick = () => selectRun(r.id);
}

function currentSearchQuery() {
  return ($('search').value || '').trim().toLowerCase();
}

function entryMatchesSearch(e, q) {
  return !q || (e.url || '').toLowerCase().includes(q) || (e.title || '').toLowerCase().includes(q);
}

function buildRequestLi(e) {
  const li = document.createElement('li');
  li.dataset.id = e.id;
  if (e.id === selectedEntryId) li.classList.add('selected');
  const st = e.responseStatus || 0;
  const stClass = st >= 200 && st < 400 ? 'status-ok' : 'status-err';
  li.innerHTML =
    `<span class="method">${escapeHtml(e.method)}</span>` +
    `<span class="${stClass}">${st || '—'}</span>` +
    `<span class="req-path">${escapeHtml(e.title || e.url)}</span>`;
  li.addEventListener('click', () => selectEntry(e.id));
  return li;
}

function renderRequestList() {
  const q = currentSearchQuery();
  const ul = $('request-list');
  ul.innerHTML = '';
  const filtered = entries.filter((e) => entryMatchesSearch(e, q));
  for (const e of filtered) {
    ul.appendChild(buildRequestLi(e));
  }
  updateSendButtons();
}

function prependRequestEntry(entry) {
  const q = currentSearchQuery();
  if (!entryMatchesSearch(entry, q)) return;
  const ul = $('request-list');
  ul.insertBefore(buildRequestLi(entry), ul.firstChild);
  while (ul.children.length > CSAI_LIMITS.MAX_CAPTURED) {
    ul.removeChild(ul.lastChild);
  }
  updateSendButtons();
}

function updateRequestSelection() {
  $('request-list').querySelectorAll('li').forEach((li) => {
    li.classList.toggle('selected', li.dataset.id === selectedEntryId);
  });
}

function updateSendButtons() {
  const ok = !!token;
  $('btn-send').disabled = !selectedEntryId || !ok;
  $('btn-latest-xhr').disabled = !ok;
}

function selectEntry(id) {
  selectedEntryId = id;
  const e = selectedEntry();
  updateRequestSelection();
  if (!e) return;
  $('view-request').textContent = formatRawRequest(e);
  $('view-response').textContent = formatRawResponse(e);
}

function updateRunSelection(prevId, nextId) {
  const ul = $('run-list');
  if (prevId && prevId !== nextId) {
    const prevLi = ul.querySelector(`li[data-id="${CSS.escape(prevId)}"]`);
    if (prevLi) prevLi.classList.remove('selected');
  }
  if (nextId) {
    const nextLi = ul.querySelector(`li[data-id="${CSS.escape(nextId)}"]`);
    if (nextLi) nextLi.classList.add('selected');
  }
}

function trimInactiveRunFinals(activeId) {
  const max = CSAI_LIMITS.MAX_FINAL_ARCHIVE_CHARS;
  for (const r of runs) {
    if (r.id === activeId || r.status === 'running') continue;
    if (r.final && r.final.length > max) {
      r.final = r.final.slice(0, max) + '\n\n… [inactive run truncated at ' + max + ' chars]';
    }
  }
}

function selectRun(id) {
  const prevId = selectedRunId;
  selectedRunId = id;
  if (prevId !== id) {
    updateRunSelection(prevId, id);
    trimInactiveRunFinals(id);
  }
  const r = selectedRun();
  if (!r) return;
  flushOutputViewNow();
  refreshFinalView(r.final, { streaming: r.status === 'running' && activeRunId === r.id });
  if (r.entry) {
    $('view-request').textContent = formatRawRequest(r.entry);
    $('view-response').textContent = formatRawResponse(r.entry);
  }
}

function formatRawRequest(e) {
  return formatRequestDisplay(e);
}

function formatRawResponse(e) {
  return formatResponseDisplay(e);
}

function resetFinalDom(finalText) {
  const raw = $('final-raw');
  raw.textContent = finalText || '';
  finalRenderedLen = (finalText || '').length;
}

function appendFinalDom(delta) {
  if (!delta) return;
  const raw = $('final-raw');
  raw.append(document.createTextNode(delta));
  finalRenderedLen += delta.length;
  raw.scrollTop = raw.scrollHeight;
}

function refreshFinalView(finalText, options) {
  const opts = options || {};
  const streaming = !!opts.streaming;
  const text = finalText || '';
  const raw = $('final-raw');
  const md = $('final-md');
  const wantMd =
    $('render-md').checked &&
    text.trim() &&
    text.length <= CSAI_LIMITS.MAX_MARKDOWN_CHARS;

  if (streaming) {
    md.classList.add('hidden');
    raw.classList.remove('hidden');
    if (text.length < finalRenderedLen) {
      resetFinalDom(text);
    } else if (text.length > finalRenderedLen) {
      appendFinalDom(text.slice(finalRenderedLen));
    }
    return;
  }

  resetFinalDom(text);
  markdownRenderToken += 1;
  const token = markdownRenderToken;

  if (wantMd) {
    const renderMd = () => {
      if (token !== markdownRenderToken) return;
      md.classList.remove('hidden');
      raw.classList.add('hidden');
      md.srcdoc = markdownToHtml(text);
    };
    if (typeof requestIdleCallback === 'function') {
      requestIdleCallback(renderMd, { timeout: 500 });
    } else {
      setTimeout(renderMd, 0);
    }
    return;
  }

  md.classList.add('hidden');
  raw.classList.remove('hidden');
  if (text.length > CSAI_LIMITS.MAX_MARKDOWN_CHARS && $('render-md').checked) {
    raw.textContent =
      text + '\n\n… [Markdown disabled: content exceeds ' + CSAI_LIMITS.MAX_MARKDOWN_CHARS + ' chars]';
  }
}

function finalizeFinalView(finalText) {
  flushOutputViewNow();
  refreshFinalView(finalText, { streaming: false });
}

function scheduleOutputFlush() {
  if (outputFlushScheduled) return;
  outputFlushScheduled = true;
  requestAnimationFrame(flushOutputView);
}

function flushOutputView() {
  outputFlushScheduled = false;
  if (pendingProgressRunId != null) {
    const rid = pendingProgressRunId;
    pendingProgressRunId = null;
    if (rid === selectedRunId) {
      const r = runs.find((x) => x.id === rid);
      if (r) {
        const el = $('progress');
        el.textContent = r.progress;
        el.scrollTop = el.scrollHeight;
      }
    }
  }
  if (pendingFinalRunId != null) {
    const rid = pendingFinalRunId;
    pendingFinalRunId = null;
    if (rid === selectedRunId) {
      const r = runs.find((x) => x.id === rid);
      if (r) {
        refreshFinalView(r.final, { streaming: isRunStreaming(rid) });
      }
    }
  }
}

function flushOutputViewNow() {
  outputFlushScheduled = false;
  if (pendingProgressRunId != null || pendingFinalRunId != null) {
    const p = pendingProgressRunId;
    const f = pendingFinalRunId;
    pendingProgressRunId = null;
    pendingFinalRunId = null;
    if (p === selectedRunId) {
      const r = runs.find((x) => x.id === p);
      if (r) {
        $('progress').textContent = r.progress;
        $('progress').scrollTop = $('progress').scrollHeight;
      }
    }
    if (f === selectedRunId) {
      const r = runs.find((x) => x.id === f);
      if (r) {
        refreshFinalView(r.final, { streaming: isRunStreaming(f) });
      }
    }
  }
}

function isRunStreaming(runId) {
  if (runId !== activeRunId) return false;
  const r = runs.find((x) => x.id === runId);
  return !!(r && r.status === 'running');
}

function appendToRun(runId, field, text) {
  const r = runs.find((x) => x.id === runId);
  if (!r || !text) return;
  if (field === 'progress') {
    r.progress = truncateOutput(r.progress + text, CSAI_LIMITS.MAX_PROGRESS_CHARS);
  } else {
    r[field] = (r[field] || '') + text;
  }
  if (runId === selectedRunId) {
    if (field === 'progress') {
      pendingProgressRunId = runId;
      scheduleOutputFlush();
    } else if (field === 'final') {
      pendingFinalRunId = runId;
      scheduleOutputFlush();
    }
  }
}

function setRunStatus(runId, status) {
  const r = runs.find((x) => x.id === runId);
  if (!r) return;
  if (r.status === status) return;
  r.status = status;
  updateRunListItem(runId);
  if (runId === selectedRunId && status !== 'running') {
    finalizeFinalView(r ? r.final : '');
  }
}

function setRunConversationId(runId, cid) {
  const r = runs.find((x) => x.id === runId);
  if (r && cid) r.conversationId = cid;
}

async function initConfig() {
  try {
    config = await loadConfig();
  } catch (err) {
    if (onContextLoss(err)) return;
    throw err;
  }
  token = config.token || '';
  tokenExpiresAt = config.tokenExpiresAt || '';
  $('host').value = config.host;
  $('port').value = config.port;
  $('https').checked = config.https;
  $('filter-api').checked = config.filterApiOnly;
  captureEnabled = config.captureEnabled !== false;
  $('render-md').checked = config.renderMarkdown;
  $('debug-events').checked = config.showDebugEvents;

  if (token && isTokenExpiredByTime(tokenExpiresAt)) {
    await clearAuthSession('Session expired — please Validate again');
  } else {
    refreshAuthStatus();
    if (token) {
      startTokenWatch();
      probeTokenOnServer().catch(() => {});
    }
  }

  setConnExpanded(!token);
  updateCaptureToggleUi();
  syncCaptureEnabled().catch(() => {});
  updateSendButtons();
}

async function persistConnection() {
  try {
    await saveConfig(connectionConfigFromForm());
  } catch (err) {
    onContextLoss(err);
  }
}

function connectionConfigFromForm() {
  return {
    host: $('host').value.trim(),
    port: $('port').value.trim(),
    https: $('https').checked,
    filterApiOnly: $('filter-api').checked,
    renderMarkdown: $('render-md').checked,
    showDebugEvents: $('debug-events').checked,
  };
}

async function onValidate() {
  if (validating) {
    validateAbort?.abort();
    validating = false;
    $('btn-validate').textContent = 'Validate';
    setStatus('Cancelled', 'warn');
    return;
  }
  validating = true;
  validateAbort = new AbortController();
  $('btn-validate').textContent = 'Cancel';
  setStatus('Validating...', 'pending');

  const nextConfig = connectionConfigFromForm();
  const baseUrl = baseUrlFrom(nextConfig);
  const password = $('password').value;
  let hostPermission;
  try {
    // Invoke before the first await so Chrome still associates the optional
    // permission prompt with the Validate button's user gesture.
    hostPermission = ensureHostPermission(baseUrl);
  } catch (err) {
    hostPermission = Promise.reject(err);
  }

  try {
    await hostPermission;
    await saveConfig(nextConfig);
    config = { ...config, ...nextConfig };
    const auth = await loginAndValidate(baseUrl, password, validateAbort.signal);
    token = auth.token;
    tokenExpiresAt = auth.expiresAt || '';
    await saveConfig({ token, tokenExpiresAt });
    invalidateCatalogCache();
    refreshAuthStatus();
    startTokenWatch();
    setConnExpanded(false);
    updateSendButtons();
  } catch (err) {
    if (onContextLoss(err)) return;
    if (err.name === 'AbortError') {
      setStatus('Cancelled', 'warn');
    } else {
      token = '';
      tokenExpiresAt = '';
      try {
        await saveConfig({ token: '', tokenExpiresAt: '' });
      } catch (saveErr) {
        onContextLoss(saveErr);
      }
      setStatus('Failed: ' + err.message, 'error');
      setConnExpanded(true);
    }
    updateSendButtons();
  } finally {
    validating = false;
    validateAbort = null;
    $('btn-validate').textContent = 'Validate';
  }
}

function fillAgentSelect(sel, selected) {
  fillSelect(sel, AGENT_MODES.map((m) => ({ id: m.id, label: m.label })), selected || 'eino_single');
}

function csSelectWrap(input) {
  return input && input.closest ? input.closest('.cs-select') : null;
}

function closeAllCsSelects(except) {
  document.querySelectorAll('.cs-select.open').forEach((wrap) => {
    if (except && wrap === except) return;
    wrap.classList.remove('open');
    const trigger = wrap.querySelector('.cs-select-trigger');
    const menu = wrap.querySelector('.cs-select-menu');
    if (trigger) trigger.setAttribute('aria-expanded', 'false');
    if (menu) menu.hidden = true;
  });
}

function fillSelect(sel, items, value) {
  const wrap = csSelectWrap(sel);
  const has = items.some((i) => i.id === value);
  const resolved = has ? value : (items[0] && items[0].id) || '';
  const selectedItem = items.find((i) => i.id === resolved) || items[0];

  sel.value = resolved;

  if (!wrap) {
    // Fallback for plain <select> if any remain
    sel.innerHTML = '';
    for (const item of items) {
      const opt = document.createElement('option');
      opt.value = item.id;
      opt.textContent = item.label;
      sel.appendChild(opt);
    }
    sel.value = resolved;
    return;
  }

  const valueEl = wrap.querySelector('.cs-select-value');
  const menu = wrap.querySelector('.cs-select-menu');
  if (valueEl) valueEl.textContent = (selectedItem && selectedItem.label) || '—';
  if (!menu) return;

  menu.innerHTML = '';
  for (const item of items) {
    const li = document.createElement('li');
    li.setAttribute('role', 'option');
    li.className = 'cs-select-option' + (item.id === resolved ? ' is-selected' : '');
    li.setAttribute('data-value', item.id);
    li.setAttribute('aria-selected', item.id === resolved ? 'true' : 'false');
    li.innerHTML =
      '<span class="cs-select-option-check" aria-hidden="true">✓</span>' +
      '<span class="cs-select-option-label"></span>';
    li.querySelector('.cs-select-option-label').textContent = item.label;
    li.addEventListener('click', (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      sel.value = item.id;
      if (valueEl) valueEl.textContent = item.label;
      menu.querySelectorAll('.cs-select-option').forEach((opt) => {
        const on = (opt.getAttribute('data-value') || '') === item.id;
        opt.classList.toggle('is-selected', on);
        opt.setAttribute('aria-selected', on ? 'true' : 'false');
      });
      closeAllCsSelects();
    });
    menu.appendChild(li);
  }
}

function setupCsSelects() {
  document.querySelectorAll('.cs-select').forEach((wrap) => {
    const trigger = wrap.querySelector('.cs-select-trigger');
    const menu = wrap.querySelector('.cs-select-menu');
    if (!trigger || !menu || trigger.dataset.csBound) return;
    trigger.dataset.csBound = '1';
    trigger.addEventListener('click', (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      const willOpen = !wrap.classList.contains('open');
      closeAllCsSelects();
      if (willOpen) {
        wrap.classList.add('open');
        trigger.setAttribute('aria-expanded', 'true');
        menu.hidden = false;
        const selected = menu.querySelector('.cs-select-option.is-selected');
        if (selected) selected.scrollIntoView({ block: 'nearest' });
      }
    });
  });

  document.addEventListener('click', (ev) => {
    if (ev.target.closest && ev.target.closest('.cs-select')) return;
    closeAllCsSelects();
  });

  document.addEventListener('keydown', (ev) => {
    if (ev.key === 'Escape') closeAllCsSelects();
  });

  const dlg = $('send-dialog');
  if (dlg) {
    dlg.addEventListener('close', () => closeAllCsSelects());
  }
}

async function openSendDialog(entryOverride) {
  const e = entryOverride || selectedEntry();
  if (!e) return;
  if (!(await ensureAuthReady())) return;
  const dlg = $('send-dialog');
  config = await loadConfig();
  const baseUrl = baseUrlFrom(config);

  $('dlg-instruction').value = config.lastInstruction || defaultInstruction();
  fillAgentSelect($('dlg-agent'), config.lastAgentMode);
  fillSelect($('dlg-project'), [{ id: '', label: 'Loading…' }], '');
  fillSelect($('dlg-role'), [{ id: '', label: 'Loading…' }], '');
  dlg.showModal();

  try {
    const { projects, roles } = await fetchCatalogCached(baseUrl, token);
    fillSelect($('dlg-project'), projects, config.lastProjectId);
    // "默认" is the legacy Chinese default-role name kept in older extension configs; "default" is the current name.
    const lastRole = (config.lastRole === '默认' || config.lastRole === 'default') ? '' : config.lastRole;
    fillSelect($('dlg-role'), roles, lastRole);
  } catch (err) {
    if (await handleAuthFailure(err, 'Token invalid or expired — please Validate again')) {
      dlg.close();
      return;
    }
    fillSelect($('dlg-project'), [{ id: '', label: '(load failed)' }], '');
    fillSelect($('dlg-role'), [{ id: '', label: 'Default' }], '');
  }

  dlg.dataset.entryId = e.id;
}

async function onSendConfirmed() {
  const entryId = $('send-dialog').dataset.entryId;
  const e = entries.find((x) => x.id === entryId) || selectedEntry();
  if (!e || !(await ensureAuthReady())) return;

  const instruction = $('dlg-instruction').value.trim() || defaultInstruction();
  const projectId = $('dlg-project').value || '';
  const role = $('dlg-role').value || '';
  const agentMode = $('dlg-agent').value || 'eino_single';

  try {
    await saveConfig({
      lastInstruction: instruction,
      lastProjectId: projectId,
      lastRole: role,
      lastAgentMode: agentMode,
    });
  } catch (err) {
    if (onContextLoss(err)) return;
    throw err;
  }

  const modeLabel = (AGENT_MODES.find((m) => m.id === agentMode) || {}).label || agentMode;
  const roleValueEl = document.querySelector('[data-cs-select="dlg-role"] .cs-select-value');
  const roleLabel = (roleValueEl && roleValueEl.textContent) || role || 'Default';
  const run = createRun(e, modeLabel + ' · ' + roleLabel);
  activeRunId = run.id;
  prependRunItem(run);
  finalRenderedLen = 0;
  markdownRenderToken += 1;
  selectRun(run.id);

  const baseUrl = baseUrlFrom(await loadConfig());
  const prompt = toPrompt(e, instruction);
  appendToRun(run.id, 'progress', `[*] ${e.title}\n[server] ${baseUrl}\n[mode] ${modeLabel}\n`);
  if (projectId) appendToRun(run.id, 'progress', `[project] ${projectId}\n`);
  appendToRun(run.id, 'progress', '\n');

  try {
    await streamTest(
      baseUrl,
      token,
      { message: prompt, role, projectId, agentMode },
      {
        setAbortController(ctrl) {
          activeAbort = ctrl;
        },
        onEvent(type, message, ev) {
          handleStreamEvent(run.id, type, message, ev);
        },
        onDone() {
          appendToRun(run.id, 'progress', '\n[done]\n');
          setRunStatus(run.id, 'done');
          activeAbort = null;
          activeRunId = null;
        },
      }
    );
  } catch (err) {
    if (err.name === 'AbortError') {
      appendToRun(run.id, 'progress', '\n[info] Local stream stopped.\n');
      setRunStatus(run.id, 'cancelled');
    } else {
      await handleAuthFailure(err, 'Token invalid or expired — please Validate again');
      appendToRun(run.id, 'progress', '\n[error] ' + err.message + '\n');
      setRunStatus(run.id, 'error');
    }
    activeAbort = null;
    activeRunId = null;
  }
}

function handleStreamEvent(runId, type, message, ev) {
  const debug = $('debug-events').checked;
  switch (type) {
    case 'response_start':
      appendToRun(runId, 'progress', '\n\n[Main reply]\n');
      break;
    case 'response_delta':
      if (message) appendToRun(runId, 'final', message);
      break;
    case 'response':
      if (message) appendToRun(runId, 'final', message);
      break;
    case 'eino_agent_reply_stream_start':
      appendToRun(runId, 'progress', '\n\n[Sub-agent reply]\n');
      break;
    case 'eino_agent_reply_stream_delta':
      if (message) appendToRun(runId, 'progress', message);
      break;
    case 'progress':
      appendToRun(runId, 'progress', '\n[progress] ' + message + '\n');
      setRunStatus(runId, 'running');
      break;
    case 'error':
      appendToRun(runId, 'progress', '\n[error] ' + message + '\n');
      setRunStatus(runId, 'error');
      break;
    case 'cancelled':
      appendToRun(runId, 'progress', '\n[cancelled] ' + message + '\n');
      setRunStatus(runId, 'cancelled');
      break;
    case 'conversation':
    case 'message_saved': {
      const cid = extractConversationId(ev);
      if (cid) setRunConversationId(runId, cid);
      if (type === 'conversation' && debug && message) {
        appendToRun(runId, 'progress', '\n[conversation] ' + message + '\n');
      }
      break;
    }
    case 'tool_call':
    case 'tool_result':
    case 'tool_result_delta':
      if (debug && message) appendToRun(runId, 'progress', '\n[' + type + '] ' + message + '\n');
      break;
    default:
      if (debug && message && !type.endsWith('_stream_delta') && !type.endsWith('_stream_start')) {
        appendToRun(runId, 'progress', '\n[' + type + '] ' + message + '\n');
      }
      break;
  }
}

async function onStop() {
  const runId = activeRunId || selectedRunId;
  const run = runs.find((x) => x.id === runId);
  if (!run) return;

  if (activeAbort) {
    activeAbort.abort();
    activeAbort = null;
  }

  setRunStatus(run.id, 'cancelling');
  appendToRun(run.id, 'progress', '\n[info] Stop requested (local stream + server cancel).\n');

  const cid = run.conversationId;
  if (!cid || !token) {
    if (!cid) {
      appendToRun(run.id, 'progress', '[info] conversationId not ready yet; only local stream was stopped.\n');
    }
    setRunStatus(run.id, 'cancelled');
    activeRunId = null;
    return;
  }

  const baseUrl = baseUrlFrom(await loadConfig());
  try {
    await cancelByConversationId(baseUrl, token, cid);
    appendToRun(run.id, 'progress', '[info] Server cancel acknowledged.\n');
    setRunStatus(run.id, 'cancelled');
  } catch (err) {
    appendToRun(run.id, 'progress', '[error] Server cancel failed: ' + err.message + '\n');
    setRunStatus(run.id, 'cancelled');
  }
  activeRunId = null;
}

function clearCurrentRunOutput() {
  pendingProgressRunId = null;
  pendingFinalRunId = null;
  outputFlushScheduled = false;
  finalRenderedLen = 0;
  markdownRenderToken += 1;
  const r = selectedRun();
  if (r) {
    r.progress = '';
    r.final = '';
    $('progress').textContent = '';
    refreshFinalView('');
  } else {
    $('progress').textContent = '';
    refreshFinalView('');
  }
}

function clearAllRuns() {
  runs = [];
  selectedRunId = null;
  pendingProgressRunId = null;
  pendingFinalRunId = null;
  outputFlushScheduled = false;
  finalRenderedLen = 0;
  markdownRenderToken += 1;
  renderRunList();
  $('progress').textContent = '';
  refreshFinalView('');
}

function connectBackground() {
  connectRetryTimer = null;

  if (!extensionAlive()) {
    showContextBanner(true);
    bgPort = null;
    return;
  }

  showContextBanner(false);

  try {
    if (!chrome.devtools || !chrome.devtools.inspectedWindow) {
      scheduleConnect(1000);
      return;
    }
    inspectedTabId = chrome.devtools.inspectedWindow.tabId;
    bgPort = chrome.runtime.connect({ name: 'cyberstrike-panel' });
  } catch (_) {
    if (!extensionAlive()) {
      showContextBanner(true);
      bgPort = null;
      return;
    }
    scheduleConnect(1000);
    return;
  }

  bgPort.postMessage({
    type: 'subscribe',
    tabId: inspectedTabId,
    filterApiOnly: $('filter-api').checked,
  });
  bgPort.onMessage.addListener((msg) => {
    if (msg.type === 'list') {
      entries = msg.entries || [];
      renderRequestList();
    } else if (msg.type === 'entry') {
      entries.unshift(msg.entry);
      if (entries.length > CSAI_LIMITS.MAX_CAPTURED) entries.length = CSAI_LIMITS.MAX_CAPTURED;
      prependRequestEntry(msg.entry);
    }
  });
  bgPort.onDisconnect.addListener(() => {
    bgPort = null;
    if (!extensionAlive()) {
      showContextBanner(true);
      return;
    }
    scheduleConnect(1000);
  });
}

async function onFilterApiChange() {
  await persistConnection();
  if (bgPort && inspectedTabId != null) {
    bgPort.postMessage({
      type: 'subscribe',
      tabId: inspectedTabId,
      filterApiOnly: $('filter-api').checked,
    });
    runtimeSendMessage({
      type: 'set-filter-api',
      tabId: inspectedTabId,
      filterApiOnly: $('filter-api').checked,
    }).catch(() => {});
  }
}

async function clearCaptures() {
  if (inspectedTabId == null) return;
  await runtimeSendMessage({ type: 'clear-captures', tabId: inspectedTabId });
  entries = [];
  selectedEntryId = null;
  renderRequestList();
}

async function sendLatestXhr() {
  if (inspectedTabId == null) return;
  if (!(await ensureAuthReady())) return;
  const resp = await runtimeSendMessage({
    type: 'get-latest-api',
    tabId: inspectedTabId,
  });
  if (resp && resp.entry) {
    selectedEntryId = resp.entry.id;
    selectEntry(resp.entry.id);
    openSendDialog(resp.entry);
  }
}

function onSearchInput() {
  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(renderRequestList, 150);
}

function onCopyOutput() {
  const activeTab = document.querySelector('.tab.active');
  const tabName = activeTab ? activeTab.dataset.tab : 'output';
  let text = '';
  if (tabName === 'request') text = $('view-request').textContent;
  else if (tabName === 'response') text = $('view-response').textContent;
  else {
    const r = selectedRun();
    text = (r && r.final) ? r.final : $('final-raw').textContent;
  }
  navigator.clipboard.writeText(text || '').catch(() => {});
}

function setupTabs() {
  document.querySelectorAll('.tab').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.tab').forEach((b) => b.classList.remove('active'));
      document.querySelectorAll('.tab-panel').forEach((p) => p.classList.add('hidden'));
      btn.classList.add('active');
      $('tab-' + btn.dataset.tab).classList.remove('hidden');
    });
  });
}

$('btn-validate').addEventListener('click', onValidate);
$('btn-conn-toggle').addEventListener('click', onConnToggle);
$('host').addEventListener('input', updateConnSummary);
$('port').addEventListener('input', updateConnSummary);
$('https').addEventListener('change', updateConnSummary);
$('btn-send').addEventListener('click', () => openSendDialog());
$('btn-latest-xhr').addEventListener('click', sendLatestXhr);
$('btn-stop').addEventListener('click', onStop);
$('btn-copy').addEventListener('click', onCopyOutput);
$('btn-clear').addEventListener('click', clearCurrentRunOutput);
$('btn-clear-runs').addEventListener('click', clearAllRuns);
$('btn-clear-captures').addEventListener('click', clearCaptures);
$('search').addEventListener('input', onSearchInput);
$('btn-capture-toggle').addEventListener('click', onCaptureToggle);
$('filter-api').addEventListener('change', onFilterApiChange);
$('render-md').addEventListener('change', () => {
  const r = selectedRun();
  const text = r ? r.final : $('final-raw').textContent;
  const streaming = r && r.status === 'running' && activeRunId === r.id;
  refreshFinalView(text, { streaming });
  persistConnection();
});
$('debug-events').addEventListener('change', persistConnection);
$('send-form').addEventListener('submit', (ev) => {
  ev.preventDefault();
  $('send-dialog').close();
  onSendConfirmed();
});
$('dlg-cancel').addEventListener('click', () => $('send-dialog').close());

setupTabs();
setupCsSelects();
setupAuthProbeHooks();
initConfig().then(() => {
  if (extensionAlive()) connectBackground();
  else showContextBanner(true);
});
