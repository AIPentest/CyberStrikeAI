// 微信 iLink 机器人：扫码绑定与状态轮询

let wechatBindSessionKey = null;
let wechatBindPollTimer = null;
let wechatBindFlashTimer = null;

function wechatT(key, fallback) {
    return typeof t === 'function' ? t(key) : fallback;
}

function getWechatCard() {
    return document.getElementById('robot-wechat-subsection');
}

function setWechatBadge(mode) {
    const badge = document.getElementById('robot-wechat-status-badge');
    if (!badge) return;
    badge.classList.remove('robot-wechat-badge--idle', 'robot-wechat-badge--bound', 'robot-wechat-badge--scanning');
    if (mode === 'bound') {
        badge.classList.add('robot-wechat-badge--bound');
        badge.textContent = wechatT('settings.robots.wechat.statusBound', '已连接');
    } else if (mode === 'scanning') {
        badge.classList.add('robot-wechat-badge--scanning');
        badge.textContent = wechatT('settings.robots.wechat.statusScanning', '绑定中…');
    } else {
        badge.classList.add('robot-wechat-badge--idle');
        badge.textContent = wechatT('settings.robots.wechat.statusIdle', '未绑定');
    }
}

function setWechatCardBound(isBound) {
    const card = getWechatCard();
    if (card) card.classList.toggle('is-bound', !!isBound);
}

function updateWechatSteps(phase) {
    const steps = document.querySelectorAll('.robot-wechat-step');
    if (!steps.length) return;
    const order = ['generate', 'scan', 'confirm'];
    const idx = order.indexOf(phase);
    steps.forEach((el, i) => {
        el.classList.remove('is-active', 'is-done');
        if (idx < 0) {
            if (i === 0) el.classList.add('is-active');
        } else if (i < idx) {
            el.classList.add('is-done');
        } else if (i === idx) {
            el.classList.add('is-active');
        }
    });
}

function ensureWechatSteps() {
    const panel = document.getElementById('robot-wechat-scan-panel');
    if (!panel || panel.querySelector('.robot-wechat-steps')) return;
    const ol = document.createElement('ol');
    ol.className = 'robot-wechat-steps';
    ol.innerHTML = `
        <li class="robot-wechat-step is-active">${wechatT('settings.robots.wechat.step1', '生成二维码')}</li>
        <li class="robot-wechat-step">${wechatT('settings.robots.wechat.step2', '微信扫码')}</li>
        <li class="robot-wechat-step">${wechatT('settings.robots.wechat.step3', '确认绑定')}</li>`;
    panel.insertBefore(ol, panel.firstChild);
}

function ensureWechatQrFrame() {
    const img = document.getElementById('robot-wechat-qr-img');
    if (!img || img.parentElement?.classList.contains('robot-wechat-qr-frame')) return;
    const frame = document.createElement('div');
    frame.className = 'robot-wechat-qr-frame';
    img.parentNode.insertBefore(frame, img);
    frame.appendChild(img);
    let ph = document.getElementById('robot-wechat-qr-placeholder');
    if (!ph) {
        ph = document.createElement('div');
        ph.id = 'robot-wechat-qr-placeholder';
        ph.className = 'robot-wechat-qr-placeholder';
        ph.setAttribute('aria-hidden', 'true');
        ph.innerHTML = '<svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M14 14h3v3h-3v-3zm4 0h3v3h-3v-3zm-4 4h3v3h-3v-3zm4 0h3v3h-3v-3z"/></svg>';
        frame.appendChild(ph);
    } else {
        frame.appendChild(ph);
    }
}

function stopWechatBindPoll() {
    if (wechatBindPollTimer) {
        clearTimeout(wechatBindPollTimer);
        wechatBindPollTimer = null;
    }
}

function clearWechatBindSuccessNotice() {
    if (wechatBindFlashTimer) {
        clearTimeout(wechatBindFlashTimer);
        wechatBindFlashTimer = null;
    }
    const flash = document.getElementById('robot-wechat-bound-flash');
    if (flash) {
        flash.classList.remove('is-visible');
        flash.hidden = true;
    }
}

/** 绑定成功后的内联提示（约 4.5 秒后自动淡出） */
function showWechatBindSuccessNotice(message) {
    const text = message || wechatT('settings.robots.wechat.boundSuccess', '绑定成功，微信机器人已启用。');
    const flash = document.getElementById('robot-wechat-bound-flash');
    const flashText = document.getElementById('robot-wechat-bound-flash-text');

    if (flash) {
        if (flashText) flashText.textContent = text;
        flash.hidden = false;
        requestAnimationFrame(() => flash.classList.add('is-visible'));
        if (wechatBindFlashTimer) clearTimeout(wechatBindFlashTimer);
        wechatBindFlashTimer = setTimeout(() => {
            flash.classList.remove('is-visible');
            wechatBindFlashTimer = setTimeout(() => {
                flash.hidden = true;
                wechatBindFlashTimer = null;
            }, 300);
        }, 4500);
    }

    if (typeof window.showChatToast === 'function') {
        window.showChatToast(text, 'success');
    }
}

/** 已绑定：收起二维码区，仅展示紧凑摘要 */
function showWechatBoundUI(wechat) {
    const wc = wechat || {};
    const wrap = document.getElementById('robot-wechat-qr-wrap');
    const boundPanel = document.getElementById('robot-wechat-bound-panel');
    const scanPanel = document.getElementById('robot-wechat-scan-panel');
    const summary = document.getElementById('robot-wechat-bound-summary');
    const btn = document.getElementById('robot-wechat-bind-btn');

    stopWechatBindPoll();
    wechatBindSessionKey = null;
    setWechatBadge('bound');
    setWechatCardBound(true);

    if (wrap) wrap.hidden = true;
    if (boundPanel) boundPanel.hidden = true;
    if (scanPanel) scanPanel.hidden = true;

    const verifyWrap = document.getElementById('robot-wechat-verify-wrap');
    if (verifyWrap) verifyWrap.hidden = true;

    const img = document.getElementById('robot-wechat-qr-img');
    const ph = document.getElementById('robot-wechat-qr-placeholder');
    if (img) {
        img.removeAttribute('src');
        img.hidden = true;
    }
    if (ph) ph.hidden = false;

    const id = wc.ilink_bot_id || document.getElementById('robot-wechat-ilink-bot-id')?.value?.trim() || '';
    if (summary) {
        if (id) {
            const prefix = wechatT('settings.robots.wechat.boundBotId', '已绑定 Bot ID：');
            summary.innerHTML = `${prefix}<code>${escapeHtml(id)}</code>`;
            summary.hidden = false;
        } else {
            summary.textContent = '';
            summary.hidden = true;
        }
    }

    if (btn) {
        btn.textContent = wechatT('settings.robots.wechat.rebindButton', '重新绑定');
    }
    if (typeof refreshRobotManager === 'function') {
        refreshRobotManager();
    }
}

function escapeHtml(text) {
    return String(text)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

/** 扫码绑定进行中 */
function showWechatScanUI() {
    const wrap = document.getElementById('robot-wechat-qr-wrap');
    const boundPanel = document.getElementById('robot-wechat-bound-panel');
    const scanPanel = document.getElementById('robot-wechat-scan-panel');
    const summary = document.getElementById('robot-wechat-bound-summary');
    const btn = document.getElementById('robot-wechat-bind-btn');

    setWechatBadge('scanning');
    setWechatCardBound(false);
    clearWechatBindSuccessNotice();
    ensureWechatSteps();
    updateWechatSteps('generate');

    if (wrap) wrap.hidden = false;
    if (boundPanel) boundPanel.hidden = true;
    if (scanPanel) scanPanel.hidden = false;
    if (summary) summary.hidden = true;

    const verifyWrap = document.getElementById('robot-wechat-verify-wrap');
    if (verifyWrap) verifyWrap.hidden = true;

    const verifyInput = document.getElementById('robot-wechat-verify-code');
    if (verifyInput) verifyInput.value = '';

    if (btn) {
        btn.textContent = wechatT('settings.robots.wechat.bindButton', '生成二维码并绑定');
    }
}

/** 未绑定且未在扫码：隐藏面板 */
function hideWechatQrWrap() {
    const wrap = document.getElementById('robot-wechat-qr-wrap');
    const summary = document.getElementById('robot-wechat-bound-summary');
    if (wrap) wrap.hidden = true;
    if (summary) summary.hidden = true;
    clearWechatBindSuccessNotice();
    setWechatBadge('idle');
    setWechatCardBound(false);
}

function setWechatQrImage(data) {
    ensureWechatQrFrame();
    const img = document.getElementById('robot-wechat-qr-img');
    const ph = document.getElementById('robot-wechat-qr-placeholder');
    const linkEl = document.getElementById('robot-wechat-qr-link');
    const openUrl = data.qrcode_open_url || data.qrcode_img_url || '';

    if (img) {
        if (data.qrcode_image_data_url) {
            img.onload = () => {
                img.hidden = false;
                if (ph) ph.hidden = true;
            };
            img.onerror = () => {
                img.hidden = true;
                if (ph) ph.hidden = false;
            };
            img.src = data.qrcode_image_data_url;
            updateWechatSteps('scan');
        } else {
            img.removeAttribute('src');
            img.hidden = true;
            if (ph) ph.hidden = false;
        }
    }
    if (linkEl) {
        if (openUrl) {
            linkEl.href = openUrl;
            linkEl.hidden = false;
        } else {
            linkEl.hidden = true;
        }
    }
}

function setWechatQrStatus(text, isError) {
    const el = document.getElementById('robot-wechat-qr-status');
    if (!el) return;
    el.textContent = text || '';
    el.classList.toggle('is-error', !!isError);
    el.classList.toggle('is-success', !isError && !!text);
}

async function startWechatRobotBind() {
    stopWechatBindPoll();
    wechatBindSessionKey = null;
    showWechatScanUI();
    ensureWechatQrFrame();

    const loading = document.getElementById('robot-wechat-qr-loading');
    const img = document.getElementById('robot-wechat-qr-img');
    const ph = document.getElementById('robot-wechat-qr-placeholder');
    const btn = document.getElementById('robot-wechat-bind-btn');

    if (loading) loading.hidden = false;
    if (img) {
        img.removeAttribute('src');
        img.hidden = true;
    }
    if (ph) ph.hidden = false;
    setWechatQrStatus('', false);
    if (btn) btn.disabled = true;

    const botType = document.getElementById('robot-wechat-bot-type')?.value.trim() || '3';

    try {
        const res = await apiFetch('/api/robot/wechat/qrcode', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ bot_type: botType })
        });
        const data = await res.json();
        if (!res.ok) {
            throw new Error(data.error || data.message || wechatT('settings.robots.wechat.qrFetchFail', '获取二维码失败'));
        }
        wechatBindSessionKey = data.session_key;
        setWechatQrImage(data);
        setWechatQrStatus(data.message || wechatT('settings.robots.wechat.qrReadyHint', '请使用微信扫描二维码'), false);
        pollWechatBindStatus();
    } catch (e) {
        setWechatQrStatus(e.message || String(e), true);
        setWechatBadge('idle');
    } finally {
        if (loading) loading.hidden = true;
        if (btn) btn.disabled = false;
    }
}

async function pollWechatBindStatus() {
    if (!wechatBindSessionKey) return;

    try {
        const url = `/api/robot/wechat/qrcode/status?session_key=${encodeURIComponent(wechatBindSessionKey)}`;
        const res = await apiFetch(url, { method: 'GET' });
        const data = await res.json();
        if (!res.ok) {
            throw new Error(data.error || wechatT('settings.robots.wechat.pollFail', '轮询失败'));
        }

        const verifyWrap = document.getElementById('robot-wechat-verify-wrap');

        switch (data.status) {
            case 'confirmed':
                stopWechatBindPoll();
                updateWechatSteps('confirm');
                document.getElementById('robot-wechat-enabled').checked = true;
                if (data.ilink_bot_id) {
                    const idEl = document.getElementById('robot-wechat-ilink-bot-id');
                    if (idEl) idEl.value = data.ilink_bot_id;
                }
                showWechatBindSuccessNotice(
                    data.message || wechatT('settings.robots.wechat.boundSuccess', '绑定成功，微信机器人已启用。')
                );
                if (typeof loadConfig === 'function') {
                    await loadConfig(false);
                } else {
                    showWechatBoundUI({
                        ilink_bot_id: data.ilink_bot_id,
                        bound: true
                    });
                }
                if (typeof refreshRobotManager === 'function') {
                    refreshRobotManager();
                }
                return;
            case 'need_verifycode':
                updateWechatSteps('scan');
                if (verifyWrap) verifyWrap.hidden = false;
                setWechatQrStatus(data.message || wechatT('settings.robots.wechat.verifyCodeHint', '请输入手机微信显示的数字'), false);
                break;
            case 'scaned':
                updateWechatSteps('confirm');
                if (verifyWrap) verifyWrap.hidden = true;
                setWechatQrStatus(wechatT('settings.robots.wechat.qrScanWait', '已扫码，请在手机上确认…'), false);
                break;
            case 'binded_redirect':
                stopWechatBindPoll();
                showWechatBindSuccessNotice(
                    data.message || wechatT('settings.robots.wechat.alreadyBound', '该微信已绑定过，无需重复绑定。')
                );
                showWechatBoundUI({ bound: true });
                return;
            case 'expired':
                setWechatQrStatus(wechatT('settings.robots.wechat.qrExpired', '二维码已过期，请重新点击「生成二维码并绑定」'), true);
                setWechatBadge('scanning');
                stopWechatBindPoll();
                return;
            default:
                if (verifyWrap) verifyWrap.hidden = true;
                break;
        }
    } catch (e) {
        setWechatQrStatus(e.message || String(e), true);
    }

    wechatBindPollTimer = setTimeout(pollWechatBindStatus, 1500);
}

async function submitWechatVerifyCode() {
    const code = document.getElementById('robot-wechat-verify-code')?.value.trim();
    if (!code || !wechatBindSessionKey) return;
    try {
        const res = await apiFetch('/api/robot/wechat/qrcode/verify', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_key: wechatBindSessionKey, verify_code: code })
        });
        const data = await res.json();
        if (!res.ok) throw new Error(data.error || wechatT('settings.robots.wechat.verifyCodeFail', '提交失败'));
        setWechatQrStatus(data.message || wechatT('settings.robots.wechat.verifyCodeWait', '已提交配对码，等待确认…'), false);
        pollWechatBindStatus();
    } catch (e) {
        setWechatQrStatus(e.message || String(e), true);
    }
}

function refreshWechatRobotBoundUI(wechat) {
    const wc = wechat || {};
    const isBound = wc.bound || (wc.bot_token && wc.ilink_bot_id) || !!(wc.ilink_bot_id && wc.enabled);
    if (isBound) {
        showWechatBoundUI(wc);
    } else {
        hideWechatQrWrap();
        const btn = document.getElementById('robot-wechat-bind-btn');
        if (btn) {
            btn.textContent = wechatT('settings.robots.wechat.bindButton', '生成二维码并绑定');
        }
        if (typeof refreshRobotManager === 'function') {
            refreshRobotManager();
        }
    }
}
