// 运行空间占用统计与垃圾清理（系统设置 -> 存储清理）
//
// 数据来源：
//   GET  /api/storage/meta    清理策略与类别元信息
//   GET  /api/storage/status  文件系统容量 + 各类别占用/可回收量
//   POST /api/storage/cleanup 预览（dry_run）或执行清理
//   PUT  /api/config          保存策略（storage 段）
(function () {
    'use strict';

    var meta = null;
    var status = null;
    var busy = false;

    function esc(v) {
        var s = v == null ? '' : String(v);
        if (typeof escapeHtml === 'function') return escapeHtml(s);
        return s.replace(/[&<>"']/g, function (c) {
            return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
        });
    }

    function st(key, fallback) {
        if (typeof t === 'function') {
            var v = t(key);
            if (v && v !== key) return v;
        }
        return fallback;
    }

    // 类别名称/说明来自后端（中文硬编码），优先走 i18n，缺失时回退服务端文案。
    function catLabel(cat) {
        if (!cat) return '';
        return st('settingsStorage.cat.' + cat.key, cat.label || cat.key);
    }

    function catHint(cat) {
        if (!cat) return '';
        return st('settingsStorage.catHint.' + cat.key, cat.hint || '');
    }

    function fmtBytes(n) {
        var v = Number(n);
        if (!Number.isFinite(v) || v <= 0) return '0 B';
        var units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
        var i = 0;
        while (v >= 1024 && i < units.length - 1) {
            v /= 1024;
            i++;
        }
        // 字节不给小数，其余保留一位；数值较大时省去小数避免噪声
        var digits = i === 0 ? 0 : (v >= 100 ? 0 : 1);
        return v.toFixed(digits) + ' ' + units[i];
    }

    function setText(id, text) {
        var el = document.getElementById(id);
        if (el) el.textContent = text == null ? '' : String(text);
    }

    function intOr(raw, fallback) {
        var v = parseInt(raw, 10);
        return Number.isFinite(v) && v >= 0 ? v : fallback;
    }

    async function readErr(r, fallback) {
        try {
            var body = await r.json();
            return (body && body.error) || fallback;
        } catch (_) {
            return fallback;
        }
    }

    async function loadMeta() {
        var r = await apiFetch('/api/storage/meta');
        if (!r.ok) throw new Error(await readErr(r, st('settingsStorage.loadMetaFailed', '获取清理策略失败')));
        meta = await r.json();
    }

    async function loadStatus(refresh) {
        var r = await apiFetch('/api/storage/status' + (refresh ? '?refresh=1' : ''));
        if (!r.ok) throw new Error(await readErr(r, st('settingsStorage.loadStatusFailed', '获取存储占用失败')));
        status = await r.json();
    }

    async function postCleanup(body) {
        var r = await apiFetch('/api/storage/cleanup', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (!r.ok) throw new Error(await readErr(r, st('settingsStorage.cleanupFailed', '清理请求失败')));
        return await r.json();
    }

    function render() {
        renderOverview();
        renderRows();
        renderPolicy();
        showScannedAt();
    }

    function renderOverview() {
        var ov = document.getElementById('storage-overview');
        if (!ov) return;
        ov.hidden = false;
        var fs = (status && status.filesystem) || {};
        var totals = (status && status.totals) || {};

        setText('storage-disk-used', fs.available ? fmtBytes(fs.used_bytes) : st('settingsStorage.unavailable', '不可用'));
        var fill = document.getElementById('storage-usage-bar-fill');
        if (fill) {
            var pct = fs.available ? Math.max(0, Math.min(100, Number(fs.used_percent) || 0)) : 0;
            fill.style.width = pct.toFixed(1) + '%';
            fill.classList.toggle('storage-usage-bar-fill--warn', pct >= 85);
        }
        setText('storage-disk-detail', fs.available
            ? fmtBytes(fs.free_bytes) + ' ' + st('settingsStorage.free', '可用') + ' / ' + fmtBytes(fs.total_bytes)
            : '');

        setText('storage-total-bytes', fmtBytes(totals.bytes));
        setText('storage-total-units', (totals.units || 0) + ' ' + st('settingsStorage.itemsUnit', '项'));
        setText('storage-reclaimable-bytes', fmtBytes(totals.reclaimable_bytes));
        setText('storage-reclaimable-units', (totals.reclaimable_units || 0) + ' ' + st('settingsStorage.itemsUnit', '项'));

        if (fs.available && fs.inodes_total > 0) {
            var usedInodes = Math.max(0, fs.inodes_total - fs.inodes_free);
            var inodePct = (usedInodes / fs.inodes_total) * 100;
            setText('storage-inodes', usedInodes.toLocaleString() + ' / ' + fs.inodes_total.toLocaleString()
                + ' (' + inodePct.toFixed(1) + '%)');
        } else {
            setText('storage-inodes', st('settingsStorage.unavailable', '不可用'));
        }
    }

    function renderRows() {
        var tbody = document.getElementById('storage-category-rows');
        if (!tbody) return;
        var categories = (meta && meta.categories) || [];
        if (!categories.length) {
            tbody.innerHTML = '<tr><td colspan="7" class="storage-empty">'
                + esc(st('settingsStorage.noCategories', '暂无可清理类别')) + '</td></tr>';
            return;
        }
        var byKey = {};
        ((status && status.categories) || []).forEach(function (c) { byKey[c.key] = c; });

        tbody.innerHTML = categories.map(function (m) {
            var s = byKey[m.key] || {};
            var missing = !!s.missing;
            var reclaimParts = [];
            if (!missing) {
                reclaimParts.push((s.reclaimable_units || 0) + ' ' + st('settingsStorage.itemsUnit', '项'));
                if (s.orphan_units) {
                    reclaimParts.push(st('settingsStorage.orphanSuffix', '含孤儿') + ' ' + s.orphan_units);
                }
                if (s.skipped_active) {
                    reclaimParts.push(st('settingsStorage.skippedActive', '跳过活跃') + ' ' + s.skipped_active);
                }
            }
            var retentionTitle = st('settingsStorage.defaultRetention', '默认') + ' '
                + (m.default_retention != null ? m.default_retention : '-') + ' '
                + st('settingsStorage.daysUnit', '天');
            return '<tr class="' + (m.enabled ? '' : 'storage-row--disabled') + '">'
                + '<td><span class="storage-cat-label" title="' + esc(catHint(m)) + '">' + esc(catLabel(m)) + '</span></td>'
                + '<td>' + (missing
                    ? '<span class="storage-muted">' + esc(st('settingsStorage.unused', '未使用')) + '</span>'
                    : '<code class="storage-root" title="' + esc(s.root || '') + '">' + esc(shortRoot(s.root)) + '</code>') + '</td>'
                + '<td>' + (missing ? '—' : (s.units || 0)) + '</td>'
                + '<td>' + (missing ? '—' : fmtBytes(s.bytes)) + '</td>'
                + '<td>' + (missing ? '—' : '<strong>' + fmtBytes(s.reclaimable_bytes) + '</strong>'
                    + '<span class="storage-sub">' + esc(reclaimParts.join(' · ')) + '</span>') + '</td>'
                + '<td><input type="number" class="storage-retention-input" min="0" step="1"'
                + ' data-key="' + esc(m.key) + '" value="' + esc(m.retention_days != null ? m.retention_days : 0) + '"'
                + ' title="' + esc(retentionTitle) + '" aria-label="' + esc(retentionTitle) + '" /></td>'
                + '<td><input type="checkbox" class="storage-enabled-input" data-key="' + esc(m.key) + '"'
                + (m.enabled ? ' checked' : '') + ' aria-label="' + esc(catLabel(m)) + '" /></td>'
                + '</tr>';
        }).join('');
    }

    // 目录列只展示末两级，完整路径放在 title 里，避免长绝对路径把表格撑破。
    function shortRoot(root) {
        var s = String(root || '');
        if (!s) return '';
        var parts = s.split('/').filter(Boolean);
        if (parts.length <= 2) return s;
        return '…/' + parts.slice(-2).join('/');
    }

    function renderPolicy() {
        if (!meta) return;
        var auto = document.getElementById('storage-auto-clean');
        if (auto) auto.checked = !!meta.auto_clean;
        setNumber('storage-interval-minutes', meta.interval_minutes);
        setNumber('storage-orphan-grace-days', meta.orphan_grace_days);
        setNumber('storage-active-grace-hours', meta.active_grace_hours);
    }

    function setNumber(id, value) {
        var el = document.getElementById(id);
        if (el) el.value = value != null ? value : '';
    }

    function showScannedAt() {
        var el = document.getElementById('storage-scanned-at');
        if (!el) return;
        if (!status || !status.scanned_at) {
            el.textContent = '';
            return;
        }
        var d = new Date(status.scanned_at);
        el.textContent = st('settingsStorage.scannedAt', '扫描于') + ' '
            + (isNaN(d.getTime()) ? String(status.scanned_at) : d.toLocaleString());
    }

    function renderResult(rep, isPreview) {
        var el = document.getElementById('storage-result');
        if (!el) return;
        var totals = (rep && rep.totals) || {};
        var head = isPreview
            ? st('settingsStorage.previewTitle', '预览结果（未删除任何文件）')
            : st('settingsStorage.cleanDone', '清理完成');
        var summary = isPreview
            ? fmtBytes(totals.reclaimable_bytes) + ' / ' + (totals.reclaimable_units || 0) + ' ' + st('settingsStorage.itemsUnit', '项')
            : fmtBytes(totals.freed_bytes) + ' / ' + (totals.removed_units || 0) + ' ' + st('settingsStorage.itemsUnit', '项');

        var rows = ((rep && rep.categories) || [])
            .filter(function (c) {
                return isPreview ? (c.reclaimable_units || 0) > 0 : ((c.removed_units || 0) > 0 || (c.errors || []).length > 0);
            })
            .map(function (c) {
                var main = isPreview
                    ? fmtBytes(c.reclaimable_bytes) + ' / ' + (c.reclaimable_units || 0) + ' ' + st('settingsStorage.itemsUnit', '项')
                    : fmtBytes(c.freed_bytes) + ' / ' + (c.removed_units || 0) + ' ' + st('settingsStorage.itemsUnit', '项');
                var errs = (c.errors || []).slice(0, 3).map(esc).join('<br>');
                return '<li><span>' + esc(catLabel(c)) + '</span><span>' + esc(main)
                    + (errs ? '<br><span class="storage-error-text">' + errs + '</span>' : '')
                    + '</span></li>';
            });

        var html = '<div class="storage-result-head"><strong>' + esc(head) + '</strong><span>' + esc(summary) + '</span></div>';
        html += rows.length
            ? '<ul class="storage-result-list">' + rows.join('') + '</ul>'
            : '<p class="storage-muted">' + esc(st('settingsStorage.nothingToClean', '当前没有符合清理条件的内容。')) + '</p>';
        if (totals.skipped_active) {
            html += '<p class="storage-muted">' + esc(st('settingsStorage.skippedActiveNote', '已跳过最近仍在活动的会话：')
                + totals.skipped_active + ' ' + st('settingsStorage.itemsUnit', '项')) + '</p>';
        }
        el.className = 'storage-result';
        el.innerHTML = html;
        el.hidden = false;
    }

    function showError(message) {
        var el = document.getElementById('storage-result');
        if (!el) return;
        el.className = 'storage-result storage-result--error';
        el.innerHTML = '<div class="storage-result-head"><strong>'
            + esc(st('settingsStorage.failed', '操作失败')) + '</strong><span>' + esc(message) + '</span></div>';
        el.hidden = false;
    }

    function setButtonsDisabled(disabled) {
        var section = document.getElementById('settings-section-storage');
        if (!section) return;
        section.querySelectorAll('button').forEach(function (btn) { btn.disabled = disabled; });
    }

    async function withBusy(fn) {
        if (busy) return;
        busy = true;
        setButtonsDisabled(true);
        try {
            await fn();
        } catch (e) {
            showError(e && e.message ? e.message : String(e));
        } finally {
            busy = false;
            setButtonsDisabled(false);
        }
    }

    function collectPolicy() {
        var payload = {
            auto_clean: !!(document.getElementById('storage-auto-clean') || {}).checked,
            interval_minutes: intOr(document.getElementById('storage-interval-minutes') && document.getElementById('storage-interval-minutes').value, 5),
            orphan_grace_days: intOr(document.getElementById('storage-orphan-grace-days') && document.getElementById('storage-orphan-grace-days').value, 0),
            active_grace_hours: intOr(document.getElementById('storage-active-grace-hours') && document.getElementById('storage-active-grace-hours').value, 1),
            categories: {}
        };
        // 先用服务端元信息铺底，保证未渲染/缺字段时不会把既有策略清成默认值。
        ((meta && meta.categories) || []).forEach(function (m) {
            payload.categories[m.key] = { enabled: !!m.enabled, retention_days: intOr(m.retention_days, 0) };
        });
        document.querySelectorAll('#storage-category-rows .storage-retention-input').forEach(function (el) {
            var key = el.getAttribute('data-key');
            if (!key || !payload.categories[key]) return;
            payload.categories[key].retention_days = intOr(el.value, 0);
        });
        document.querySelectorAll('#storage-category-rows .storage-enabled-input').forEach(function (el) {
            var key = el.getAttribute('data-key');
            if (!key || !payload.categories[key]) return;
            payload.categories[key].enabled = !!el.checked;
        });
        return payload;
    }

    window.initStorageSection = async function () {
        if (typeof apiFetch !== 'function') return;
        if (!document.getElementById('storage-category-rows')) return;
        await withBusy(async function () {
            await loadMeta();
            await loadStatus(false);
            render();
        });
    };

    window.refreshStorageStatus = async function (force) {
        if (typeof apiFetch !== 'function') return;
        await withBusy(async function () {
            await loadStatus(force === true);
            render();
        });
    };

    window.previewStorageCleanup = async function () {
        await withBusy(async function () {
            var rep = await postCleanup({ dry_run: true });
            renderResult(rep, true);
        });
    };

    window.runStorageCleanup = async function () {
        var totals = (status && status.totals) || {};
        var message = st('settingsStorage.confirmClean', '将永久删除约 {size}（{count} 项）运行空间文件，无法恢复。建议先执行「预览可清理项」。确认继续？')
            .replace('{size}', fmtBytes(totals.reclaimable_bytes))
            .replace('{count}', String(totals.reclaimable_units || 0));
        if (!window.confirm(message)) return;
        await withBusy(async function () {
            // 服务端要求真实删除必须同时带 dry_run=false 与 confirm=true。
            var rep = await postCleanup({ dry_run: false, confirm: true });
            renderResult(rep, false);
            await loadStatus(true);
            render();
        });
    };

    window.saveStorageSettings = async function () {
        var payload = collectPolicy();
        await withBusy(async function () {
            // 只调 PUT /api/config：UpdateConfig 会合并 storage 段并写回 config.yaml，
            // 清理器每次执行都实时读取配置，无需 /api/config/apply 触发重启类副作用。
            var r = await apiFetch('/api/config', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ storage: payload })
            });
            if (!r.ok) throw new Error(await readErr(r, st('settingsStorage.saveFailed', '保存失败')));
            await loadMeta();
            renderPolicy();
            renderRows();
            window.alert(st('settingsStorage.saved', '清理策略已保存'));
        });
    };
})();
