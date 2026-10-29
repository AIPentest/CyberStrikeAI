/**
 * 资产页「按类型分 tab + 扁平表格」视图（单文件、自包含、无第三方依赖）
 *
 * 设计：
 *   - 资产页顶部 tab 由 index.html 静态渲染，本文件负责切换与数据加载；
 *   - 数据 tab（子域名 / 站点 / IP / 服务）走 GET /api/assets/table；
 *   - 「全部资产」复用 assets.js 既有列表（本文件只控制显隐，不改其逻辑）；
 *   - 「层级」复用 assets-tree.js 的七层树；
 *   - 「SSL证书 / 文件泄露 / URL信息」为占位 tab，不发起请求；
 *   - 每个数据 tab 有独立筛选状态（关键字 / 来源 / 解析类型）与分页状态，切回时可恢复；
 *   - 「站点」tab 额外渲染截图列：缩略图走 apiFetch + blob URL（带 Authorization），
 *     支持大图预览、重新截图（POST 后 2s/次、最多 15 次轮询；pending/running 也会自动观察），
 *     切换 tab / 翻页时释放 blob。
 *
 * 对外只暴露：assetTableInit()、assetTableLoad(type, page)、assetTableSwitch(type)
 * 请求统一走全局 apiFetch()（自动附带 Authorization，401 弹登录框）。
 */
(function () {
    'use strict';

    const ASSET_TABLE_API = '/api/assets/table';
    const ASSET_TABLE_PAGE_SIZE = 50;
    const ASSET_TABLE_DATA_TYPES = ['subdomain', 'site', 'ip', 'service'];
    const ASSET_TABLE_PLACEHOLDER_TYPES = ['ssl', 'fileleak', 'urlinfo'];
    const ASSET_TABLE_TABS = [
        'subdomain', 'site', 'ip', 'service',
        'ssl', 'fileleak', 'urlinfo',
        'all', 'company', 'tree'
    ];
    const ASSET_TABLE_DEFAULT_TAB = 'subdomain';

    /** 公司 / 主域名接口（公司 tab 不请求 /api/assets/table） */
    const ASSET_COMPANY_API = '/api/companies';
    const ASSET_COMPANY_DOMAINS_API = '/api/company-domains';

    /** 截图状态轮询参数（契约建议）：2s/次、最多 15 次；手动触发与 pending/running 自动观察共用 */
    const ASSET_TABLE_SHOT_POLL_INTERVAL = 2000;
    const ASSET_TABLE_SHOT_POLL_MAX = 15;
    /** 缩略图并发加载上限，避免一次 50 张图把浏览器连接池打满 */
    const ASSET_TABLE_SHOT_CONCURRENCY = 4;

    function createTypeState() {
        return {
            loaded: false,
            loading: false,
            error: '',
            rows: [],
            total: 0,
            page: 1,
            pageSize: ASSET_TABLE_PAGE_SIZE,
            q: '',
            source: '',
            recordType: '',
            onlyNoShot: false,   // 站点 tab：仅看无截图（前端过滤当前页）
            pendingShots: [],    // 本次渲染待异步加载的缩略图描述
            requestSeq: 0
        };
    }

    const assetTableState = {
        initialized: false,
        current: ASSET_TABLE_DEFAULT_TAB,
        types: {
            subdomain: createTypeState(),
            site: createTypeState(),
            ip: createTypeState(),
            service: createTypeState()
        }
    };

    /**
     * 截图相关的运行时状态：
     *   blobUrls    —— 当前页缩略图 uid -> objectURL，重建表体 / 切换 tab 时统一 revoke；
     *   renderSeq   —— 表体渲染序号，异步缩略图返回时若已变化则丢弃并释放 URL；
     *   pollers     —— assetId -> 轮询定时器，切换 tab / 翻页 / 重新渲染时统一停止；
     *   lightbox    —— 大图预览状态（上次焦点、lightbox 自己创建的 blob URL）。
     */
    const assetTableShotState = {
        blobUrls: new Map(),
        renderSeq: 0,
        pollers: new Map(),
        lightbox: {
            open: false,
            lastFocus: null,
            ownBlobUrl: ''
        }
    };

    /** 公司 tab 状态：列表 / 待归属主域名 / 内联编辑 / 自动查找建议 / 操作提示 */
    const assetCompanyState = {
        loaded: false,
        loading: false,
        error: '',
        companies: [],
        pending: [],
        pendingLoaded: false,
        editingId: '',
        lookup: {
            companyId: '',
            loading: false,
            error: '',
            warnings: [],
            suggestions: []
        },
        notice: { type: '', text: '' },
        requestSeq: 0
    };

    /* ------------------------------------------------------------------ *
     * 基础工具
     * ------------------------------------------------------------------ */

    /** i18n 取值：i18next 未就绪 / 缺 key 时回退中文兜底（支持 {{name}} 插值） */
    function assetTableT(key, fallback, options) {
        try {
            if (typeof window.t === 'function') {
                const translated = window.t(key, options);
                if (translated && translated !== key) return translated;
            }
        } catch (error) {
            /* 忽略 i18n 异常，走兜底文案 */
        }
        if (fallback == null) return key;
        if (!options || typeof fallback !== 'string') return fallback;
        return fallback.replace(/\{\{\s*([\w.]+)\s*\}\}/g, function (match, name) {
            return options[name] == null ? match : String(options[name]);
        });
    }

    function assetTableEscape(value) {
        return String(value == null ? '' : value)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    function assetTableEscapeAttr(value) {
        return assetTableEscape(value).replace(/`/g, '&#96;');
    }

    function assetTableText(value) {
        return value == null ? '' : String(value).trim();
    }

    function assetTableNumber(value) {
        const parsed = Number(value);
        if (!Number.isFinite(parsed) || parsed < 0) return 0;
        return Math.floor(parsed);
    }

    function assetTableEl(id) {
        return document.getElementById(id);
    }

    function assetTableEmptyCell() {
        return '<span class="asset-type-cell-empty">-</span>';
    }

    /** 普通文本单元格（空值显示占位短横线） */
    function assetTableCellText(value) {
        const text = assetTableText(value);
        return text ? assetTableEscape(text) : assetTableEmptyCell();
    }

    /** 长文本单元格：超长省略，title 保留完整内容 */
    function assetTableCellTruncated(value) {
        const text = assetTableText(value);
        if (!text) return assetTableEmptyCell();
        return '<span class="asset-type-truncate" title="' + assetTableEscapeAttr(text) + '">'
            + assetTableEscape(text) + '</span>';
    }

    /** 数字单元格：0 也照常显示 */
    function assetTableCellNumber(value) {
        return String(assetTableNumber(value));
    }

    /** 来源单元格：manual 显示「手动导入」，其余原样显示（如 tool:httpx） */
    function assetTableSourceCell(value) {
        const source = assetTableText(value);
        if (!source) return assetTableEmptyCell();
        const label = source.toLowerCase() === 'manual'
            ? assetTableT('assets.type.sourceManual', '手动导入')
            : source;
        return '<span class="asset-type-source-badge">' + assetTableEscape(label) + '</span>';
    }

    /** 关联 IP：支持多值，每格渲染为多个小标签 */
    function assetTableIpsCell(row) {
        const list = [];
        if (Array.isArray(row.ips)) {
            row.ips.forEach(function (ip) {
                const text = assetTableText(ip);
                if (text) list.push(text);
            });
        }
        if (!list.length) {
            const fallback = assetTableText(row.ip);
            if (fallback) list.push(fallback);
        }
        if (!list.length) return assetTableEmptyCell();
        return '<span class="asset-type-ip-list">' + list.map(function (ip) {
            return '<span class="asset-type-ip">' + assetTableEscape(ip) + '</span>';
        }).join('') + '</span>';
    }

    /** 域名列：主显示 host（缺失回退 domain），父域名作为次要说明 */
    function assetTableDomainCell(row) {
        const host = assetTableText(row.host);
        const domain = assetTableText(row.domain);
        const primary = host || domain;
        if (!primary) return assetTableEmptyCell();
        let html = '<span class="asset-type-cell-primary">' + assetTableEscape(primary) + '</span>';
        if (domain && domain !== primary) {
            html += '<span class="asset-type-cell-sub">' + assetTableEscape(domain) + '</span>';
        }
        return html;
    }

    /** 服务 tab 的主机列：host 缺失时用 ip，二者不同时把 ip 作为次要说明 */
    function assetTableServiceHostCell(row) {
        const host = assetTableText(row.host);
        const ip = assetTableText(row.ip);
        const primary = host || ip;
        if (!primary) return assetTableEmptyCell();
        let html = '<span class="asset-type-cell-primary">' + assetTableEscape(primary) + '</span>';
        if (ip && ip !== primary) {
            html += '<span class="asset-type-cell-sub">' + assetTableEscape(ip) + '</span>';
        }
        return html;
    }

    /** 端口单元格 */
    function assetTablePortCell(value) {
        const port = assetTableNumber(value);
        if (port > 0) return String(port);
        const text = assetTableText(value);
        return text ? assetTableEscape(text) : assetTableEmptyCell();
    }

    /** 端口/协议单元格，如 443/https */
    function assetTablePortProtocolCell(row) {
        const portNumber = assetTableNumber(row.port);
        const port = portNumber > 0 ? String(portNumber) : assetTableText(row.port);
        const protocol = assetTableText(row.protocol);
        if (port && protocol) return assetTableEscape(port + '/' + protocol);
        if (port) return assetTableEscape(port);
        if (protocol) return assetTableEscape(protocol);
        return assetTableEmptyCell();
    }

    /* ------------------------------------------------------------------ *
     * 站点截图：缩略图（apiFetch + blob URL）、大图预览、重新截图轮询
     * ------------------------------------------------------------------ */

    const ASSET_TABLE_SHOT_STATUSES = ['done', 'pending', 'running', 'failed', 'none', 'disabled'];

    /**
     * 截图状态 / 触发截图接口（新版契约：静态路由 + query 参数，
     * 避免 /api/assets/:id/... 与 /api/assets/tree、/api/assets/table 冲突）。
     * 注意：缩略图地址仍然直接使用后端下发的 screenshot.url，不在这里拼。
     */
    const ASSET_TABLE_SHOT_STATUS_API = '/api/assets/screenshot/status';

    function assetTableShotAssetQuery(assetId) {
        return '?id=' + encodeURIComponent(assetId);
    }

    /** 把后端 screenshot 对象归一化，缺失 / 未知状态一律视为 none */
    function assetTableNormalizeShotMeta(raw) {
        const meta = raw && typeof raw === 'object' ? raw : null;
        const status = meta ? String(meta.status || '').toLowerCase() : '';
        return {
            status: ASSET_TABLE_SHOT_STATUSES.indexOf(status) === -1 ? 'none' : status,
            url: meta ? assetTableText(meta.url) : '',
            capturedAt: meta ? assetTableText(meta.captured_at) : '',
            error: meta ? assetTableText(meta.error) : '',
            width: meta ? assetTableNumber(meta.width) : 0,
            height: meta ? assetTableNumber(meta.height) : 0
        };
    }

    function assetTableShotMeta(row) {
        return assetTableNormalizeShotMeta(row && row.screenshot);
    }

    /**
     * 取资产 ID（重新截图 POST / 状态轮询需要）。
     * 优先用行内 asset_id / id；后端若只在 screenshot.url 里带 ID，则从 URL 解析兜底
     * （仅用于拼接接口路径，图片地址仍然直接使用后端下发的 screenshot.url）。
     */
    function assetTableResolveAssetId(row) {
        const direct = assetTableText(row && row.asset_id) || assetTableText(row && row.id);
        if (direct) return direct;
        const meta = row && row.screenshot && typeof row.screenshot === 'object' ? row.screenshot : null;
        const url = meta ? assetTableText(meta.url) : '';
        const match = url.match(/\/api\/assets\/([^/?#]+)\/screenshot/);
        return match ? decodeURIComponent(match[1]) : '';
    }

    function assetTableShotUid(type, page, rowIndex) {
        return String(type) + '-p' + assetTableNumber(page) + '-r' + assetTableNumber(rowIndex);
    }

    /** 重新截图后刷新缩略图需要带时间戳破缓存 */
    function assetTableShotWithCacheBuster(url) {
        const text = assetTableText(url);
        if (!text) return text;
        return text + (text.indexOf('?') === -1 ? '?' : '&') + 't=' + Date.now();
    }

    function assetTableShotStatusText(status) {
        if (status === 'pending') return assetTableT('assets.type.shotPending', '排队中');
        if (status === 'running') return assetTableT('assets.type.shotRunning', '截图中');
        if (status === 'failed') return assetTableT('assets.type.shotFailed', '截图失败');
        if (status === 'disabled') return assetTableT('assets.type.shotDisabled', '未启用');
        return assetTableT('assets.type.shotNone', '未截图');
    }

    function assetTableShotReleaseBlob(uid) {
        const objectUrl = assetTableShotState.blobUrls.get(uid);
        if (!objectUrl) return;
        assetTableShotState.blobUrls.delete(uid);
        try {
            URL.revokeObjectURL(objectUrl);
        } catch (error) {
            /* 忽略释放异常 */
        }
    }

    function assetTableShotReleaseAllBlobs() {
        assetTableShotState.blobUrls.forEach(function (objectUrl) {
            try {
                URL.revokeObjectURL(objectUrl);
            } catch (error) {
                /* 忽略释放异常 */
            }
        });
        assetTableShotState.blobUrls.clear();
    }

    /**
     * 生成站点截图单元格（“截图”列专用）。
     * 返回 { html, shot }：shot 非空表示需要异步拉取缩略图。
     */
    function assetTableBuildShotCell(type, row, rowIndex, page) {
        const meta = assetTableShotMeta(row);
        const uid = assetTableShotUid(type, page, rowIndex);
        const assetId = assetTableResolveAssetId(row);
        const host = assetTableText(row.host) || assetTableText(row.domain) || assetTableText(row.ip);
        let shot = null;
        let html = '<span class="asset-type-shot asset-type-shot--' + meta.status + '" id="asset-shot-wrap-' + uid + '">';

        if (meta.status === 'done' && meta.url) {
            const previewTitle = assetTableT('assets.type.previewTitle', '截图预览');
            html += '<button class="asset-type-shot-thumb" id="asset-shot-thumb-' + uid + '" type="button"'
                + ' data-asset-shot-action="preview"'
                + ' data-asset-shot-uid="' + assetTableEscapeAttr(uid) + '"'
                + ' data-asset-shot-url="' + assetTableEscapeAttr(meta.url) + '"'
                + ' data-asset-shot-label="' + assetTableEscapeAttr(host) + '"'
                + ' data-asset-shot-time="' + assetTableEscapeAttr(meta.capturedAt) + '"'
                + ' title="' + assetTableEscapeAttr(previewTitle) + '">'
                + '<img class="asset-type-shot-img" id="asset-shot-img-' + uid + '"'
                + ' alt="' + assetTableEscapeAttr(host) + '" loading="lazy" hidden>'
                + '</button>'
                + '<span class="asset-type-shot-fallback" id="asset-shot-fb-' + uid + '" hidden>'
                + assetTableEscape(assetTableT('assets.type.shotLoadFailed', '截图加载失败')) + '</span>';
            shot = {
                uid: uid,
                url: meta.url,
                imgId: 'asset-shot-img-' + uid,
                thumbId: 'asset-shot-thumb-' + uid,
                fallbackId: 'asset-shot-fb-' + uid
            };
        } else {
            const errorTitle = meta.status === 'failed' ? (meta.error || assetTableShotStatusText('failed')) : '';
            html += '<span class="asset-type-shot-placeholder asset-type-shot-placeholder--' + meta.status + '"'
                + (errorTitle ? ' title="' + assetTableEscapeAttr(errorTitle) + '"' : '') + '>'
                + assetTableEscape(assetTableShotStatusText(meta.status)) + '</span>';
        }

        // 排队中 / 截图中 / 功能未启用 不再提供重复触发入口
        if (assetId && meta.status !== 'pending' && meta.status !== 'running' && meta.status !== 'disabled') {
            const recapture = assetTableT('assets.type.shotRecapture', '重新截图');
            html += '<button class="asset-type-shot-action" id="asset-shot-btn-' + uid + '" type="button"'
                + ' data-asset-shot-action="capture"'
                + ' data-asset-shot-uid="' + assetTableEscapeAttr(uid) + '"'
                + ' data-asset-asset-id="' + assetTableEscapeAttr(assetId) + '"'
                + ' data-asset-row-index="' + rowIndex + '"'
                + ' title="' + assetTableEscapeAttr(recapture) + '">'
                + assetTableEscape(recapture) + '</button>';
        }
        html += '</span>';
        return { html: html, shot: shot };
    }

    /** 缩略图加载失败：隐藏图片与缩略图按钮，显示灰色占位，不影响其它列 */
    function assetTableShotShowFallback(shot) {
        assetTableShotReleaseBlob(shot.uid);
        const thumb = assetTableEl(shot.thumbId);
        if (thumb) thumb.hidden = true;
        const image = assetTableEl(shot.imgId);
        if (image) image.hidden = true;
        const fallback = assetTableEl(shot.fallbackId);
        if (fallback) fallback.hidden = false;
    }

    /** 拉取单张缩略图并转成 blob URL（<img src> 无法带 Authorization） */
    async function assetTableShotLoadThumbnail(shot) {
        const seq = assetTableShotState.renderSeq;
        try {
            const response = typeof apiFetch === 'function' ? await apiFetch(shot.url) : await fetch(shot.url);
            if (seq !== assetTableShotState.renderSeq) return;
            if (!response || !response.ok) throw new Error('HTTP ' + (response ? response.status : 'error'));
            const blob = await response.blob();
            if (seq !== assetTableShotState.renderSeq) return;
            const objectUrl = URL.createObjectURL(blob);
            if (seq !== assetTableShotState.renderSeq) {
                URL.revokeObjectURL(objectUrl);
                return;
            }
            assetTableShotReleaseBlob(shot.uid);
            assetTableShotState.blobUrls.set(shot.uid, objectUrl);
            const image = assetTableEl(shot.imgId);
            if (!image) {
                assetTableShotReleaseBlob(shot.uid);
                return;
            }
            image.addEventListener('error', function () {
                assetTableShotShowFallback(shot);
            });
            image.src = objectUrl;
            image.hidden = false;
            image.classList.add('is-loaded');
        } catch (error) {
            if (seq !== assetTableShotState.renderSeq) return;
            console.warn('加载站点截图失败:', error);
            assetTableShotShowFallback(shot);
        }
    }

    /** 并发受限地批量加载当前页缩略图 */
    function assetTableShotHydrate(state) {
        const shots = Array.isArray(state.pendingShots) ? state.pendingShots.slice() : [];
        state.pendingShots = [];
        if (!shots.length) return;
        let nextIndex = 0;
        let activeCount = 0;
        const pump = function () {
            while (activeCount < ASSET_TABLE_SHOT_CONCURRENCY && nextIndex < shots.length) {
                const shot = shots[nextIndex++];
                activeCount += 1;
                assetTableShotLoadThumbnail(shot)
                    .catch(function () { /* 单张失败不影响其它截图 */ })
                    .then(function () {
                        activeCount -= 1;
                        pump();
                    });
            }
        };
        pump();
    }

    /* -------------------------- 大图预览 lightbox -------------------------- */

    function assetTableShotReleaseLightboxBlob() {
        if (!assetTableShotState.lightbox.ownBlobUrl) return;
        try {
            URL.revokeObjectURL(assetTableShotState.lightbox.ownBlobUrl);
        } catch (error) {
            /* 忽略释放异常 */
        }
        assetTableShotState.lightbox.ownBlobUrl = '';
    }

    async function assetTableShotLoadLightboxImage(url) {
        if (!url) return;
        const image = assetTableEl('asset-shot-lightbox-img');
        const loading = assetTableEl('asset-shot-lightbox-loading');
        try {
            const response = typeof apiFetch === 'function' ? await apiFetch(url) : await fetch(url);
            if (!assetTableShotState.lightbox.open) return;
            if (!response || !response.ok) throw new Error('HTTP ' + (response ? response.status : 'error'));
            const blob = await response.blob();
            if (!assetTableShotState.lightbox.open) return;
            assetTableShotReleaseLightboxBlob();
            const objectUrl = URL.createObjectURL(blob);
            assetTableShotState.lightbox.ownBlobUrl = objectUrl;
            if (image) {
                image.src = objectUrl;
                image.hidden = false;
            }
            if (loading) loading.hidden = true;
        } catch (error) {
            console.warn('加载截图大图失败:', error);
            if (loading) {
                loading.hidden = false;
                loading.textContent = assetTableT('assets.type.shotLoadFailed', '截图加载失败');
            }
        }
    }

    function assetTableOpenLightbox(trigger) {
        const lightbox = assetTableEl('asset-shot-lightbox');
        const image = assetTableEl('asset-shot-lightbox-img');
        if (!lightbox || !image) return;
        const uid = assetTableText(trigger.getAttribute('data-asset-shot-uid'));
        const url = assetTableText(trigger.getAttribute('data-asset-shot-url'));
        const label = assetTableText(trigger.getAttribute('data-asset-shot-label'));
        const capturedAt = assetTableText(trigger.getAttribute('data-asset-shot-time'));
        const metaEl = assetTableEl('asset-shot-lightbox-meta');
        const loading = assetTableEl('asset-shot-lightbox-loading');

        assetTableShotState.lightbox.open = true;
        assetTableShotState.lightbox.lastFocus = document.activeElement && typeof document.activeElement.focus === 'function'
            ? document.activeElement
            : null;
        image.alt = label;
        if (metaEl) {
            metaEl.textContent = capturedAt
                ? label + ' · ' + assetTableT('assets.type.shotCapturedAt', '截图于 {{time}}', { time: capturedAt })
                : label;
        }

        const cachedUrl = assetTableShotState.blobUrls.get(uid);
        if (cachedUrl) {
            image.src = cachedUrl;
            image.hidden = false;
            if (loading) loading.hidden = true;
        } else {
            image.hidden = true;
            if (loading) {
                loading.hidden = false;
                loading.textContent = assetTableT('common.loading', '加载中...');
            }
            void assetTableShotLoadLightboxImage(url);
        }

        lightbox.hidden = false;
        const closeButton = assetTableEl('asset-shot-lightbox-close');
        if (closeButton) closeButton.focus();
        if (document.body && document.body.classList) document.body.classList.add('asset-shot-lightbox-open');
    }

    function assetTableCloseLightbox() {
        const lightbox = assetTableEl('asset-shot-lightbox');
        if (!lightbox || lightbox.hidden) return;
        lightbox.hidden = true;
        assetTableShotState.lightbox.open = false;
        const image = assetTableEl('asset-shot-lightbox-img');
        if (image) {
            image.hidden = true;
            image.removeAttribute('src');
        }
        const loading = assetTableEl('asset-shot-lightbox-loading');
        if (loading) {
            loading.hidden = false;
            loading.textContent = assetTableT('common.loading', '加载中...');
        }
        assetTableShotReleaseLightboxBlob();
        if (document.body && document.body.classList) document.body.classList.remove('asset-shot-lightbox-open');
        const lastFocus = assetTableShotState.lightbox.lastFocus;
        assetTableShotState.lightbox.lastFocus = null;
        if (lastFocus && typeof lastFocus.focus === 'function') lastFocus.focus();
    }

    /* -------------------------- 重新截图 + 状态轮询 -------------------------- */

    function assetTableShotStopPoller(assetId, restoreTitle) {
        const poller = assetTableShotState.pollers.get(assetId);
        if (!poller) return;
        if (poller.timer) clearTimeout(poller.timer);
        assetTableShotState.pollers.delete(assetId);
        if (!restoreTitle) return;
        const button = assetTableEl('asset-shot-btn-' + poller.uid);
        if (!button) return;
        button.disabled = false;
        button.classList.remove('is-busy');
        button.textContent = assetTableT('assets.type.shotRecapture', '重新截图');
        button.title = restoreTitle;
    }

    function assetTableShotStopAllPollers() {
        Array.from(assetTableShotState.pollers.keys()).forEach(function (assetId) {
            assetTableShotStopPoller(assetId, '');
        });
    }

    /** 触发失败时按 HTTP 状态码给出可读原因（契约：400/404/429/503） */
    function assetTableShotTriggerErrorMessage(status) {
        if (status === 400) return assetTableT('assets.type.shotErrorNotHttp', '仅支持 http/https 站点');
        if (status === 404) return assetTableT('assets.type.shotErrorNotFound', '资产不存在或无权访问');
        if (status === 429) return assetTableT('assets.type.shotErrorBusy', '截图队列已满，请稍后重试');
        if (status === 503) return assetTableT('assets.type.shotErrorDisabled', '截图功能未启用');
        return assetTableT('assets.type.shotTriggerFailed', '触发截图失败');
    }

    function assetTableShotNotify(message) {
        if (!message) return;
        if (typeof window.showInlineToast === 'function') {
            try {
                window.showInlineToast(message);
            } catch (error) {
                /* toast 失败不影响主流程 */
            }
        }
    }

    function assetTableShotScheduleNext(assetId) {
        const poller = assetTableShotState.pollers.get(assetId);
        if (!poller) return;
        if (poller.timer) clearTimeout(poller.timer);
        poller.timer = setTimeout(function () {
            poller.timer = 0;
            void assetTableShotPollOnce(assetId);
        }, ASSET_TABLE_SHOT_POLL_INTERVAL);
    }

    function assetTableShotUpdateCell(state, row, rowIndex, uid) {
        const cell = assetTableEl('asset-shot-cell-' + uid);
        if (!cell) return;
        assetTableShotReleaseBlob(uid);
        const built = assetTableBuildShotCell('site', row, rowIndex, state.page);
        cell.innerHTML = built.html;
        if (built.shot) {
            // 重新截图成功：强制破缓存拉新图
            built.shot.url = assetTableShotWithCacheBuster(built.shot.url);
            void assetTableShotLoadThumbnail(built.shot);
        }
    }

    function assetTableShotFinishPoller(assetId, meta) {
        const poller = assetTableShotState.pollers.get(assetId);
        if (!poller) return;
        if (poller.timer) clearTimeout(poller.timer);
        assetTableShotState.pollers.delete(assetId);
        const state = assetTableState.types.site;
        const row = state.rows[poller.rowIndex];
        if (!row) return;
        // /status 不一定回 url / 时间，缺失时沿用表格数据里的旧值
        const previous = assetTableShotMeta(row);
        row.screenshot = {
            status: meta.status,
            url: meta.url || previous.url,
            captured_at: meta.capturedAt || previous.capturedAt,
            error: meta.error,
            width: meta.width || previous.width,
            height: meta.height || previous.height
        };
        assetTableShotUpdateCell(state, row, poller.rowIndex, poller.uid);
    }

    async function assetTableShotPollOnce(assetId) {
        const poller = assetTableShotState.pollers.get(assetId);
        if (!poller) return;
        poller.attempts += 1;
        const isFinalAttempt = poller.attempts >= ASSET_TABLE_SHOT_POLL_MAX;
        const timeoutText = assetTableT('assets.type.shotTimeout', '截图超时，请稍后刷新');
        try {
            const url = ASSET_TABLE_SHOT_STATUS_API + assetTableShotAssetQuery(assetId);
            const response = typeof apiFetch === 'function' ? await apiFetch(url) : await fetch(url);
            if (!assetTableShotState.pollers.has(assetId)) return; // 期间已被取消
            if (!response || !response.ok) throw new Error('HTTP ' + (response ? response.status : 'error'));
            const payload = await response.json();
            if (!assetTableShotState.pollers.has(assetId)) return;
            const meta = assetTableNormalizeShotMeta(
                payload && payload.screenshot ? payload.screenshot : payload
            );
            if (meta.status === 'done' || meta.status === 'failed' || meta.status === 'disabled') {
                assetTableShotFinishPoller(assetId, meta);
            } else if (isFinalAttempt) {
                console.warn('站点截图轮询超时:', assetId);
                assetTableShotStopPoller(assetId, timeoutText);
            } else {
                assetTableShotScheduleNext(assetId);
            }
        } catch (error) {
            if (!assetTableShotState.pollers.has(assetId)) return;
            console.warn('轮询站点截图状态失败:', error);
            assetTableShotStopPoller(assetId, timeoutText);
        }
    }

    /**
     * 对当前页 pending / running 的站点行自动轮询状态（契约建议 2s/次、最多 15 次）。
     * 与手动「重新截图」共用轮询器，切换 tab / 翻页 / 重新渲染时统一停止。
     */
    function assetTableShotStartStatusWatchers(state) {
        (state.rows || []).forEach(function (row, index) {
            const safeRow = row && typeof row === 'object' ? row : {};
            const meta = assetTableShotMeta(safeRow);
            if (meta.status !== 'pending' && meta.status !== 'running') return;
            const assetId = assetTableResolveAssetId(safeRow);
            if (!assetId || assetTableShotState.pollers.has(assetId)) return;
            assetTableShotState.pollers.set(assetId, {
                assetId: assetId,
                uid: assetTableShotUid('site', state.page, index),
                rowIndex: index,
                attempts: 0,
                timer: 0,
                auto: true
            });
            assetTableShotScheduleNext(assetId);
        });
    }

    async function assetTableShotPostScreenshot(assetId) {
        const timeoutText = assetTableT('assets.type.shotTimeout', '截图超时，请稍后刷新');
        try {
            const url = '/api/assets/screenshot' + assetTableShotAssetQuery(assetId);
            const response = typeof apiFetch === 'function'
                ? await apiFetch(url, { method: 'POST' })
                : await fetch(url, { method: 'POST' });
            if (!assetTableShotState.pollers.has(assetId)) return;
            if (!response || !response.ok) {
                const httpError = new Error('HTTP ' + (response ? response.status : 'error'));
                httpError.status = response ? response.status : 0;
                throw httpError;
            }
            assetTableShotScheduleNext(assetId);
        } catch (error) {
            console.warn('触发站点截图失败:', error);
            const message = assetTableShotTriggerErrorMessage(error && error.status);
            assetTableShotStopPoller(assetId, message || timeoutText);
            assetTableShotNotify(message);
        }
    }

    /** 点击「重新截图」：POST 后进入轮询，按钮先进入忙碌态 */
    function assetTableShotRequestCapture(button) {
        const buttonUid = assetTableText(button.getAttribute('data-asset-shot-uid'));
        const assetId = assetTableText(button.getAttribute('data-asset-asset-id'));
        const rowIndex = Number(button.getAttribute('data-asset-row-index'));
        const type = assetTableState.current;
        if (type !== 'site' || !assetId || !Number.isFinite(rowIndex)) return;
        if (assetTableShotState.pollers.has(assetId)) return;
        const row = assetTableState.types.site.rows[rowIndex];
        if (!row) return;

        const uid = buttonUid || assetTableShotUid('site', assetTableState.types.site.page, rowIndex);
        button.disabled = true;
        button.classList.add('is-busy');
        button.textContent = assetTableT('assets.type.shotCapturing', '截图中…');
        assetTableShotState.pollers.set(assetId, {
            assetId: assetId,
            uid: uid,
            rowIndex: rowIndex,
            attempts: 0,
            timer: 0
        });
        void assetTableShotPostScreenshot(assetId);
    }

    function assetTableColumn(key, labelKey, label, render, className) {
        return { key: key, labelKey: labelKey, label: label, render: render, className: className || '' };
    }

    /** 每个数据类型的列定义（列名、渲染器与顺序都集中在这里） */
    function assetTableColumns(type) {
        const indexColumn = assetTableColumn('index', 'assets.type.colIndex', '序号', null, 'asset-type-col-index');
        const sourceColumn = assetTableColumn('source', 'assets.type.colSource', '来源', function (row) {
            const source = assetTableText(row.source)
                || (Array.isArray(row.sources) ? assetTableText(row.sources[0]) : '');
            return assetTableSourceCell(source);
        });

        if (type === 'subdomain') {
            return [
                indexColumn,
                assetTableColumn('domain', 'assets.type.colDomain', '域名', assetTableDomainCell),
                assetTableColumn('record_type', 'assets.type.colRecordType', '解析类型', function (row) {
                    return assetTableCellText(row.record_type);
                }),
                assetTableColumn('record_value', 'assets.type.colRecordValue', '记录值', function (row) {
                    return assetTableCellTruncated(row.record_value);
                }),
                assetTableColumn('ips', 'assets.type.colIps', '关联IP', assetTableIpsCell),
                sourceColumn
            ];
        }
        if (type === 'site') {
            return [
                indexColumn,
                // 截图列由 assetTableBodyHtml 特殊渲染（需要行下标 / 分页信息）
                assetTableColumn('screenshot', 'assets.type.colScreenshot', '截图', null, 'asset-type-col-shot'),
                assetTableColumn('domain', 'assets.type.colDomain', '域名', assetTableDomainCell),
                assetTableColumn('protocol', 'assets.type.colProtocol', '协议', function (row) {
                    return assetTableCellText(row.protocol);
                }),
                assetTableColumn('port', 'assets.type.colPort', '端口', function (row) {
                    return assetTablePortCell(row.port);
                }),
                assetTableColumn('title', 'assets.type.colTitle', '标题', function (row) {
                    return assetTableCellTruncated(row.title);
                }),
                assetTableColumn('ips', 'assets.type.colIps', '关联IP', assetTableIpsCell),
                sourceColumn
            ];
        }
        if (type === 'ip') {
            return [
                indexColumn,
                assetTableColumn('ip', 'assets.type.colIp', 'IP', function (row) {
                    return assetTableCellText(row.ip);
                }),
                assetTableColumn('host_count', 'assets.type.colHostCount', '关联域名数', function (row) {
                    return assetTableCellNumber(row.asset_count);
                }),
                assetTableColumn('port_count', 'assets.type.colPortCount', '关联端口数', function (row) {
                    return assetTableCellNumber(Array.isArray(row.ports) ? row.ports.length : 0);
                }),
                sourceColumn
            ];
        }
        // service
        return [
            indexColumn,
            assetTableColumn('host', 'assets.type.colHost', '主机', assetTableServiceHostCell),
            assetTableColumn('port_protocol', 'assets.type.colPortProtocol', '端口/协议', assetTablePortProtocolCell),
            assetTableColumn('service', 'assets.type.colService', '服务', function (row) {
                return assetTableCellText(row.service);
            }),
            assetTableColumn('version', 'assets.type.colVersion', '版本', function (row) {
                return assetTableCellText(row.version);
            }),
            sourceColumn
        ];
    }

    /* ------------------------------------------------------------------ *
     * 渲染
     * ------------------------------------------------------------------ */

    function assetTableHeaderHtml(type) {
        const columns = assetTableColumns(type);
        return '<tr>' + columns.map(function (column) {
            const label = assetTableT(column.labelKey, column.label);
            return '<th scope="col">' + assetTableEscape(label) + '</th>';
        }).join('') + '</tr>';
    }

    function assetTableBodyHtml(type, state) {
        const columns = assetTableColumns(type);
        const colspan = String(columns.length);
        if (state.loading) {
            return '<tr><td colspan="' + colspan + '" class="asset-type-state-cell">'
                + '<div class="asset-type-state"><span class="asset-type-spinner" aria-hidden="true"></span>'
                + '<span>' + assetTableEscape(assetTableT('common.loading', '加载中...')) + '</span></div></td></tr>';
        }
        if (state.error) {
            return '<tr><td colspan="' + colspan + '" class="asset-type-state-cell">'
                + '<div class="asset-type-state asset-type-state--error">'
                + '<span>' + assetTableEscape(state.error) + '</span>'
                + '<button class="btn-secondary" type="button" data-asset-table-action="retry">'
                + assetTableEscape(assetTableT('assets.tree.retry', '重试')) + '</button>'
                + '</div></td></tr>';
        }
        if (!state.rows.length) {
            return '<tr><td colspan="' + colspan + '" class="asset-type-state-cell">'
                + '<div class="asset-type-state">'
                + assetTableEscape(assetTableT('common.noData', '暂无数据'))
                + '</div></td></tr>';
        }

        const pageSize = state.pageSize > 0 ? state.pageSize : ASSET_TABLE_PAGE_SIZE;
        const offset = (Math.max(1, state.page) - 1) * pageSize;
        // 站点 tab 的「仅看无截图」为前端过滤：保留原始行下标，序号不重排
        const onlyNoShot = type === 'site' && state.onlyNoShot;
        const renderedRows = [];
        state.rows.forEach(function (row, index) {
            const safeRow = row && typeof row === 'object' ? row : {};
            if (onlyNoShot && assetTableShotMeta(safeRow).status === 'done') return;
            renderedRows.push({ row: safeRow, index: index });
        });
        if (!renderedRows.length) {
            return '<tr><td colspan="' + colspan + '" class="asset-type-state-cell">'
                + '<div class="asset-type-state">'
                + assetTableEscape(assetTableT('common.noData', '暂无数据'))
                + '</div></td></tr>';
        }

        const parts = [];
        state.pendingShots = [];
        renderedRows.forEach(function (entry) {
            const cells = columns.map(function (column) {
                if (column.key === 'screenshot') {
                    // 截图列需要行下标与页码来生成稳定的 uid，单独渲染
                    const uid = assetTableShotUid(type, state.page, entry.index);
                    const built = assetTableBuildShotCell(type, entry.row, entry.index, state.page);
                    if (built.shot) state.pendingShots.push(built.shot);
                    return '<td class="asset-type-col-shot" id="asset-shot-cell-' + uid + '">' + built.html + '</td>';
                }
                const className = column.className ? ' class="' + column.className + '"' : '';
                const content = column.render ? column.render(entry.row) : String(offset + entry.index + 1);
                return '<td' + className + '>' + content + '</td>';
            }).join('');
            parts.push('<tr>' + cells + '</tr>');
        });
        return parts.join('');
    }

    function assetTablePaginationHtml(state) {
        if (state.loading || state.error) return '';
        const pageSize = state.pageSize > 0 ? state.pageSize : ASSET_TABLE_PAGE_SIZE;
        const totalPages = Math.max(1, Math.ceil(state.total / pageSize));
        const page = Math.min(Math.max(1, state.page), totalPages);
        const totalText = assetTableT('assets.type.total', '共 {{count}} 条', { count: state.total });
        const pageText = assetTableT('assets.type.pageInfo', '第 {{page}} / {{total}} 页', {
            page: page,
            total: totalPages
        });
        const prevDisabled = page <= 1 ? ' disabled' : '';
        const nextDisabled = page >= totalPages ? ' disabled' : '';
        return '<span class="asset-type-total">' + assetTableEscape(totalText) + '</span>'
            + '<span class="asset-type-pager">'
            + '<button class="asset-type-page-btn" type="button" data-asset-table-page="' + (page - 1) + '"' + prevDisabled + '>'
            + assetTableEscape(assetTableT('assets.type.prev', '上一页')) + '</button>'
            + '<span class="asset-type-page-indicator">' + assetTableEscape(pageText) + '</span>'
            + '<button class="asset-type-page-btn" type="button" data-asset-table-page="' + (page + 1) + '"' + nextDisabled + '>'
            + assetTableEscape(assetTableT('assets.type.next', '下一页')) + '</button>'
            + '</span>';
    }

    /** 渲染当前数据 tab 的表头、表体与分页 */
    function assetTableRender(type) {
        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) return;
        if (type !== assetTableState.current) return;
        const state = assetTableState.types[type];

        // 表体重建：先停轮询、释放上一批截图 blob URL，再递增渲染序号丢弃过期异步结果
        assetTableShotStopAllPollers();
        assetTableShotReleaseAllBlobs();
        assetTableShotState.renderSeq += 1;
        state.pendingShots = [];
        if (assetTableShotState.lightbox.open) assetTableCloseLightbox();

        const head = assetTableEl('asset-type-table-head');
        if (head) head.innerHTML = assetTableHeaderHtml(type);

        const body = assetTableEl('asset-type-table-body');
        if (body) body.innerHTML = assetTableBodyHtml(type, state);

        // 表体插入后异步拉取站点截图（有 Authorization，必须转 blob URL）
        if (type === 'site') {
            assetTableShotHydrate(state);
            // 对 pending / running 行自动轮询状态
            assetTableShotStartStatusWatchers(state);
        }

        const pagination = assetTableEl('asset-type-pagination');
        if (pagination) pagination.innerHTML = assetTablePaginationHtml(state);
    }

    /* ------------------------------------------------------------------ *
     * 筛选与布局
     * ------------------------------------------------------------------ */

    /** 把当前输入框的值写回对应类型的状态 */
    function assetTableReadFilters(type) {
        if (type !== assetTableState.current) return;
        const state = assetTableState.types[type];
        const searchInput = assetTableEl('asset-type-search');
        const sourceInput = assetTableEl('asset-type-source');
        const recordSelect = assetTableEl('asset-type-record-filter');
        const noShotInput = assetTableEl('asset-type-no-shot-filter');
        if (searchInput) state.q = String(searchInput.value || '').trim();
        if (sourceInput) state.source = String(sourceInput.value || '').trim();
        if (type === 'subdomain' && recordSelect) state.recordType = String(recordSelect.value || '');
        if (type === 'site' && noShotInput) state.onlyNoShot = !!noShotInput.checked;
    }

    /** 把类型状态写回输入框（切换 tab 时恢复该 tab 的筛选条件） */
    function assetTableSyncFilters(type) {
        const state = assetTableState.types[type];
        const searchInput = assetTableEl('asset-type-search');
        const sourceInput = assetTableEl('asset-type-source');
        const recordSelect = assetTableEl('asset-type-record-filter');
        const noShotWrap = assetTableEl('asset-type-no-shot-wrap');
        const noShotInput = assetTableEl('asset-type-no-shot-filter');
        if (searchInput) searchInput.value = state.q || '';
        if (sourceInput) sourceInput.value = state.source || '';
        if (recordSelect) {
            recordSelect.hidden = type !== 'subdomain';
            recordSelect.value = type === 'subdomain' ? (state.recordType || '') : '';
        }
        if (noShotWrap) noShotWrap.hidden = type !== 'site';
        if (noShotInput) noShotInput.checked = type === 'site' ? !!state.onlyNoShot : false;
    }

    /** 根据当前 tab 控制列表 / 表格 / 占位 / 层级树的显隐 */
    function assetTableApplyLayout(type) {
        const page = assetTableEl('page-asset-library');
        // 非「全部资产」tab 统一隐藏 assets.js 的工具栏 / 批量条 / 列表卡片（纯 CSS）
        if (page) page.classList.toggle('asset-library--non-list', type !== 'all');

        const tableView = assetTableEl('asset-table-view');
        if (tableView) tableView.hidden = ASSET_TABLE_DATA_TYPES.indexOf(type) === -1;

        const placeholderView = assetTableEl('asset-placeholder-view');
        if (placeholderView) placeholderView.hidden = ASSET_TABLE_PLACEHOLDER_TYPES.indexOf(type) === -1;

        const companyView = assetTableEl('asset-company-view');
        if (companyView) companyView.hidden = type !== 'company';

        // 层级树容器由 assets-tree.js 自己管理显隐；非 tree tab 时收起
        if (typeof window.assetTreeSetView === 'function') {
            window.assetTreeSetView(type === 'tree' ? 'tree' : 'list');
        }
    }

    function assetTableRenderTabs() {
        ASSET_TABLE_TABS.forEach(function (type) {
            const button = assetTableEl('asset-tab-' + type);
            if (!button) return;
            const active = type === assetTableState.current;
            button.classList.toggle('is-active', active);
            button.setAttribute('aria-selected', active ? 'true' : 'false');
        });
    }

    /* ------------------------------------------------------------------ *
     * 数据加载
     * ------------------------------------------------------------------ */

    function assetTableIsPageActive() {
        const page = assetTableEl('page-asset-library');
        if (page && page.classList.contains('active')) return true;
        const hash = String(window.location.hash || '').replace(/^#/, '').split('?')[0];
        return hash === 'asset-library';
    }

    /**
     * 拉取并渲染指定类型的表格数据。
     * @param {string} type  subdomain|site|ip|service
     * @param {number} page  页码（从 1 开始，缺省用当前页）
     */
    async function assetTableLoad(type, page) {
        type = type || assetTableState.current;
        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) return;
        const state = assetTableState.types[type];
        assetTableReadFilters(type);

        let nextPage = Number(page);
        if (!Number.isFinite(nextPage) || nextPage < 1) nextPage = state.page || 1;
        nextPage = Math.floor(nextPage);
        // 已知总数时先把页码夹紧，避免越界请求
        if (state.total > 0) {
            const maxPage = Math.max(1, Math.ceil(state.total / (state.pageSize || ASSET_TABLE_PAGE_SIZE)));
            if (nextPage > maxPage) nextPage = maxPage;
        }
        state.page = nextPage;
        state.loading = true;
        state.error = '';
        const seq = ++state.requestSeq;
        if (type === assetTableState.current) assetTableRender(type);

        try {
            const query = [];
            const pushParam = function (name, value) {
                if (value !== '' && value != null) {
                    query.push(encodeURIComponent(name) + '=' + encodeURIComponent(String(value)));
                }
            };
            pushParam('type', type);
            pushParam('page', state.page);
            pushParam('page_size', ASSET_TABLE_PAGE_SIZE);
            pushParam('q', state.q);
            pushParam('source', state.source);
            if (type === 'subdomain') pushParam('record_type', state.recordType);

            const url = ASSET_TABLE_API + (query.length ? '?' + query.join('&') : '');
            // 统一走全局 apiFetch（自动带 Authorization、401 弹登录框）
            const response = typeof apiFetch === 'function' ? await apiFetch(url) : await fetch(url);
            if (seq !== state.requestSeq) return; // 已被新的请求取代
            if (!response || !response.ok) {
                throw new Error('HTTP ' + (response ? response.status : 'error'));
            }
            const payload = await response.json();
            if (seq !== state.requestSeq) return;

            state.rows = Array.isArray(payload && payload.rows) ? payload.rows : [];
            state.total = assetTableNumber(payload && payload.total);
            state.pageSize = assetTableNumber(payload && payload.page_size) || ASSET_TABLE_PAGE_SIZE;
            const responsePage = assetTableNumber(payload && payload.page);
            if (responsePage > 0) state.page = responsePage;
            state.loaded = true;
            state.loading = false;
            state.error = '';

            // 过滤条件变化后落在超出范围的页码：回退到最后一页重新请求一次
            const totalPages = Math.max(1, Math.ceil(state.total / state.pageSize));
            if (state.total > 0 && state.page > totalPages && state.page !== totalPages) {
                return assetTableLoad(type, totalPages);
            }
        } catch (error) {
            if (seq !== state.requestSeq) return;
            console.error('加载资产类型表格失败:', error);
            state.loading = false;
            state.loaded = false;
            const reason = error && error.message ? '（' + error.message + '）' : '';
            state.error = assetTableT('assets.loadFailed', '加载失败') + reason;
        }
        if (type === assetTableState.current) assetTableRender(type);
    }

    /** 搜索 / 筛选变化：回到第 1 页重新请求 */
    function assetTableApplySearch() {
        const type = assetTableState.current;
        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) return;
        assetTableReadFilters(type);
        assetTableState.types[type].page = 1;
        void assetTableLoad(type, 1);
    }

    /** 重置当前 tab 的筛选条件 */
    function assetTableResetFilters() {
        const type = assetTableState.current;
        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) return;
        const state = assetTableState.types[type];
        state.q = '';
        state.source = '';
        state.recordType = '';
        state.onlyNoShot = false;
        state.page = 1;
        assetTableSyncFilters(type);
        void assetTableLoad(type, 1);
    }

    /** 刷新当前 tab 的当前页 */
    function assetTableRefresh() {
        const type = assetTableState.current;
        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) return;
        void assetTableLoad(type, assetTableState.types[type].page || 1);
    }

    /* ------------------------------------------------------------------ *
     * 公司 tab：公司列表 + 待归属主域名
     * ------------------------------------------------------------------ */

    function assetCompanyClearLookup() {
        return { companyId: '', loading: false, error: '', warnings: [], suggestions: [] };
    }

    function assetCompanyJsonOptions(method, payload) {
        return {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload || {})
        };
    }

    /** 公司相关请求统一出口：优先展示后端 error 字段，网络/401 异常也转成可展示错误 */
    async function assetCompanyRequest(url, options) {
        let response;
        try {
            response = typeof apiFetch === 'function' ? await apiFetch(url, options) : await fetch(url, options);
        } catch (error) {
            return {
                ok: false,
                status: 0,
                data: null,
                error: error && error.message ? error.message : assetCompanyT('assets.company.loadFailed', '加载公司数据失败')
            };
        }
        let raw = '';
        try {
            raw = await response.text();
        } catch (error) {
            raw = '';
        }
        let data = null;
        if (raw) {
            try {
                data = JSON.parse(raw);
            } catch (error) {
                data = null;
            }
        }
        if (!response || !response.ok) {
            const message = (data && (data.error || data.message)) || ('HTTP ' + (response ? response.status : 'error'));
            return { ok: false, status: response ? response.status : 0, data: data, error: message };
        }
        return { ok: true, status: response ? response.status : 0, data: data || {}, error: '' };
    }

    function assetCompanyT(key, fallback, options) {
        return assetTableT(key, fallback, options);
    }

    function assetCompanySourceBadge(source) {
        const text = assetTableText(source);
        if (!text) return '';
        return '<span class="asset-type-source-badge">' + assetTableEscape(text) + '</span>';
    }

    function assetCompanyFormatTime(value) {
        const text = assetTableText(value);
        if (!text) return '';
        const date = new Date(text);
        if (!date || Number.isNaN(date.getTime())) return text;
        return date.toLocaleString();
    }

    function assetCompanyEmptyHtml(key, fallback) {
        return '<div class="asset-company-empty">' + assetTableEscape(assetCompanyT(key, fallback)) + '</div>';
    }

    function assetCompanyConfirm(message) {
        if (typeof window.confirm !== 'function') return true;
        try {
            return !!window.confirm(message);
        } catch (error) {
            return false;
        }
    }

    function assetCompanySetNotice(type, text) {
        assetCompanyState.notice = type && text ? { type: type, text: text } : { type: '', text: '' };
        assetCompanyRenderNotice();
    }

    function assetCompanyRenderNotice() {
        const notice = assetTableEl('company-notice');
        if (!notice) return;
        const current = assetCompanyState.notice || { type: '', text: '' };
        notice.hidden = !current.text;
        notice.className = 'asset-company-notice'
            + (current.type === 'error' ? ' asset-company-notice--error' : '')
            + (current.type === 'success' ? ' asset-company-notice--success' : '');
        notice.textContent = current.text || '';
    }

    function assetCompanySetFormError(text) {
        const errorEl = assetTableEl('company-create-error');
        if (!errorEl) return;
        errorEl.textContent = text || '';
        errorEl.hidden = !text;
    }

    /** 更新 tab 角标与待归属数量 */
    function assetCompanyUpdateBadge() {
        const count = Array.isArray(assetCompanyState.pending) ? assetCompanyState.pending.length : 0;
        const badge = assetTableEl('company-tab-badge');
        if (badge) {
            badge.textContent = count > 0 ? String(count) : '';
            badge.hidden = count <= 0;
        }
        const countEl = assetTableEl('company-pending-count');
        if (countEl) countEl.textContent = String(count);
    }

    function assetCompanyRender() {
        const view = assetTableEl('asset-company-view');
        if (!view) return;
        assetCompanyUpdateBadge();
        assetCompanyRenderNotice();

        const status = assetTableEl('company-status');
        const pendingSection = assetTableEl('company-pending-section');
        const listSection = assetTableEl('company-list-section');
        const ready = assetCompanyState.loaded && !assetCompanyState.loading && !assetCompanyState.error;

        if (status) {
            if (assetCompanyState.loading) {
                status.hidden = false;
                status.innerHTML = '<div class="asset-type-state"><span class="asset-type-spinner" aria-hidden="true"></span><span>'
                    + assetTableEscape(assetCompanyT('common.loading', '加载中...')) + '</span></div>';
            } else if (assetCompanyState.error) {
                status.hidden = false;
                status.innerHTML = '<div class="asset-type-state asset-type-state--error"><span>'
                    + assetTableEscape(assetCompanyState.error) + '</span>'
                    + '<button class="btn-secondary" type="button" data-company-action="retry">'
                    + assetTableEscape(assetCompanyT('assets.company.retry', '重试')) + '</button></div>';
            } else {
                status.hidden = true;
                status.innerHTML = '';
            }
        }
        if (pendingSection) pendingSection.hidden = !ready;
        if (listSection) listSection.hidden = !ready;

        const pendingList = assetTableEl('company-pending-list');
        const companyList = assetTableEl('company-list');
        if (!ready) {
            if (pendingList) pendingList.innerHTML = '';
            if (companyList) companyList.innerHTML = '';
            return;
        }
        if (pendingList) pendingList.innerHTML = assetCompanyPendingHtml();
        if (companyList) companyList.innerHTML = assetCompanyCompaniesHtml();
    }

    function assetCompanyPendingHtml() {
        const pending = Array.isArray(assetCompanyState.pending) ? assetCompanyState.pending : [];
        if (!pending.length) return assetCompanyEmptyHtml('assets.company.emptyPending', '暂无待归属主域名');
        const companies = Array.isArray(assetCompanyState.companies) ? assetCompanyState.companies : [];
        const options = companies.map(function (company) {
            return '<option value="' + assetTableEscapeAttr(company.id) + '">' + assetTableEscape(company.name) + '</option>';
        }).join('');
        return pending.map(function (item) {
            const id = assetTableText(item && item.id);
            const firstSeen = assetCompanyFormatTime(item && item.created_at);
            const assetCount = assetTableNumber(item && item.asset_count);
            let actions = '<div class="asset-company-pending-actions">';
            if (companies.length) {
                actions += '<select class="asset-company-select" id="company-pending-select-' + assetTableEscapeAttr(id) + '" aria-label="'
                    + assetTableEscapeAttr(assetCompanyT('assets.company.selectCompany', '选择公司')) + '">'
                    + '<option value="">' + assetTableEscape(assetCompanyT('assets.company.selectCompany', '选择公司')) + '</option>'
                    + options + '</select>'
                    + '<button class="btn-primary" type="button" data-company-action="confirm-pending" data-pending-id="'
                    + assetTableEscapeAttr(id) + '">' + assetTableEscape(assetCompanyT('assets.company.confirmOwnership', '确认归属')) + '</button>';
            } else {
                actions += '<span class="asset-company-meta">' + assetTableEscape(assetCompanyT('assets.company.needCompany', '请先新增公司')) + '</span>';
            }
            actions += '<button class="btn-link" type="button" data-company-action="ignore-pending" data-pending-id="'
                + assetTableEscapeAttr(id) + '">' + assetTableEscape(assetCompanyT('assets.company.ignore', '忽略')) + '</button></div>';

            return '<div class="asset-company-pending-item">'
                + '<div class="asset-company-pending-main">'
                + '<strong class="asset-company-domain">' + assetTableEscape(item && item.domain) + '</strong>'
                + assetCompanySourceBadge(item && item.source)
                + (firstSeen ? '<span class="asset-company-meta">' + assetTableEscape(assetCompanyT('assets.company.pendingFirstSeen', '首次发现：{{time}}', { time: firstSeen })) + '</span>' : '')
                + '<span class="asset-company-meta">' + assetTableEscape(assetCompanyT('assets.company.pendingAssets', '关联资产 {{count}}', { count: assetCount })) + '</span>'
                + (assetTableText(item && item.note) ? '<span class="asset-company-meta">' + assetTableEscape(item.note) + '</span>' : '')
                + '</div>' + actions + '</div>';
        }).join('');
    }

    function assetCompanyDomainHtml(company, domain) {
        const domainId = assetTableText(domain && domain.id);
        const count = assetTableNumber(domain && domain.asset_count);
        return '<li class="asset-company-domain-item">'
            + '<div class="asset-company-domain-main">'
            + '<strong class="asset-company-domain">' + assetTableEscape(domain && domain.domain) + '</strong>'
            + assetCompanySourceBadge(domain && domain.source)
            + '<span class="asset-company-meta">' + assetTableEscape(assetCompanyT('assets.company.assetCount', '资产 {{count}}', { count: count })) + '</span>'
            + '</div>'
            + '<button class="btn-link asset-company-domain-remove" type="button" data-company-action="remove-domain"'
            + ' data-company-id="' + assetTableEscapeAttr(company && company.id) + '"'
            + ' data-domain-id="' + assetTableEscapeAttr(domainId) + '">'
            + assetTableEscape(assetCompanyT('common.remove', '移除')) + '</button>'
            + '</li>';
    }

    function assetCompanyLookupHtml(company) {
        const companyId = assetTableText(company && company.id);
        const lookup = assetCompanyState.lookup || assetCompanyClearLookup();
        let html = '<div class="asset-company-lookup">'
            + '<div class="asset-company-lookup-head"><h4>' + assetTableEscape(assetCompanyT('assets.company.lookupTitle', '自动查找建议')) + '</h4>'
            + '<button class="btn-link" type="button" data-company-action="cancel-lookup" data-company-id="' + assetTableEscapeAttr(companyId) + '">'
            + assetTableEscape(assetCompanyT('common.close', '关闭')) + '</button></div>';

        if (lookup.loading) {
            html += '<div class="asset-type-state"><span class="asset-type-spinner" aria-hidden="true"></span><span>'
                + assetTableEscape(assetCompanyT('common.loading', '加载中...')) + '</span></div>';
        } else {
            if (lookup.error) {
                html += '<div class="asset-company-error">' + assetTableEscape(lookup.error) + '</div>';
            }
            if (lookup.warnings && lookup.warnings.length) {
                html += '<div class="asset-company-warnings"><strong>' + assetTableEscape(assetCompanyT('assets.company.lookupWarnings', '提示')) + '</strong>'
                    + lookup.warnings.map(function (warning) {
                        return '<div class="asset-company-warning">' + assetTableEscape(warning) + '</div>';
                    }).join('') + '</div>';
            }
            const suggestions = Array.isArray(lookup.suggestions) ? lookup.suggestions : [];
            if (!suggestions.length) {
                html += assetCompanyEmptyHtml('assets.company.lookupEmpty', '没有发现新的主域名建议');
            } else {
                html += '<ul class="asset-company-suggestions">' + suggestions.map(function (suggestion, index) {
                    const domain = assetTableText(suggestion && suggestion.domain);
                    return '<li class="asset-company-suggestion">'
                        + '<label class="asset-company-suggestion-label">'
                        + '<input type="checkbox" id="company-suggestion-' + assetTableEscapeAttr(companyId) + '-' + index + '" value="' + assetTableEscapeAttr(domain) + '">'
                        + '<span class="asset-company-domain">' + assetTableEscape(domain) + '</span>'
                        + assetCompanySourceBadge(suggestion && suggestion.source)
                        + '<span class="asset-company-meta">' + assetTableEscape(assetCompanyT('assets.company.lookupHits', '证据 {{count}} 条', { count: assetTableNumber(suggestion && suggestion.hits) })) + '</span>'
                        + (assetTableText(suggestion && suggestion.note) ? '<span class="asset-company-meta">' + assetTableEscape(suggestion.note) + '</span>' : '')
                        + '</label></li>';
                }).join('') + '</ul>';
                html += '<div class="asset-company-form-actions"><button class="btn-primary" type="button" data-company-action="add-domains" data-company-id="'
                    + assetTableEscapeAttr(companyId) + '">' + assetTableEscape(assetCompanyT('assets.company.lookupAddSelected', '加入公司')) + '</button></div>';
            }
        }
        html += '</div>';
        return html;
    }

    function assetCompanyCardHtml(company) {
        const companyId = assetTableText(company && company.id);
        const name = assetTableText(company && company.name);
        const note = assetTableText(company && company.note);
        const assetCount = assetTableNumber(company && company.asset_count);
        const domains = Array.isArray(company && company.domains) ? company.domains : [];
        const editing = assetCompanyState.editingId === companyId;
        const lookup = assetCompanyState.lookup && assetCompanyState.lookup.companyId === companyId;

        let html = '<article class="asset-company-card" data-company-id="' + assetTableEscapeAttr(companyId) + '">';
        html += '<div class="asset-company-card-head"><div class="asset-company-card-title">'
            + '<h3>' + assetTableEscape(name) + '</h3>'
            + '<span class="asset-company-asset-count">' + assetTableEscape(assetCompanyT('assets.company.assetCount', '资产 {{count}}', { count: assetCount })) + '</span>'
            + '</div><div class="asset-company-card-actions">'
            + '<button class="btn-secondary" type="button" data-company-action="lookup" data-company-id="' + assetTableEscapeAttr(companyId) + '">'
            + assetTableEscape(assetCompanyT('assets.company.lookup', '自动查找主域名')) + '</button>'
            + '<button class="btn-secondary" type="button" data-company-action="edit" data-company-id="' + assetTableEscapeAttr(companyId) + '">'
            + assetTableEscape(assetCompanyT('common.edit', '编辑')) + '</button>'
            + '<button class="btn-link" type="button" data-company-action="delete" data-company-id="' + assetTableEscapeAttr(companyId) + '">'
            + assetTableEscape(assetCompanyT('common.delete', '删除')) + '</button>'
            + '</div></div>';

        if (note) html += '<p class="asset-company-note">' + assetTableEscape(note) + '</p>';

        if (editing) {
            html += '<div class="asset-company-edit">'
                + '<label class="asset-company-field"><span>' + assetTableEscape(assetCompanyT('assets.company.name', '公司名')) + '</span>'
                + '<input id="company-edit-name-' + assetTableEscapeAttr(companyId) + '" type="text" maxlength="200" value="' + assetTableEscapeAttr(name) + '"></label>'
                + '<label class="asset-company-field"><span>' + assetTableEscape(assetCompanyT('assets.company.note', '备注')) + '</span>'
                + '<input id="company-edit-note-' + assetTableEscapeAttr(companyId) + '" type="text" maxlength="500" value="' + assetTableEscapeAttr(note) + '"></label>'
                + '<div class="asset-company-form-actions">'
                + '<button class="btn-primary" type="button" data-company-action="save-edit" data-company-id="' + assetTableEscapeAttr(companyId) + '">'
                + assetTableEscape(assetCompanyT('common.save', '保存')) + '</button>'
                + '<button class="btn-secondary" type="button" data-company-action="cancel-edit" data-company-id="' + assetTableEscapeAttr(companyId) + '">'
                + assetTableEscape(assetCompanyT('common.cancel', '取消')) + '</button></div></div>';
        }

        html += '<div class="asset-company-domains"><h4>'
            + assetTableEscape(assetCompanyT('assets.company.domainsTitle', '主域名（{{count}}）', { count: domains.length }))
            + '</h4>';
        if (!domains.length) {
            html += assetCompanyEmptyHtml('assets.company.noDomains', '暂无主域名');
        } else {
            html += '<ul class="asset-company-domain-list">' + domains.map(function (domain) {
                return assetCompanyDomainHtml(company, domain);
            }).join('') + '</ul>';
        }
        html += '</div>';

        if (lookup) html += assetCompanyLookupHtml(company);
        html += '</article>';
        return html;
    }

    function assetCompanyCompaniesHtml() {
        const companies = Array.isArray(assetCompanyState.companies) ? assetCompanyState.companies : [];
        if (!companies.length) return assetCompanyEmptyHtml('assets.company.emptyCompanies', '暂无公司');
        return companies.map(assetCompanyCardHtml).join('');
    }

    function assetCompanyFindCompany(companyId) {
        return (assetCompanyState.companies || []).find(function (company) {
            return String(company && company.id) === String(companyId);
        }) || null;
    }

    function assetCompanyParseDomains(value) {
        const seen = Object.create(null);
        const list = [];
        String(value || '').split(/[\s,，;；]+/).forEach(function (part) {
            const domain = part.trim();
            if (!domain) return;
            const key = domain.toLowerCase();
            if (seen[key]) return;
            seen[key] = true;
            list.push(domain);
        });
        return list;
    }

    async function assetCompanyLoad(force) {
        if (assetCompanyState.loading) return;
        if (assetCompanyState.loaded && !force) {
            assetCompanyRender();
            return;
        }
        const seq = ++assetCompanyState.requestSeq;
        assetCompanyState.loading = true;
        assetCompanyState.error = '';
        assetCompanyRender();

        try {
            const results = await Promise.all([
                assetCompanyRequest(ASSET_COMPANY_API),
                assetCompanyRequest(ASSET_COMPANY_DOMAINS_API + '?status=pending')
            ]);
            if (seq !== assetCompanyState.requestSeq) return;
            const companiesResult = results[0];
            const pendingResult = results[1];
            if (!companiesResult.ok) throw new Error(companiesResult.error);
            if (!pendingResult.ok) throw new Error(pendingResult.error);
            const companies = companiesResult.data ? companiesResult.data.companies : null;
            const pending = pendingResult.data ? pendingResult.data.domains : null;
            assetCompanyState.companies = Array.isArray(companies) ? companies : [];
            assetCompanyState.pending = Array.isArray(pending) ? pending : [];
            assetCompanyState.pendingLoaded = true;
            assetCompanyState.loaded = true;
            assetCompanyState.loading = false;
            assetCompanyState.error = '';
            assetCompanyState.editingId = '';
            assetCompanyState.lookup = assetCompanyClearLookup();
        } catch (error) {
            if (seq !== assetCompanyState.requestSeq) return;
            console.error('加载公司数据失败:', error);
            assetCompanyState.loading = false;
            assetCompanyState.loaded = false;
            assetCompanyState.error = error && error.message
                ? error.message
                : assetCompanyT('assets.company.loadFailed', '加载公司数据失败');
        }
        assetCompanyRender();
    }

    /** 只刷新待归属列表（用于 tab 角标 / 进入资产页时静默更新） */
    async function assetCompanyRefreshPending() {
        if (assetCompanyState.loading) return;
        const result = await assetCompanyRequest(ASSET_COMPANY_DOMAINS_API + '?status=pending');
        if (!result.ok) return;
        const pending = result.data ? result.data.domains : null;
        assetCompanyState.pending = Array.isArray(pending) ? pending : [];
        assetCompanyState.pendingLoaded = true;
        assetCompanyUpdateBadge();
        if (assetCompanyState.loaded && assetTableState.current === 'company') {
            const pendingList = assetTableEl('company-pending-list');
            if (pendingList) pendingList.innerHTML = assetCompanyPendingHtml();
        }
    }

    function assetCompanyEnsureLoaded() {
        if (!assetCompanyState.loaded && !assetCompanyState.loading) {
            void assetCompanyLoad(true);
        } else {
            assetCompanyRender();
        }
    }

    async function assetCompanyCreate() {
        const nameInput = assetTableEl('company-create-name');
        const noteInput = assetTableEl('company-create-note');
        const domainsInput = assetTableEl('company-create-domains');
        const name = nameInput ? String(nameInput.value || '').trim() : '';
        const note = noteInput ? String(noteInput.value || '').trim() : '';
        const domains = assetCompanyParseDomains(domainsInput ? domainsInput.value : '');

        if (!name) {
            assetCompanySetFormError(assetCompanyT('assets.company.nameRequired', '公司名不能为空'));
            return;
        }
        assetCompanySetFormError('');

        const result = await assetCompanyRequest(
            ASSET_COMPANY_API,
            assetCompanyJsonOptions('POST', { name: name, note: note, domains: domains })
        );
        if (!result.ok) {
            assetCompanySetFormError(result.error);
            return;
        }
        if (nameInput) nameInput.value = '';
        if (noteInput) noteInput.value = '';
        if (domainsInput) domainsInput.value = '';
        assetCompanySetNotice('success', assetCompanyT('assets.company.createSuccess', '已新增公司「{{name}}」', { name: name }));
        await assetCompanyLoad(true);
    }

    async function assetCompanySaveEdit(companyId) {
        const nameInput = assetTableEl('company-edit-name-' + companyId);
        const noteInput = assetTableEl('company-edit-note-' + companyId);
        const name = nameInput ? String(nameInput.value || '').trim() : '';
        const note = noteInput ? String(noteInput.value || '').trim() : '';
        if (!name) {
            assetCompanySetNotice('error', assetCompanyT('assets.company.nameRequired', '公司名不能为空'));
            return;
        }
        const result = await assetCompanyRequest(
            ASSET_COMPANY_API + '/' + encodeURIComponent(companyId),
            assetCompanyJsonOptions('PUT', { name: name, note: note })
        );
        if (!result.ok) {
            assetCompanySetNotice('error', result.error);
            return;
        }
        assetCompanySetNotice('success', assetCompanyT('assets.company.editSuccess', '已更新公司信息'));
        await assetCompanyLoad(true);
    }

    async function assetCompanyDelete(companyId) {
        const company = assetCompanyFindCompany(companyId);
        const name = assetTableText(company && company.name) || companyId;
        const message = assetCompanyT('assets.company.deleteConfirm', '确定删除公司「{{name}}」？公司名下主域名会转为待归属。', { name: name });
        if (!assetCompanyConfirm(message)) return;
        const result = await assetCompanyRequest(
            ASSET_COMPANY_API + '/' + encodeURIComponent(companyId),
            { method: 'DELETE' }
        );
        if (!result.ok) {
            assetCompanySetNotice('error', result.error);
            return;
        }
        assetCompanySetNotice('success', assetCompanyT('assets.company.deleteSuccess', '已删除公司「{{name}}」', { name: name }));
        await assetCompanyLoad(true);
    }

    async function assetCompanyRemoveDomain(companyId, domainId) {
        const company = assetCompanyFindCompany(companyId);
        const domains = company && Array.isArray(company.domains) ? company.domains : [];
        const domain = domains.find(function (item) {
            return String(item && item.id) === String(domainId);
        }) || null;
        const domainName = assetTableText(domain && domain.domain) || domainId;
        const message = assetCompanyT(
            'assets.company.removeDomainConfirm',
            '确定从公司「{{company}}」移除主域名「{{domain}}」？移除后该域名会转为待归属。',
            { company: assetTableText(company && company.name), domain: domainName }
        );
        if (!assetCompanyConfirm(message)) return;
        const result = await assetCompanyRequest(
            ASSET_COMPANY_API + '/' + encodeURIComponent(companyId) + '/domains/' + encodeURIComponent(domainId),
            { method: 'DELETE' }
        );
        if (!result.ok) {
            assetCompanySetNotice('error', result.error);
            return;
        }
        assetCompanySetNotice('success', assetCompanyT('assets.company.removeDomainSuccess', '已移除主域名「{{domain}}」', { domain: domainName }));
        await assetCompanyLoad(true);
    }

    async function assetCompanyLookup(companyId) {
        if (!companyId) return;
        assetCompanyState.editingId = '';
        assetCompanyState.lookup = { companyId: companyId, loading: true, error: '', warnings: [], suggestions: [] };
        assetCompanyRender();

        const result = await assetCompanyRequest(
            ASSET_COMPANY_API + '/' + encodeURIComponent(companyId) + '/lookup',
            assetCompanyJsonOptions('POST', {})
        );
        const current = assetCompanyState.lookup;
        // 期间已关闭面板 / 切换了其它公司：丢弃本次结果
        if (!current || current !== assetCompanyState.lookup || current.companyId !== companyId) return;
        if (!result.ok) {
            current.loading = false;
            current.error = result.error;
            assetCompanyRender();
            return;
        }
        const data = result.data || {};
        if (data.company) {
            assetCompanyState.companies = assetCompanyState.companies.map(function (company) {
                return String(company && company.id) === String(companyId) ? data.company : company;
            });
        }
        current.loading = false;
        current.error = '';
        current.warnings = Array.isArray(data.warnings) ? data.warnings.map(function (warning) { return assetTableText(warning); }) : [];
        current.suggestions = Array.isArray(data.suggestions) ? data.suggestions : [];
        assetCompanyRender();
    }

    async function assetCompanyAddSelectedDomains(companyId) {
        const lookup = assetCompanyState.lookup;
        if (!lookup || lookup.companyId !== companyId) return;
        const selected = [];
        (Array.isArray(lookup.suggestions) ? lookup.suggestions : []).forEach(function (suggestion, index) {
            const checkbox = assetTableEl('company-suggestion-' + companyId + '-' + index);
            if (!checkbox || !checkbox.checked) return;
            const domain = assetTableText(suggestion && suggestion.domain);
            if (domain) selected.push({ domain: domain, note: assetTableText(suggestion.note) });
        });
        if (!selected.length) {
            assetCompanySetNotice('error', assetCompanyT('assets.company.lookupNoSelection', '请至少勾选一个主域名'));
            return;
        }

        let successCount = 0;
        let firstError = '';
        for (let i = 0; i < selected.length; i += 1) {
            const item = selected[i];
            const result = await assetCompanyRequest(
                ASSET_COMPANY_API + '/' + encodeURIComponent(companyId) + '/domains',
                assetCompanyJsonOptions('POST', { domain: item.domain, note: item.note })
            );
            if (result.ok) successCount += 1;
            else if (!firstError) firstError = result.error;
        }

        assetCompanyState.lookup = assetCompanyClearLookup();
        if (firstError) assetCompanySetNotice('error', firstError);
        else assetCompanySetNotice('success', assetCompanyT('assets.company.lookupSuccess', '已加入 {{count}} 个主域名', { count: successCount }));
        await assetCompanyLoad(true);
    }

    async function assetCompanyConfirmPending(pendingId) {
        const select = assetTableEl('company-pending-select-' + pendingId);
        const companyId = select ? assetTableText(select.value) : '';
        if (!companyId) {
            assetCompanySetNotice('error', assetCompanyT('assets.company.selectCompany', '选择公司'));
            return;
        }
        const result = await assetCompanyRequest(
            ASSET_COMPANY_DOMAINS_API + '/' + encodeURIComponent(pendingId) + '/confirm',
            assetCompanyJsonOptions('POST', { company_id: companyId })
        );
        if (!result.ok) {
            assetCompanySetNotice('error', result.error);
            return;
        }
        assetCompanySetNotice('success', assetCompanyT('assets.company.confirmSuccess', '已确认主域名归属'));
        await assetCompanyLoad(true);
    }

    async function assetCompanyIgnorePending(pendingId) {
        const result = await assetCompanyRequest(
            ASSET_COMPANY_DOMAINS_API + '/' + encodeURIComponent(pendingId) + '/ignore',
            assetCompanyJsonOptions('POST', {})
        );
        if (!result.ok) {
            assetCompanySetNotice('error', result.error);
            return;
        }
        assetCompanySetNotice('success', assetCompanyT('assets.company.ignoreSuccess', '已忽略主域名'));
        await assetCompanyLoad(true);
    }

    function assetCompanyHandleClick(event) {
        const target = event.target;
        if (!target || typeof target.closest !== 'function') return;
        const actionEl = target.closest('[data-company-action]');
        if (!actionEl) return;
        const action = actionEl.getAttribute('data-company-action');
        const companyId = assetTableText(actionEl.getAttribute('data-company-id'));
        const pendingId = assetTableText(actionEl.getAttribute('data-pending-id'));
        const domainId = assetTableText(actionEl.getAttribute('data-domain-id'));

        if (action === 'retry') {
            event.preventDefault();
            void assetCompanyLoad(true);
            return;
        }
        if (action === 'lookup') {
            event.preventDefault();
            void assetCompanyLookup(companyId);
            return;
        }
        if (action === 'cancel-lookup') {
            event.preventDefault();
            assetCompanyState.lookup = assetCompanyClearLookup();
            assetCompanyRender();
            return;
        }
        if (action === 'edit') {
            event.preventDefault();
            assetCompanyState.editingId = companyId;
            assetCompanyState.lookup = assetCompanyClearLookup();
            assetCompanyRender();
            return;
        }
        if (action === 'cancel-edit') {
            event.preventDefault();
            assetCompanyState.editingId = '';
            assetCompanyRender();
            return;
        }
        if (action === 'save-edit') {
            event.preventDefault();
            void assetCompanySaveEdit(companyId);
            return;
        }
        if (action === 'delete') {
            event.preventDefault();
            void assetCompanyDelete(companyId);
            return;
        }
        if (action === 'remove-domain') {
            event.preventDefault();
            void assetCompanyRemoveDomain(companyId, domainId);
            return;
        }
        if (action === 'add-domains') {
            event.preventDefault();
            void assetCompanyAddSelectedDomains(companyId);
            return;
        }
        if (action === 'confirm-pending') {
            event.preventDefault();
            void assetCompanyConfirmPending(pendingId);
            return;
        }
        if (action === 'ignore-pending') {
            event.preventDefault();
            void assetCompanyIgnorePending(pendingId);
        }
    }

    /* ------------------------------------------------------------------ *
     * tab 切换 / 事件绑定 / 初始化
     * ------------------------------------------------------------------ */

    /** 切换资产类型 tab（「全部资产」「层级」及占位 tab 都在这里统一收口） */
    function assetTableSwitch(type) {
        if (!assetTableState.initialized) {
            assetTableInit();
        }
        if (ASSET_TABLE_TABS.indexOf(type) === -1) return;
        // 切换 tab 时停止轮询、释放截图 blob URL，避免离开页面后仍占内存
        assetTableShotStopAllPollers();
        assetTableShotReleaseAllBlobs();
        if (assetTableShotState.lightbox.open) assetTableCloseLightbox();
        assetTableState.current = type;
        assetTableRenderTabs();
        assetTableApplyLayout(type);

        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) {
            if (type === 'company') assetCompanyEnsureLoaded();
            return;
        }
        const state = assetTableState.types[type];
        assetTableSyncFilters(type);
        assetTableRender(type);
        // 首次进入且资产页可见时才请求；页面未激活时等路由切换后再加载
        if (!state.loaded && !state.loading && assetTableIsPageActive()) {
            void assetTableLoad(type, state.page || 1);
        }
    }

    /** 资产页重新激活时（hashchange）按需加载当前 tab */
    function assetTableHandlePageReady() {
        if (!assetTableIsPageActive()) return;
        // 进入资产页时静默刷新「待归属主域名」角标（只做一次，之后由公司 tab 自己维护）
        if (!assetCompanyState.pendingLoaded && !assetCompanyState.loading) {
            void assetCompanyRefreshPending();
        }
        const type = assetTableState.current;
        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) {
            if (type === 'company') assetCompanyEnsureLoaded();
            return;
        }
        const state = assetTableState.types[type];
        if (!state.loaded && !state.loading) {
            void assetTableLoad(type, state.page || 1);
        }
    }

    function assetTableBindEvents() {
        const searchInput = assetTableEl('asset-type-search');
        const sourceInput = assetTableEl('asset-type-source');
        const recordSelect = assetTableEl('asset-type-record-filter');
        const searchButton = assetTableEl('asset-type-search-btn');
        const resetButton = assetTableEl('asset-type-reset-btn');
        const refreshButton = assetTableEl('asset-type-refresh-btn');
        const tableBody = assetTableEl('asset-type-table-body');
        const pagination = assetTableEl('asset-type-pagination');

        [searchInput, sourceInput].forEach(function (input) {
            if (!input) return;
            input.addEventListener('keydown', function (event) {
                if (event.key === 'Enter') {
                    event.preventDefault();
                    assetTableApplySearch();
                }
            });
        });
        // 浏览器原生搜索框清空按钮
        if (searchInput) searchInput.addEventListener('search', assetTableApplySearch);
        if (recordSelect) {
            recordSelect.addEventListener('change', function () {
                if (assetTableState.current === 'subdomain') assetTableApplySearch();
            });
        }
        if (searchButton) searchButton.addEventListener('click', assetTableApplySearch);
        if (resetButton) resetButton.addEventListener('click', assetTableResetFilters);
        if (refreshButton) refreshButton.addEventListener('click', assetTableRefresh);

        // 站点 tab「仅看无截图」：前端过滤当前页，不额外发请求
        const noShotInput = assetTableEl('asset-type-no-shot-filter');
        if (noShotInput) {
            noShotInput.addEventListener('change', function () {
                if (assetTableState.current !== 'site') return;
                assetTableReadFilters('site');
                assetTableRender('site');
            });
        }

        // 表体交互：失败重试 / 截图预览 / 重新截图
        if (tableBody) {
            tableBody.addEventListener('click', function (event) {
                const target = event.target;
                if (!target || typeof target.closest !== 'function') return;
                const shotAction = target.closest('[data-asset-shot-action]');
                if (shotAction) {
                    const action = shotAction.getAttribute('data-asset-shot-action');
                    if (action === 'preview') {
                        event.preventDefault();
                        assetTableOpenLightbox(shotAction);
                        return;
                    }
                    if (action === 'capture') {
                        event.preventDefault();
                        assetTableShotRequestCapture(shotAction);
                        return;
                    }
                }
                const retryButton = target.closest('[data-asset-table-action="retry"]');
                if (!retryButton) return;
                event.preventDefault();
                assetTableRefresh();
            });
        }
        // 分页按钮
        if (pagination) {
            pagination.addEventListener('click', function (event) {
                const target = event.target;
                if (!target || typeof target.closest !== 'function') return;
                const pageButton = target.closest('[data-asset-table-page]');
                if (!pageButton || pageButton.disabled) return;
                const page = Number(pageButton.getAttribute('data-asset-table-page'));
                if (!Number.isFinite(page) || page < 1) return;
                void assetTableLoad(assetTableState.current, page);
            });
        }

        // 公司 tab：卡片/待归属操作走事件委托，另绑定新增表单与刷新
        const companyView = assetTableEl('asset-company-view');
        if (companyView) companyView.addEventListener('click', assetCompanyHandleClick);
        const companyCreateForm = assetTableEl('company-create-form');
        if (companyCreateForm) {
            companyCreateForm.addEventListener('submit', function (event) {
                event.preventDefault();
                void assetCompanyCreate();
            });
        }
        const companyRefresh = assetTableEl('company-refresh');
        if (companyRefresh) {
            companyRefresh.addEventListener('click', function () {
                void assetCompanyLoad(true);
            });
        }

        // 语言切换后重绘动态表头 / 表体 / 公司面板（静态 data-i18n 由 i18n.js 统一刷新）
        document.addEventListener('languagechange', function () {
            if (!assetTableState.initialized) return;
            if (assetTableState.current === 'company') {
                assetCompanyRender();
                return;
            }
            if (ASSET_TABLE_DATA_TYPES.indexOf(assetTableState.current) !== -1) {
                assetTableRender(assetTableState.current);
            }
        });

        // 大图预览：关闭按钮 / 点击遮罩 / ESC
        const lightbox = assetTableEl('asset-shot-lightbox');
        const lightboxClose = assetTableEl('asset-shot-lightbox-close');
        if (lightboxClose) {
            lightboxClose.addEventListener('click', function (event) {
                event.preventDefault();
                assetTableCloseLightbox();
            });
        }
        if (lightbox) {
            lightbox.addEventListener('click', function (event) {
                if (event.target === lightbox) assetTableCloseLightbox();
            });
        }
        document.addEventListener('keydown', function (event) {
            if (event.key === 'Escape' && assetTableShotState.lightbox.open) {
                event.preventDefault();
                assetTableCloseLightbox();
            }
        });

        // 路由切回资产页时按需加载（router.js 不改动，用 hashchange 观察）；
        // 离开资产页时停止截图轮询并释放 blob URL
        window.addEventListener('hashchange', function () {
            if (!assetTableIsPageActive()) {
                assetTableShotStopAllPollers();
                assetTableShotReleaseAllBlobs();
                return;
            }
            assetTableHandlePageReady();
        });
    }

    /** 初始化：绑定事件、渲染默认 tab（幂等） */
    function assetTableInit() {
        if (assetTableState.initialized) return;
        const tableView = assetTableEl('asset-table-view');
        if (!tableView) return;
        assetTableState.initialized = true;

        assetTableBindEvents();

        // 页面已激活（深链）时先取一次待归属数量，用于 tab 角标
        if (assetTableIsPageActive() && !assetCompanyState.pendingLoaded) {
            void assetCompanyRefreshPending();
        }

        // 若树先于语言包渲染，等 i18n 就绪后补一次重绘（与 assets-tree.js 一致）
        if (window.i18nReady && typeof window.i18nReady.then === 'function') {
            window.i18nReady.then(function () {
                if (assetTableState.current === 'company') {
                    assetCompanyRender();
                    return;
                }
                if (ASSET_TABLE_DATA_TYPES.indexOf(assetTableState.current) !== -1) {
                    assetTableRender(assetTableState.current);
                }
            }).catch(function () {
                /* 忽略语言包加载异常，保留兜底文案 */
            });
        }

        // 默认选中「子域名」；页面未激活时只准备布局，不发请求
        assetTableSwitch(ASSET_TABLE_DEFAULT_TAB);
    }

    /* ------------------------------------------------------------------ *
     * 对外暴露（仅 3 个 API）
     * ------------------------------------------------------------------ */
    window.assetTableInit = assetTableInit;
    window.assetTableLoad = assetTableLoad;
    window.assetTableSwitch = assetTableSwitch;

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', assetTableInit);
    } else {
        assetTableInit();
    }
})();
