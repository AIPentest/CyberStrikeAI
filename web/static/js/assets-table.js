/**
 * 资产页「按类型分 tab + 扁平表格」视图（单文件、自包含、无第三方依赖）
 *
 * 设计：
 *   - 资产页顶部 tab 由 index.html 静态渲染，本文件负责切换与数据加载；
 *   - 数据 tab（子域名 / 站点 / IP / 服务）走 GET /api/assets/table；
 *   - 「全部资产」复用 assets.js 既有列表（本文件只控制显隐，不改其逻辑）；
 *   - 「层级」复用 assets-tree.js 的七层树；
 *   - 「SSL证书 / 文件泄露 / URL信息」为占位 tab，不发起请求；
 *   - 每个数据 tab 有独立筛选状态（关键字 / 来源 / 解析类型）与分页状态，切回时可恢复。
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
        'all', 'tree'
    ];
    const ASSET_TABLE_DEFAULT_TAB = 'subdomain';

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
        const parts = [];
        state.rows.forEach(function (row, index) {
            const safeRow = row && typeof row === 'object' ? row : {};
            const cells = columns.map(function (column) {
                const className = column.className ? ' class="' + column.className + '"' : '';
                const content = column.render ? column.render(safeRow) : String(offset + index + 1);
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

        const head = assetTableEl('asset-type-table-head');
        if (head) head.innerHTML = assetTableHeaderHtml(type);

        const body = assetTableEl('asset-type-table-body');
        if (body) body.innerHTML = assetTableBodyHtml(type, state);

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
        if (searchInput) state.q = String(searchInput.value || '').trim();
        if (sourceInput) state.source = String(sourceInput.value || '').trim();
        if (type === 'subdomain' && recordSelect) state.recordType = String(recordSelect.value || '');
    }

    /** 把类型状态写回输入框（切换 tab 时恢复该 tab 的筛选条件） */
    function assetTableSyncFilters(type) {
        const state = assetTableState.types[type];
        const searchInput = assetTableEl('asset-type-search');
        const sourceInput = assetTableEl('asset-type-source');
        const recordSelect = assetTableEl('asset-type-record-filter');
        if (searchInput) searchInput.value = state.q || '';
        if (sourceInput) sourceInput.value = state.source || '';
        if (recordSelect) {
            recordSelect.hidden = type !== 'subdomain';
            recordSelect.value = type === 'subdomain' ? (state.recordType || '') : '';
        }
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
     * tab 切换 / 事件绑定 / 初始化
     * ------------------------------------------------------------------ */

    /** 切换资产类型 tab（「全部资产」「层级」及占位 tab 都在这里统一收口） */
    function assetTableSwitch(type) {
        if (!assetTableState.initialized) {
            assetTableInit();
        }
        if (ASSET_TABLE_TABS.indexOf(type) === -1) return;
        assetTableState.current = type;
        assetTableRenderTabs();
        assetTableApplyLayout(type);

        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) return;
        const state = assetTableState.types[type];
        assetTableSyncFilters(type);
        assetTableRender(type);
        // 首次进入且资产页可见时才请求；页面未激活时等路由切换后再加载
        if (!state.loaded && !state.loading && assetTableIsPageActive()) {
            void assetTableLoad(type, state.page || 1);
        }
    }

    /** 资产页重新激活时（hashchange）按需加载当前数据 tab */
    function assetTableHandlePageReady() {
        if (!assetTableIsPageActive()) return;
        const type = assetTableState.current;
        if (ASSET_TABLE_DATA_TYPES.indexOf(type) === -1) return;
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

        // 失败重试（表体内部按钮）
        if (tableBody) {
            tableBody.addEventListener('click', function (event) {
                const target = event.target;
                if (!target || typeof target.closest !== 'function') return;
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

        // 语言切换后重绘动态表头 / 表体（静态 data-i18n 由 i18n.js 统一刷新）
        document.addEventListener('languagechange', function () {
            if (assetTableState.initialized && ASSET_TABLE_DATA_TYPES.indexOf(assetTableState.current) !== -1) {
                assetTableRender(assetTableState.current);
            }
        });

        // 路由切回资产页时按需加载（router.js 不改动，用 hashchange 观察）
        window.addEventListener('hashchange', assetTableHandlePageReady);
    }

    /** 初始化：绑定事件、渲染默认 tab（幂等） */
    function assetTableInit() {
        if (assetTableState.initialized) return;
        const tableView = assetTableEl('asset-table-view');
        if (!tableView) return;
        assetTableState.initialized = true;

        assetTableBindEvents();

        // 若树先于语言包渲染，等 i18n 就绪后补一次重绘（与 assets-tree.js 一致）
        if (window.i18nReady && typeof window.i18nReady.then === 'function') {
            window.i18nReady.then(function () {
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
