/**
 * 资产层级树视图（单文件、自包含、无第三方依赖）
 *
 * 功能：
 *   1. 资产库页面内的「列表 / 层级」视图切换（默认列表，不改变现有使用习惯）；
 *   2. 七层树渲染：项目（公司）→ 域名 → 子域名 → IP → 端口 → 服务 → 指纹/版本；
 *   3. 每层节点可展开/折叠，默认展开到「域名」层，并提供展开全部 / 折叠全部；
 *   4. 前端关键字搜索（保留匹配节点的祖先链）并高亮匹配文本；
 *   5. 加载中 / 空数据 / 加载失败 / 无搜索结果等状态占位；
 *   6. 批量 DOM 渲染（template + DocumentFragment），避免逐行插入造成重排。
 *
 * 对接接口：GET /api/assets/tree?project_id=<可选>
 * 通过全局 apiFetch() 发请求（自动附带 Authorization，401 会触发登录框）。
 *
 * 约定：
 *   - 只挂在 window 上的函数以 initAssetTree / loadAssetTree / assetTree 前缀命名，
 *     其余实现全部封装在本 IIFE 内，不新增其它全局变量；
 *   - 所有动态文本都经过转义，节点名称来自后端，禁止直接拼 innerHTML。
 */
(function () {
    'use strict';

    /** 后端层级树接口 */
    const ASSET_TREE_API = '/api/assets/tree';
    /** 搜索输入防抖（毫秒） */
    const ASSET_TREE_SEARCH_DEBOUNCE = 180;

    /**
     * 七层类型元数据：
     *   level  —— 语义层级（用于 aria-level / 样式，不随“空层跳过”而改变）；
     *   i18n   —— 层级名称文案 key；
     *   fallback —— i18n 未就绪时的中文兜底。
     */
    const ASSET_TREE_TYPE_META = {
        company: { level: 1, i18n: 'assets.tree.levelCompany', fallback: '项目' },
        domain: { level: 2, i18n: 'assets.tree.levelDomain', fallback: '域名' },
        subdomain: { level: 3, i18n: 'assets.tree.levelSubdomain', fallback: '子域名' },
        ip: { level: 4, i18n: 'assets.tree.levelIp', fallback: 'IP' },
        port: { level: 5, i18n: 'assets.tree.levelPort', fallback: '端口' },
        service: { level: 6, i18n: 'assets.tree.levelService', fallback: '服务' },
        fingerprint: { level: 7, i18n: 'assets.tree.levelFingerprint', fallback: '指纹' }
    };

    /** 默认展开的层级类型：只展开到「域名」层，避免一打开就撑爆列表 */
    const ASSET_TREE_DEFAULT_EXPAND_TYPES = ['company', 'domain'];

    const assetTreeState = {
        initialized: false,
        view: 'list',          // 'list' | 'tree'
        loaded: false,         // 是否已成功拉取过数据
        loading: false,
        error: '',             // 非空表示加载失败
        root: null,            // 归一化后的节点数组
        nodeIndex: new Map(),  // key -> 节点，便于 O(1) 查展开状态
        visibleRoot: null,     // 最近一次渲染使用的（搜索过滤后的）节点数组
        totalAssets: 0,
        projectId: '',         // 上次加载时使用的项目筛选值
        search: '',            // 已生效的搜索关键字（小写、已 trim）
        matchCount: 0,
        renderedExpanded: new Set(), // 最近一次渲染中实际展开的节点 key（搜索态判断用）
        expanded: new Set(),        // 已展开节点的 key
        searchOverrides: new Map(), // 搜索态下手动展开/折叠的覆盖（key -> boolean）
        requestSeq: 0,         // 请求序号，丢弃过期响应
        searchTimer: 0
    };

    /* ------------------------------------------------------------------ *
     * 基础工具
     * ------------------------------------------------------------------ */

    /** 统一的 i18n 取值：i18next 未就绪 / 缺 key 时回退到中文兜底 */
    function assetTreeT(key, fallback, options) {
        try {
            if (typeof window.t === 'function') {
                const translated = window.t(key, options);
                if (translated && translated !== key) return translated;
            }
        } catch (error) {
            /* i18n 异常不影响树渲染，走兜底 */
        }
        if (fallback == null) return key;
        if (!options || typeof fallback !== 'string') return fallback;
        return fallback.replace(/\{\{\s*([\w.]+)\s*\}\}/g, function (match, name) {
            return options[name] == null ? match : String(options[name]);
        });
    }

    /** HTML 文本转义（节点名称全部来自接口，必须转义） */
    function assetTreeEscape(value) {
        return String(value == null ? '' : value)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    /** HTML 属性转义（标签同文本转义即可） */
    function assetTreeEscapeAttr(value) {
        return assetTreeEscape(value).replace(/`/g, '&#96;');
    }

    /** 安全取字符串：null/undefined -> ''，其余 trim */
    function assetTreeText(value) {
        return value == null ? '' : String(value).trim();
    }

    /** 解析资产数量：非法值回退为 0 */
    function assetTreeNumber(value) {
        const parsed = Number(value);
        if (!Number.isFinite(parsed) || parsed < 0) return 0;
        return Math.floor(parsed);
    }

    /**
     * 高亮匹配文本：先按原文切片，再分别转义，避免先转义后匹配导致实体内误匹配。
     * 返回的是安全 HTML。
     */
    function assetTreeHighlight(text, keyword) {
        const raw = String(text == null ? '' : text);
        if (!keyword) return assetTreeEscape(raw);
        const lowerRaw = raw.toLowerCase();
        const needle = String(keyword).toLowerCase();
        if (!needle) return assetTreeEscape(raw);
        let from = 0;
        let cursor = lowerRaw.indexOf(needle, from);
        if (cursor === -1) return assetTreeEscape(raw);
        const parts = [];
        while (cursor !== -1) {
            parts.push(assetTreeEscape(raw.slice(from, cursor)));
            parts.push('<mark class="asset-tree-mark">' + assetTreeEscape(raw.slice(cursor, cursor + needle.length)) + '</mark>');
            from = cursor + needle.length;
            cursor = lowerRaw.indexOf(needle, from);
        }
        parts.push(assetTreeEscape(raw.slice(from)));
        return parts.join('');
    }

    /** 前序遍历节点 */
    function assetTreeWalk(nodes, visitor) {
        (nodes || []).forEach(function (node) {
            visitor(node);
            if (node.children && node.children.length) {
                assetTreeWalk(node.children, visitor);
            }
        });
    }

    /* ------------------------------------------------------------------ *
     * 数据归一化：把接口数据整理成带 key / searchText 的节点树
     * ------------------------------------------------------------------ */

    function assetTreeCreateNode(type, label, count, key) {
        const meta = ASSET_TREE_TYPE_META[type];
        return {
            key: key,
            type: type,
            level: meta ? meta.level : 1,
            label: label,
            count: assetTreeNumber(count),
            children: [],
            searchText: String(label == null ? '' : label).toLowerCase(),
            query: '',      // 端口节点：点击「查看资产」时用于回填列表搜索词
            assetIds: []
        };
    }

    function assetTreeCreateFingerprintNodes(technologies, keyPrefix, count) {
        const result = [];
        (Array.isArray(technologies) ? technologies : []).forEach(function (technology, index) {
            const name = assetTreeText(technology && technology.name);
            const version = assetTreeText(technology && technology.version);
            // 指纹节点显示「名称 版本」，版本为空则只显示名称
            const label = [name, version].filter(Boolean).join(' ');
            if (!label) return; // 名称与版本都为空：跳过，不渲染空节点
            result.push(assetTreeCreateNode('fingerprint', label, count, keyPrefix + '.f' + index));
        });
        return result;
    }

    /** 构建端口节点；端口与协议都缺失时返回 null（该层整体跳过） */
    function assetTreeBuildPortNode(port, key, queryTarget) {
        const safePort = port || {};
        const portNumber = safePort.port;
        const hasPortNumber = portNumber !== null && portNumber !== undefined && String(portNumber).trim() !== '';
        const protocol = assetTreeText(safePort.protocol);
        const service = assetTreeText(safePort.service);
        if (!hasPortNumber && !protocol) return null;

        // 端口节点显示「端口/协议」，例如 443/https；协议为空时只显示端口
        const label = hasPortNumber
            ? (protocol ? String(portNumber) + '/' + protocol : String(portNumber))
            : protocol;
        const node = assetTreeCreateNode('port', label, safePort.asset_count, key);
        node.query = assetTreeText(queryTarget);
        node.assetIds = Array.isArray(safePort.asset_ids) ? safePort.asset_ids.slice() : [];

        const fingerprintNodes = assetTreeCreateFingerprintNodes(safePort.technologies, key, node.count);
        if (service) {
            // 服务层：服务名紧跟端口节点之后；指纹作为服务节点的子层
            const serviceNode = assetTreeCreateNode('service', service, node.count, key + '.sv');
            serviceNode.children = fingerprintNodes;
            node.children = [serviceNode];
        } else {
            // 服务为空字符串：跳过服务层，指纹直接挂到端口下
            node.children = fingerprintNodes;
        }
        return node;
    }

    /** 构建 IP 层：IP 为空时跳过该层，直接把端口节点返回给上一层 */
    function assetTreeBuildIpNodes(ipRecord, keyPrefix, queryTarget) {
        const ipAddress = assetTreeText(ipRecord && ipRecord.ip);
        const portNodes = [];
        const ports = Array.isArray(ipRecord && ipRecord.ports) ? ipRecord.ports : [];
        ports.forEach(function (port, portIndex) {
            const portNode = assetTreeBuildPortNode(port, keyPrefix + '.p' + portIndex, ipAddress || queryTarget);
            if (portNode) portNodes.push(portNode);
        });
        if (!ipAddress) return portNodes; // IP 缺失：跳过该层
        const ipNode = assetTreeCreateNode('ip', ipAddress, ipRecord.asset_count, keyPrefix);
        ipNode.children = portNodes;
        return [ipNode];
    }

    /** 构建子域名层：host 为空时跳过该层，直接把 IP 节点返回给上一层 */
    function assetTreeBuildSubdomainNodes(subdomain, keyPrefix, queryTarget) {
        const host = assetTreeText(subdomain && subdomain.host);
        const ipNodes = [];
        const ips = Array.isArray(subdomain && subdomain.ips) ? subdomain.ips : [];
        ips.forEach(function (ipRecord, ipIndex) {
            const built = assetTreeBuildIpNodes(ipRecord, keyPrefix + '.i' + ipIndex, host || queryTarget);
            Array.prototype.push.apply(ipNodes, built);
        });
        if (!host) return ipNodes; // host 缺失：跳过子域名层
        const hostNode = assetTreeCreateNode('subdomain', host, subdomain.asset_count, keyPrefix);
        hostNode.children = ipNodes;
        return [hostNode];
    }

    /**
     * 把 /api/assets/tree 的响应归一化为内部节点树。
     * 空字符串的 domain / host / ip / service 均视为「该层缺失」，跳过而不是渲染空节点。
     */
    function assetTreeNormalize(payload) {
        const companies = Array.isArray(payload && payload.companies) ? payload.companies : [];
        const companyNodes = [];
        companies.forEach(function (company, companyIndex) {
            const safeCompany = company || {};
            const companyName = assetTreeText(safeCompany.company_name)
                || assetTreeT('assets.tree.unboundCompany', '未绑定项目');
            const companyNode = assetTreeCreateNode('company', companyName, safeCompany.asset_count, 'c' + companyIndex);

            const domains = Array.isArray(safeCompany.domains) ? safeCompany.domains : [];
            domains.forEach(function (domain, domainIndex) {
                const safeDomain = domain || {};
                const domainName = assetTreeText(safeDomain.domain);
                const keyPrefix = companyNode.key + '.d' + domainIndex;

                const childNodes = [];
                const subdomains = Array.isArray(safeDomain.subdomains) ? safeDomain.subdomains : [];
                subdomains.forEach(function (subdomain, subdomainIndex) {
                    const built = assetTreeBuildSubdomainNodes(
                        subdomain,
                        keyPrefix + '.s' + subdomainIndex,
                        domainName || companyName
                    );
                    Array.prototype.push.apply(childNodes, built);
                });

                if (!domainName) {
                    // 域名缺失：跳过域名层，子域名（或 IP）直接挂到项目下
                    Array.prototype.push.apply(companyNode.children, childNodes);
                    return;
                }
                const domainNode = assetTreeCreateNode('domain', domainName, safeDomain.asset_count, keyPrefix);
                domainNode.children = childNodes;
                companyNode.children.push(domainNode);
            });

            companyNodes.push(companyNode);
        });
        return companyNodes;
    }

    /** 重建 key -> 节点 索引 */
    function assetTreeBuildNodeIndex(nodes) {
        const index = new Map();
        assetTreeWalk(nodes, function (node) {
            index.set(node.key, node);
        });
        return index;
    }

    /** 收集默认需要展开的节点（项目层与域名层） */
    function assetTreeCollectDefaultExpanded(nodes, expanded) {
        (nodes || []).forEach(function (node) {
            if (ASSET_TREE_DEFAULT_EXPAND_TYPES.indexOf(node.type) !== -1 && node.children.length) {
                expanded.add(node.key);
            }
            assetTreeCollectDefaultExpanded(node.children, expanded);
        });
        return expanded;
    }

    /* ------------------------------------------------------------------ *
     * 搜索过滤：保留匹配节点 + 祖先链；节点自身命中时保留其完整子树
     * ------------------------------------------------------------------ */

    /**
     * 命中节点保留完整子树渲染，但仍继续向下统计 / 标注匹配，
     * 以便「匹配 N 个节点」计数准确、命中链也能自动展开。
     */
    function assetTreeAnnotateMatches(node, keyword, counter) {
        const selfMatch = node.searchText.indexOf(keyword) !== -1;
        if (selfMatch) counter.matchCount += 1;
        let hasMatchBelow = selfMatch;
        const children = (node.children || []).map(function (child) {
            const annotated = assetTreeAnnotateMatches(child, keyword, counter);
            if (annotated._selfMatch || annotated._hasMatchBelow) hasMatchBelow = true;
            return annotated;
        });
        return Object.assign({}, node, {
            children: children,
            _selfMatch: selfMatch,
            _hasMatchBelow: hasMatchBelow
        });
    }

    function assetTreeFilterNode(node, keyword, counter) {
        if (node.searchText.indexOf(keyword) !== -1) {
            // 命中节点：保留完整子树便于对照，同时继续统计子层匹配
            return assetTreeAnnotateMatches(node, keyword, counter);
        }
        const kept = [];
        let hasMatchBelow = false;
        (node.children || []).forEach(function (child) {
            const filteredChild = assetTreeFilterNode(child, keyword, counter);
            if (!filteredChild) return;
            kept.push(filteredChild);
            if (filteredChild._selfMatch || filteredChild._hasMatchBelow) hasMatchBelow = true;
        });
        if (!kept.length) return null;
        return Object.assign({}, node, {
            children: kept,
            _selfMatch: false,
            _hasMatchBelow: hasMatchBelow
        });
    }

    function assetTreeFilterNodes(nodes, keyword) {
        const counter = { matchCount: 0 };
        const result = [];
        (nodes || []).forEach(function (node) {
            const filtered = assetTreeFilterNode(node, keyword, counter);
            if (filtered) result.push(filtered);
        });
        return { nodes: result, matchCount: counter.matchCount };
    }

    /* ------------------------------------------------------------------ *
     * 展开状态
     * ------------------------------------------------------------------ */

    /** 判断某节点在当前状态下是否展开（搜索态会按命中链自动展开，除非用户手动覆盖） */
    function assetTreeNodeIsExpanded(node, searchMode) {
        if (!node.children || !node.children.length) return false;
        if (searchMode) {
            const override = assetTreeState.searchOverrides.get(node.key);
            if (override !== undefined) return override;
            if (node._selfMatch || node._hasMatchBelow) return true;
        }
        return assetTreeState.expanded.has(node.key);
    }

    function assetTreeVisibleNodes() {
        if (!assetTreeState.root) return [];
        if (!assetTreeState.search) return assetTreeState.root;
        if (assetTreeState.visibleRoot) return assetTreeState.visibleRoot;
        return assetTreeFilterNodes(assetTreeState.root, assetTreeState.search).nodes;
    }

    /** 展开/折叠单个节点 */
    function assetTreeToggle(key) {
        if (!key) return;
        const searchMode = !!assetTreeState.search;
        if (searchMode) {
            // 搜索态下按“当前实际渲染状态”取反并写入覆盖表，
            // 否则自动展开的命中链会被误判为已折叠。
            assetTreeState.searchOverrides.set(key, !assetTreeState.renderedExpanded.has(key));
        } else if (assetTreeState.expanded.has(key)) {
            assetTreeState.expanded.delete(key);
        } else {
            assetTreeState.expanded.add(key);
        }
        assetTreeRender();
    }

    /** 展开全部：展开当前可见树中所有有子节点的节点 */
    function assetTreeExpandAll() {
        if (!assetTreeState.root) return;
        assetTreeState.searchOverrides.clear();
        assetTreeWalk(assetTreeVisibleNodes(), function (node) {
            if (node.children && node.children.length) {
                assetTreeState.expanded.add(node.key);
            }
        });
        assetTreeRender();
    }

    /** 折叠全部：清空展开集合；搜索态下额外写入 false 覆盖，让命中链也收起 */
    function assetTreeCollapseAll() {
        assetTreeState.expanded.clear();
        assetTreeState.searchOverrides.clear();
        if (assetTreeState.search) {
            assetTreeWalk(assetTreeVisibleNodes(), function (node) {
                if (node.children && node.children.length) {
                    assetTreeState.searchOverrides.set(node.key, false);
                }
            });
        }
        assetTreeRender();
    }

    /* ------------------------------------------------------------------ *
     * 渲染
     * ------------------------------------------------------------------ */

    function assetTreeKindLabel(type) {
        const meta = ASSET_TREE_TYPE_META[type];
        if (!meta) return type;
        return assetTreeT(meta.i18n, meta.fallback);
    }

    function assetTreePlaceholderHtml(text, extraHtml) {
        return '<div class="asset-tree-placeholder">'
            + '<span class="asset-tree-placeholder-text">' + assetTreeEscape(text) + '</span>'
            + (extraHtml || '')
            + '</div>';
    }

    function assetTreeToggleHtml(hasChildren, isExpanded) {
        if (!hasChildren) {
            return '<span class="asset-tree-toggle asset-tree-toggle--leaf" aria-hidden="true"></span>';
        }
        return '<button class="asset-tree-toggle" type="button" data-asset-tree-action="toggle"'
            + ' aria-label="' + assetTreeEscapeAttr(assetTreeT('assets.tree.toggleNode', '展开或折叠')) + '"'
            + ' aria-expanded="' + (isExpanded ? 'true' : 'false') + '">'
            + '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"'
            + ' stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6"/></svg>'
            + '</button>';
    }

    /** 单行节点 HTML */
    function assetTreeRowHtml(node, depth, hasChildren, isExpanded, keyword) {
        const classes = ['asset-tree-row', 'asset-tree-row--' + node.type];
        if (isExpanded) classes.push('is-expanded');
        const count = assetTreeNumber(node.count);
        const countTitle = assetTreeT('assets.tree.assetCount', '{{count}} 个资产', { count: count });

        let html = '<div class="' + classes.join(' ') + '"'
            + ' style="--asset-tree-depth:' + Number(depth) + '"'
            + ' data-asset-tree-key="' + assetTreeEscapeAttr(node.key) + '"'
            + ' data-asset-tree-level="' + node.level + '"'
            + (hasChildren ? ' data-asset-tree-toggle="1"' : '')
            + ' role="treeitem" aria-level="' + node.level + '"'
            + (hasChildren ? ' aria-expanded="' + (isExpanded ? 'true' : 'false') + '"' : '')
            + '>';
        html += assetTreeToggleHtml(hasChildren, isExpanded);
        html += '<span class="asset-tree-dot" aria-hidden="true"></span>';
        html += '<span class="asset-tree-kind">' + assetTreeEscape(assetTreeKindLabel(node.type)) + '</span>';
        html += '<span class="asset-tree-label" title="' + assetTreeEscapeAttr(countTitle) + '">'
            + assetTreeHighlight(node.label, keyword) + '</span>';
        html += '<span class="asset-tree-count">' + count + '</span>';
        if (node.type === 'port') {
            // 端口节点提供「查看资产」入口；点击行本身仍然是展开/折叠
            html += '<button class="asset-tree-row-action" type="button"'
                + ' data-asset-tree-action="view-assets"'
                + ' title="' + assetTreeEscapeAttr(assetTreeT('assets.tree.viewAssets', '查看资产')) + '">'
                + assetTreeEscape(assetTreeT('assets.tree.viewAssets', '查看资产')) + '</button>';
        }
        html += '</div>';
        return html;
    }

    /** 递归展开成扁平行（扁平 DOM + 缩进，长列表下比深层嵌套更轻） */
    function assetTreeRenderRows(nodes, depth, parts, searchMode) {
        (nodes || []).forEach(function (node) {
            const hasChildren = !!(node.children && node.children.length);
            const isExpanded = assetTreeNodeIsExpanded(node, searchMode);
            if (isExpanded) assetTreeState.renderedExpanded.add(node.key);
            parts.push(assetTreeRowHtml(node, depth, hasChildren, isExpanded, assetTreeState.search));
            if (isExpanded) {
                assetTreeRenderRows(node.children, depth + 1, parts, searchMode);
            }
        });
    }

    function assetTreeUpdateMeta(text) {
        const metaEl = document.getElementById('asset-tree-meta');
        if (metaEl) metaEl.textContent = text || '';
    }

    function assetTreeSyncClearButton() {
        const input = document.getElementById('asset-tree-search');
        const clearBtn = document.getElementById('asset-tree-search-clear');
        if (clearBtn) clearBtn.hidden = !(input && String(input.value || '').trim());
    }

    /** 主渲染入口：按 加载中 / 失败 / 空 / 无匹配 / 正常 五态渲染 */
    function assetTreeRender() {
        const rootEl = document.getElementById('asset-tree-root');
        if (!rootEl) return;
        assetTreeSyncClearButton();

        if (assetTreeState.loading) {
            rootEl.setAttribute('aria-busy', 'true');
            rootEl.innerHTML = '<div class="asset-tree-placeholder">'
                + '<span class="asset-tree-spinner" aria-hidden="true"></span>'
                + '<span class="asset-tree-placeholder-text">'
                + assetTreeEscape(assetTreeT('common.loading', '加载中...'))
                + '</span></div>';
            assetTreeUpdateMeta('');
            return;
        }
        rootEl.removeAttribute('aria-busy');

        if (assetTreeState.error) {
            rootEl.innerHTML = assetTreePlaceholderHtml(
                assetTreeState.error,
                '<button class="btn-secondary" type="button" data-asset-tree-action="retry">'
                + assetTreeEscape(assetTreeT('assets.tree.retry', '重试')) + '</button>'
            );
            assetTreeUpdateMeta('');
            return;
        }

        const searchMode = !!assetTreeState.search;
        let visibleRoot = assetTreeState.root || [];
        assetTreeState.matchCount = 0;
        if (searchMode) {
            const filtered = assetTreeFilterNodes(visibleRoot, assetTreeState.search);
            visibleRoot = filtered.nodes;
            assetTreeState.matchCount = filtered.matchCount;
        }
        assetTreeState.visibleRoot = visibleRoot;

        if (!visibleRoot.length) {
            const emptyText = searchMode
                ? assetTreeT('assets.tree.noSearchResult', '没有匹配的节点')
                : assetTreeT('assets.tree.empty', '暂无资产');
            rootEl.innerHTML = assetTreePlaceholderHtml(emptyText);
            assetTreeUpdateMeta(searchMode && assetTreeState.search
                ? assetTreeT('assets.tree.matchCount', '匹配 {{count}} 个节点', { count: 0 })
                : '');
            return;
        }

        const parts = [];
        assetTreeState.renderedExpanded = new Set();
        assetTreeRenderRows(visibleRoot, 0, parts, searchMode);

        // template.content 是 DocumentFragment，一次性替换，避免逐行 append
        const template = document.createElement('template');
        template.innerHTML = parts.join('');
        rootEl.innerHTML = '';
        rootEl.appendChild(template.content);

        if (searchMode) {
            assetTreeUpdateMeta(assetTreeT('assets.tree.matchCount', '匹配 {{count}} 个节点', {
                count: assetTreeState.matchCount
            }));
        } else {
            assetTreeUpdateMeta(assetTreeT('assets.tree.total', '共 {{count}} 个资产', {
                count: assetTreeState.totalAssets
            }));
        }
    }

    /* ------------------------------------------------------------------ *
     * 数据加载
     * ------------------------------------------------------------------ */

    function assetTreeCurrentProjectId() {
        const projectFilter = document.getElementById('asset-project-filter');
        return projectFilter ? String(projectFilter.value || '').trim() : '';
    }

    /**
     * 拉取并渲染层级数据。
     * @param {boolean} force 强制重新请求（刷新按钮 / 项目筛选变化时使用）
     */
    async function loadAssetTree(force) {
        const rootEl = document.getElementById('asset-tree-root');
        if (!rootEl) return;

        const projectId = assetTreeCurrentProjectId();
        if (assetTreeState.loaded && !force && assetTreeState.projectId === projectId) {
            assetTreeRender();
            return;
        }

        const seq = ++assetTreeState.requestSeq;
        const isProjectChange = assetTreeState.loaded && assetTreeState.projectId !== projectId;
        assetTreeState.loading = true;
        assetTreeState.error = '';
        assetTreeRender(); // 先渲染 loading

        try {
            const url = ASSET_TREE_API + (projectId ? '?project_id=' + encodeURIComponent(projectId) : '');
            // 统一走全局 apiFetch（自动带 Authorization、401 弹登录框）
            const response = typeof apiFetch === 'function' ? await apiFetch(url) : await fetch(url);
            if (seq !== assetTreeState.requestSeq) return; // 已被更新的请求取代
            if (!response || !response.ok) {
                throw new Error('HTTP ' + (response ? response.status : 'error'));
            }
            const payload = await response.json();
            if (seq !== assetTreeState.requestSeq) return;

            const rootNodes = assetTreeNormalize(payload);
            const nodeIndex = assetTreeBuildNodeIndex(rootNodes);

            // 首次加载 / 切换项目：重置为默认展开层级；普通刷新保留用户现有展开状态
            if (!assetTreeState.loaded || isProjectChange) {
                assetTreeState.expanded = assetTreeCollectDefaultExpanded(rootNodes, new Set());
            } else {
                // 丢弃已不存在的节点 key，避免状态集合膨胀
                const nextExpanded = new Set();
                assetTreeState.expanded.forEach(function (key) {
                    if (nodeIndex.has(key)) nextExpanded.add(key);
                });
                assetTreeState.expanded = nextExpanded;
            }

            const fallbackTotal = rootNodes.reduce(function (sum, node) {
                return sum + assetTreeNumber(node.count);
            }, 0);

            assetTreeState.root = rootNodes;
            assetTreeState.nodeIndex = nodeIndex;
            assetTreeState.totalAssets = assetTreeNumber(payload && payload.total_assets) || fallbackTotal;
            assetTreeState.projectId = projectId;
            assetTreeState.loaded = true;
            assetTreeState.loading = false;
            assetTreeState.error = '';
            assetTreeState.searchOverrides.clear();
            assetTreeRender();
        } catch (error) {
            if (seq !== assetTreeState.requestSeq) return;
            console.error('加载资产层级树失败:', error);
            assetTreeState.loading = false;
            assetTreeState.loaded = false;
            const reason = error && error.message ? '（' + error.message + '）' : '';
            assetTreeState.error = assetTreeT('assets.tree.loadFailed', '层级视图加载失败') + reason;
            assetTreeRender();
        }
    }

    /* ------------------------------------------------------------------ *
     * 视图切换 / 搜索 / 事件
     * ------------------------------------------------------------------ */

    /** 切换「列表 / 层级」视图，默认列表且不回写任何既有列表状态 */
    function assetTreeSetView(view) {
        const nextView = view === 'tree' ? 'tree' : 'list';
        assetTreeState.view = nextView;

        const page = document.getElementById('page-asset-library');
        const container = document.getElementById('asset-tree-view');
        const listBtn = document.getElementById('asset-view-tab-list');
        const treeBtn = document.getElementById('asset-view-tab-tree');

        if (page) page.classList.toggle('asset-library--tree-view', nextView === 'tree');
        if (container) container.hidden = nextView !== 'tree';
        if (listBtn) {
            listBtn.classList.toggle('is-active', nextView === 'list');
            listBtn.setAttribute('aria-selected', nextView === 'list' ? 'true' : 'false');
        }
        if (treeBtn) {
            treeBtn.classList.toggle('is-active', nextView === 'tree');
            treeBtn.setAttribute('aria-selected', nextView === 'tree' ? 'true' : 'false');
        }

        if (nextView !== 'tree') return;

        // 进入层级视图：首次加载或项目筛选变化时拉数据，否则直接复用缓存
        const stale = !assetTreeState.loaded || assetTreeState.projectId !== assetTreeCurrentProjectId();
        if (stale && !assetTreeState.loading) {
            void loadAssetTree(true);
        } else {
            assetTreeRender();
        }
    }

    function assetTreeApplySearch() {
        const input = document.getElementById('asset-tree-search');
        const keyword = input ? String(input.value || '').trim().toLowerCase() : '';
        assetTreeSyncClearButton();
        if (keyword === assetTreeState.search) return;
        assetTreeState.search = keyword;
        assetTreeState.searchOverrides.clear();
        assetTreeRender();
    }

    function assetTreeClearSearch() {
        const input = document.getElementById('asset-tree-search');
        if (input) input.value = '';
        assetTreeState.search = '';
        assetTreeState.searchOverrides.clear();
        assetTreeSyncClearButton();
        assetTreeRender();
        if (input) input.focus();
    }

    /**
     * 端口行「查看资产」：切回列表视图，用该端口所在 IP（缺失时退回主机/域名）作为搜索词。
     * 说明：现有列表详情接口按“列表内序号”打开详情，无法按 asset_id 直接打开，
     * 因此这里不硬编码不存在的函数，退化为一次针对该目标的列表搜索。
     */
    function assetTreeViewPortAssets(key) {
        const node = key ? assetTreeState.nodeIndex.get(key) : null;
        if (!node) return;
        const target = assetTreeText(node.query);
        assetTreeSetView('list');
        const searchInput = document.getElementById('asset-search');
        if (!searchInput || !target) return;
        searchInput.value = target;
        if (typeof window.loadAssets === 'function') {
            window.loadAssets(1);
        }
    }

    function assetTreeHandleRootClick(event) {
        const target = event.target;
        if (!target || typeof target.closest !== 'function') return;

        const actionEl = target.closest('[data-asset-tree-action]');
        if (actionEl) {
            const action = actionEl.getAttribute('data-asset-tree-action');
            if (action === 'retry') {
                event.preventDefault();
                void loadAssetTree(true);
                return;
            }
            if (action === 'view-assets') {
                event.preventDefault();
                event.stopPropagation();
                const actionRow = actionEl.closest('[data-asset-tree-key]');
                if (actionRow) assetTreeViewPortAssets(actionRow.getAttribute('data-asset-tree-key'));
                return;
            }
            if (action === 'toggle') {
                event.preventDefault();
                event.stopPropagation();
                const toggleRow = actionEl.closest('[data-asset-tree-key]');
                if (toggleRow) assetTreeToggle(toggleRow.getAttribute('data-asset-tree-key'));
                return;
            }
            return;
        }

        // 点击行本身：有子节点则展开/折叠
        const row = target.closest('.asset-tree-row');
        if (row && row.hasAttribute('data-asset-tree-toggle')) {
            event.preventDefault();
            assetTreeToggle(row.getAttribute('data-asset-tree-key'));
        }
    }

    /** 初始化：绑定事件（幂等，可重复调用） */
    function initAssetTree() {
        if (assetTreeState.initialized) return;
        const container = document.getElementById('asset-tree-view');
        if (!container) return;
        assetTreeState.initialized = true;

        const rootEl = document.getElementById('asset-tree-root');
        if (rootEl) rootEl.addEventListener('click', assetTreeHandleRootClick);

        const searchInput = document.getElementById('asset-tree-search');
        if (searchInput) {
            searchInput.addEventListener('input', function () {
                if (assetTreeState.searchTimer) clearTimeout(assetTreeState.searchTimer);
                assetTreeState.searchTimer = setTimeout(function () {
                    assetTreeState.searchTimer = 0;
                    assetTreeApplySearch();
                }, ASSET_TREE_SEARCH_DEBOUNCE);
            });
            searchInput.addEventListener('keydown', function (event) {
                if (event.key === 'Escape') {
                    event.preventDefault();
                    assetTreeClearSearch();
                } else if (event.key === 'Enter') {
                    event.preventDefault();
                    if (assetTreeState.searchTimer) {
                        clearTimeout(assetTreeState.searchTimer);
                        assetTreeState.searchTimer = 0;
                    }
                    assetTreeApplySearch();
                }
            });
            // 浏览器原生搜索框的清空按钮触发 search 事件
            searchInput.addEventListener('search', function () {
                if (assetTreeState.searchTimer) {
                    clearTimeout(assetTreeState.searchTimer);
                    assetTreeState.searchTimer = 0;
                }
                assetTreeApplySearch();
            });
        }

        // 语言切换后重绘动态节点（静态 data-i18n 文案由 i18n.js 统一刷新）
        document.addEventListener('languagechange', function () {
            if (assetTreeState.loaded) assetTreeRender();
        });

        // 若树先于语言包加载完成渲染（极端情况），等 i18n 就绪后重绘一次，避免残留中文兜底
        if (window.i18nReady && typeof window.i18nReady.then === 'function') {
            window.i18nReady.then(function () {
                if (assetTreeState.loaded) assetTreeRender();
            }).catch(function () {
                /* 忽略语言包加载异常，保留兜底文案 */
            });
        }

        // 从其它页面回到资产库且仍停留在层级视图时刷新一次数据
        window.addEventListener('hashchange', function () {
            const pageId = String(window.location.hash || '').replace(/^#/, '').split('?')[0];
            if (pageId !== 'asset-library' || assetTreeState.view !== 'tree') return;
            if (assetTreeState.loaded && !assetTreeState.loading) {
                void loadAssetTree(true);
            }
        });

        // 默认列表视图（与 HTML 初始状态一致，这里只是同步一次按钮状态）
        assetTreeSetView('list');
    }

    /* ------------------------------------------------------------------ *
     * 对外暴露（保持 minimal API，均带 assetTree 前缀或语义化名称）
     * ------------------------------------------------------------------ */
    window.initAssetTree = initAssetTree;
    window.loadAssetTree = loadAssetTree;
    window.assetTreeSetView = assetTreeSetView;
    window.assetTreeExpandAll = assetTreeExpandAll;
    window.assetTreeCollapseAll = assetTreeCollapseAll;
    window.assetTreeClearSearch = assetTreeClearSearch;

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initAssetTree);
    } else {
        initAssetTree();
    }
})();
