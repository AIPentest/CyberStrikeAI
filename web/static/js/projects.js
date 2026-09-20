/**
 * 项目管理与事实黑板
 */
let projectsCache = [];
let projectsCacheAll = [];
const PROJECTS_LIST_PAGE_SIZE_KEY = 'cyberstrike.projects_list_page_size';
let currentProjectId = null;
let currentProjectUpdatedAt = null;
let currentProjectTab = 'facts';
let currentProjectAssets = [];
const PROJECT_ASSETS_PAGE_SIZE_KEY = 'cyberstrike.project_assets_page_size';
let projectAssetsPagination = {
    page: 1,
    pageSize: (() => {
        try {
            const size = Number(localStorage.getItem(PROJECT_ASSETS_PAGE_SIZE_KEY));
            return [10, 20, 50, 100].includes(size) ? size : 20;
        } catch (e) {
            return 20;
        }
    })(),
    total: 0,
    totalPages: 1,
};
const projectNameById = {};
let _projectsListReady = false;
let _projectsFetchPromise = null;

const PROJECT_ACTIVE_KEY = 'cyberstrike.activeProjectId';
const PROJECT_DESCRIPTION_MAX_LENGTH = 4000;
const PROJECT_NAME_MAX_LENGTH = 200;

function tp(key, opts) {
    if (typeof window.t === 'function') return window.t(key, opts);
    return key;
}

function tpFmt(key, fallback, opts) {
    const text = tp(key, opts);
    if (!text || text === key) return fallback;
    return text;
}

function requireProjectWrite() {
    if (typeof requirePermission !== 'function') return true;
    return requirePermission(
        'project:write',
        tpFmt('projects.writePermissionDenied', '当前账号仅有只读权限，无法创建或修改项目'),
    );
}

function requireProjectDelete() {
    if (typeof requirePermission !== 'function') return true;
    return requirePermission(
        'project:delete',
        tpFmt('projects.writePermissionDenied', '当前账号仅有只读权限，无法删除项目'),
    );
}

async function notifyProjectApiFailure(response, fallbackKey, fallbackText) {
    if (typeof ensureApiOk === 'function') {
        return ensureApiOk(response, tpFmt(fallbackKey, fallbackText));
    }
    if (!response || response.ok) return true;
    alert(tpFmt(fallbackKey, fallbackText));
    return false;
}

const PROJECTS_FILTER_SELECT_HANDLERS = {
    'project-facts-filter-category': function () { loadProjectFacts(); },
    'project-facts-filter-confidence': function () { loadProjectFacts(); },
    'project-graph-view': function () { loadProjectFactGraph(); },
    'project-vulns-filter-severity': function () { loadProjectVulnerabilities(); },
    'project-vulns-filter-status': function () { loadProjectVulnerabilities(); },
    'projects-page-size-pagination': function () { changeProjectsPageSize(); }
};
const projectsFilterSelectMap = {};
let projectsFilterSelectDocBound = false;
const PROJECTS_FILTER_SELECT_CARET = '<svg class="projects-filter-select-caret" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';

function closeAllProjectsFilterSelects() {
    Object.keys(projectsFilterSelectMap).forEach(function (id) {
        const reg = projectsFilterSelectMap[id];
        if (!reg || !reg.wrapper) return;
        reg.wrapper.classList.remove('open');
        if (reg.trigger) reg.trigger.setAttribute('aria-expanded', 'false');
    });
}

function pruneProjectsFilterSelectMap(root) {
    Object.keys(projectsFilterSelectMap).forEach(function (id) {
        const select = document.getElementById(id);
        if (!select || (root && !root.contains(select))) {
            delete projectsFilterSelectMap[id];
        }
    });
}

function syncProjectsFilterSelect(select) {
    const reg = projectsFilterSelectMap[select.id];
    if (!reg) return;
    const dropdown = reg.dropdown;
    const trigger = reg.trigger;
    const valueSpan = trigger.querySelector('.projects-filter-select-value');

    dropdown.innerHTML = '';
    Array.prototype.forEach.call(select.options, function (opt) {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'projects-filter-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-value', opt.value);
        if (opt.value === select.value) {
            item.classList.add('is-selected');
            item.setAttribute('aria-selected', 'true');
        } else {
            item.setAttribute('aria-selected', 'false');
        }
        const check = document.createElement('span');
        check.className = 'projects-filter-select-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';
        const label = document.createElement('span');
        label.className = 'projects-filter-select-label';
        label.textContent = opt.textContent;
        item.appendChild(check);
        item.appendChild(label);
        dropdown.appendChild(item);
    });

    const selectedOpt = select.options[select.selectedIndex];
    if (valueSpan) {
        valueSpan.textContent = selectedOpt ? selectedOpt.textContent : '';
    }
    trigger.disabled = !!select.disabled;
    reg.wrapper.classList.toggle('is-disabled', !!select.disabled);
}

function syncAllProjectsFilterSelects() {
    Object.keys(projectsFilterSelectMap).forEach(function (id) {
        const select = document.getElementById(id);
        if (select) syncProjectsFilterSelect(select);
    });
}

function enhanceProjectsFilterSelect(select) {
    if (!select || !select.id) return;
    const existing = projectsFilterSelectMap[select.id];
    if (existing && existing.select !== select) {
        delete projectsFilterSelectMap[select.id];
    }
    if (select.dataset.projectsCustomSelect === '1') {
        syncProjectsFilterSelect(select);
        return;
    }
    select.dataset.projectsCustomSelect = '1';
    select.classList.add('projects-filter-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'projects-filter-select-ui';
    if (select.id === 'projects-page-size-pagination') {
        wrapper.classList.add('projects-filter-select-ui--compact');
    }

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'projects-filter-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    const valueSpan = document.createElement('span');
    valueSpan.className = 'projects-filter-select-value';
    trigger.appendChild(valueSpan);
    trigger.insertAdjacentHTML('beforeend', PROJECTS_FILTER_SELECT_CARET);

    const dropdown = document.createElement('div');
    dropdown.className = 'projects-filter-select-dropdown';
    dropdown.setAttribute('role', 'listbox');

    const parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    projectsFilterSelectMap[select.id] = { wrapper: wrapper, trigger: trigger, dropdown: dropdown, select: select };

    trigger.addEventListener('click', function (e) {
        e.stopPropagation();
        if (select.disabled) return;
        const open = wrapper.classList.contains('open');
        closeAllProjectsFilterSelects();
        if (!open) {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
        }
    });

    dropdown.addEventListener('click', function (e) {
        const opt = e.target.closest('.projects-filter-select-option');
        if (!opt) return;
        e.stopPropagation();
        const val = opt.getAttribute('data-value');
        if (val === null) return;
        if (select.value !== val) {
            select.value = val;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        wrapper.classList.remove('open');
        trigger.setAttribute('aria-expanded', 'false');
        syncProjectsFilterSelect(select);
    });

    select.addEventListener('change', function () {
        syncProjectsFilterSelect(select);
    });

    if (!select.dataset.projectsFilterBound) {
        select.dataset.projectsFilterBound = '1';
        const handler = PROJECTS_FILTER_SELECT_HANDLERS[select.id];
        if (typeof handler === 'function') {
            select.addEventListener('change', handler);
        }
    }

    syncProjectsFilterSelect(select);
}

function refreshProjectsFilterSelects() {
    const page = document.getElementById('page-projects');
    if (!page) return;
    pruneProjectsFilterSelectMap(page);
    page.querySelectorAll('select.projects-filter-select-native, #projects-page-size-pagination').forEach(function (select) {
        enhanceProjectsFilterSelect(select);
    });
    if (!projectsFilterSelectDocBound) {
        projectsFilterSelectDocBound = true;
        document.addEventListener('click', closeAllProjectsFilterSelects);
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') closeAllProjectsFilterSelects();
        });
    }
}

/** 与后端 internal/project/fact_template.go 对齐 */
const FACT_ATTACK_CHAIN_BODY_TEMPLATE = `## 结论（可验证，一句话）
<勿仅写「存在漏洞」；写明类型 + 位置 + 触发条件>

## 目标与入口
- 目标: <URL / IP:Port / 主机名>
- 入口: <路径 / 接口 / 参数>
- 前置条件: <匿名 / 角色 / Cookie / 其他依赖>

## 攻击链（逐步可复现）
1. <侦察/发现>
2. <利用/触发>
3. <影响证明（读文件、RCE 回显、越权数据等）>

## Exploit / POC
### 请求
\`\`\`http
<METHOD> <path> HTTP/1.1
Host: ...
...

<body>
\`\`\`

### 响应 / 现象
<关键响应片段、状态码、差异点>

### 命令 / 脚本（如有）
\`\`\`bash
<command>
\`\`\`

## 关键证据
- <工具输出摘要 / 截图路径 / 会话或消息 ID>

## 关联
- related_vulnerability_id: <可选>
- 依赖事实: <fact_key，如 auth/session_cookie>
  - 结构化关系边（自动同步；links 文本格式 type: source_fact_key）:
  - discovered_on: target/primary_domain

## 备注与不确定性
<待验证假设、环境差异、绕过尝试记录>`;

const FACT_ENV_BODY_TEMPLATE = `## 摘要
<该事实的核心认知>

## 细节
<端口/版本/路径/凭据特征/业务规则等>

## 来源与证据
<命令输出、响应片段、发现时间>

## 关联
- 相关 fact_key: <可选>`;

const FACT_ATTACK_CHAIN_PREFIXES = ['finding/', 'chain/', 'exploit/', 'poc/'];
const FACT_ATTACK_CHAIN_CATEGORIES = new Set(['finding', 'chain', 'exploit', 'poc', 'vuln']);

function requiresAttackChainFact(category, factKey) {
    const c = (category || '').trim().toLowerCase();
    if (FACT_ATTACK_CHAIN_CATEGORIES.has(c)) return true;
    const key = (factKey || '').trim().toLowerCase();
    return FACT_ATTACK_CHAIN_PREFIXES.some((p) => key.startsWith(p));
}

function isSparseFactBody(category, factKey, body) {
    if (!requiresAttackChainFact(category, factKey)) return false;
    const text = (body || '').trim();
    if (!text) return true;
    const lower = text.toLowerCase();
    const hasSteps =
        lower.includes('攻击链') ||
        lower.includes('## 攻击') ||
        lower.includes('## exploit') ||
        lower.includes('## poc');
    const hasHTTP =
        lower.includes('```http') ||
        lower.includes('```bash') ||
        lower.includes('curl ') ||
        lower.includes('get ') ||
        lower.includes('post ');
    const hasReq = lower.includes('请求') || lower.includes('响应') || lower.includes('payload');
    return !(hasSteps || hasHTTP || hasReq);
}

function formatFactBodyBadge(f) {
    if (!requiresAttackChainFact(f.category, f.fact_key)) {
        const hasBody = !!(f.body || '').trim();
        return `<span class="projects-fact-badge projects-fact-badge--na" title="${escapeHtml(tp('projects.factBodyEnvTitle'))}">${hasBody ? escapeHtml(tp('projects.factBodyHasDetail')) : '—'}</span>`;
    }
    if (isSparseFactBody(f.category, f.fact_key, f.body)) {
        return `<span class="projects-fact-badge projects-fact-badge--warn" title="${escapeHtml(tp('projects.factBodySparseTitle'))}">${escapeHtml(tp('projects.factBodySparse'))}</span>`;
    }
    return `<span class="projects-fact-badge projects-fact-badge--ok" title="${escapeHtml(tp('projects.factBodyReproducibleTitle'))}">${escapeHtml(tp('projects.factBodyReproducible'))}</span>`;
}

function updateFactFormHints() {
    const cat = document.getElementById('fact-modal-category')?.value || '';
    const key = document.getElementById('fact-modal-key')?.value || '';
    const body = document.getElementById('fact-modal-body')?.value || '';
    const hint = document.getElementById('fact-modal-body-hint');
    if (!hint) return;
    if (requiresAttackChainFact(cat, key)) {
        const sparse = isSparseFactBody(cat, key, body);
        hint.textContent = sparse
            ? tp('projects.factHintAttackSparse')
            : tp('projects.factHintAttackReady');
        hint.classList.toggle('projects-field-hint--warn', sparse);
    } else {
        hint.textContent = tp('projects.factHintEnv');
        hint.classList.remove('projects-field-hint--warn');
    }
}

function insertFactBodyTemplate(kind) {
    const ta = document.getElementById('fact-modal-body');
    if (!ta) return;
    const tpl = kind === 'env' ? FACT_ENV_BODY_TEMPLATE : FACT_ATTACK_CHAIN_BODY_TEMPLATE;
    if (ta.value.trim() && !confirm(tp('projects.confirmOverwriteBodyTemplate'))) return;
    ta.value = tpl;
    updateFactFormHints();
    ta.focus();
}

function getActiveProjectId() {
    try {
        return localStorage.getItem(PROJECT_ACTIVE_KEY) || '';
    } catch (e) {
        return '';
    }
}

function setActiveProjectId(id) {
    try {
        if (id) localStorage.setItem(PROJECT_ACTIVE_KEY, id);
        else localStorage.removeItem(PROJECT_ACTIVE_KEY);
    } catch (e) { /* ignore */ }
}

function rebuildProjectNameMap(list) {
    Object.keys(projectNameById).forEach((k) => delete projectNameById[k]);
    (list || []).forEach((p) => {
        if (p && p.id) projectNameById[p.id] = p.name || p.id;
    });
}

function rememberProjectsInNameMap(list) {
    (list || []).forEach((p) => {
        if (p && p.id) projectNameById[p.id] = p.name || p.id;
    });
}

/** 与后端 projectListSearchPattern 对齐：name / description / id 子串匹配（忽略大小写） */
function matchProjectSearchQuery(project, query) {
    const q = String(query || '').trim().toLowerCase();
    if (!q) return true;
    const name = String(project.name || '').toLowerCase();
    const desc = String(project.description || '').toLowerCase();
    const id = String(project.id || '').toLowerCase();
    return name.includes(q) || desc.includes(q) || id.includes(q);
}

function sortProjectsForPicker(projects) {
    return [...projects].sort((a, b) => {
        const ap = a.pinned ? 1 : 0;
        const bp = b.pinned ? 1 : 0;
        if (bp !== ap) return bp - ap;
        const au = a.updated_at || a.updatedAt || '';
        const bu = b.updated_at || b.updatedAt || '';
        return String(bu).localeCompare(String(au));
    });
}

/** 从已加载列表中筛选活跃项目（对话选择器 / 项目筛选下拉） */
function filterActiveProjectsLocal(projects, query) {
    const list = (projects || []).filter((p) => p && p.id && p.status !== 'archived');
    const q = String(query || '').trim();
    const filtered = q ? list.filter((p) => matchProjectSearchQuery(p, q)) : list;
    return sortProjectsForPicker(filtered);
}

async function searchActiveProjects(query, opts = {}) {
    const params = new URLSearchParams();
    params.set('status', opts.status || 'active');
    params.set('limit', String(opts.limit ?? (String(query || '').trim() ? PROJECT_PICKER_SEARCH_LIMIT : PROJECT_PICKER_INITIAL_LIMIT)));
    params.set('offset', String(opts.offset ?? 0));
    const q = String(query || '').trim();
    if (q) params.set('search', q);
    const res = await apiFetch(`/api/projects?${params}`);
    if (!res.ok) throw new Error(tp('projects.loadProjectsFailed'));
    const parsed = parseProjectsListResponse(await res.json());
    rememberProjectsInNameMap(parsed.items);
    return parsed;
}

async function fetchProjectSummary(projectId) {
    const id = String(projectId || '').trim();
    if (!id) return null;
    const res = await apiFetch(`/api/projects/${encodeURIComponent(id)}`);
    if (!res.ok) return null;
    const project = await res.json();
    if (project && project.id) rememberProjectsInNameMap([project]);
    return project;
}

function getProjectsListPageSize() {
    try {
        const saved = parseInt(localStorage.getItem(PROJECTS_LIST_PAGE_SIZE_KEY), 10);
        if ([20, 50, 100].includes(saved)) return saved;
    } catch (e) { /* ignore */ }
    return 50;
}

let projectsListPagination = { page: 1, pageSize: getProjectsListPageSize(), total: 0 };
let projectsListSearch = '';
let _projectsListSearchDebounce = null;

function parseListTotalValue(raw, itemsLength) {
    if (typeof raw === 'number' && Number.isFinite(raw) && raw >= 0) return raw;
    if (raw != null && raw !== '') {
        const n = parseInt(String(raw), 10);
        if (Number.isFinite(n) && n >= 0) return n;
    }
    return itemsLength;
}

function parseListOffsetValue(raw) {
    if (typeof raw === 'number' && Number.isFinite(raw) && raw >= 0) return raw;
    if (raw != null && raw !== '') {
        const n = parseInt(String(raw), 10);
        if (Number.isFinite(n) && n >= 0) return n;
    }
    return 0;
}

function parseProjectsListResponse(data) {
    if (Array.isArray(data)) {
        return { items: data, total: data.length, limit: data.length, offset: 0, isLegacyArray: true };
    }
    const items = data.projects || data.items || [];
    const arr = Array.isArray(items) ? items : [];
    return {
        items: arr,
        total: parseListTotalValue(data.total, arr.length),
        limit: parseListTotalValue(data.limit, arr.length) || arr.length,
        offset: parseListOffsetValue(data.offset),
        isLegacyArray: false,
    };
}

async function resolveProjectsListTotal(params, parsed, pageSize, offset) {
    const serverTotal = parsed.total;
    // 服务端 total 明确大于当前页末尾 → 直接信任
    if (!parsed.isLegacyArray && serverTotal > offset + parsed.items.length) {
        return serverTotal;
    }
    // 不足一页 → 已是最后一页
    if (parsed.items.length < pageSize) {
        return Math.max(serverTotal, offset + parsed.items.length);
    }
    // 满页但 total 可能被误算为 items.length → 探测下一页
    const probe = new URLSearchParams(params);
    probe.set('offset', String(offset + pageSize));
    probe.set('limit', '1');
    try {
        const res = await apiFetch(`/api/projects?${probe}`);
        if (!res.ok) return Math.max(serverTotal, offset + parsed.items.length);
        const probeParsed = parseProjectsListResponse(await res.json());
        if (probeParsed.total > serverTotal) return probeParsed.total;
        if (probeParsed.items.length > 0) {
            return Math.max(serverTotal, offset + pageSize + 1);
        }
    } catch (e) { /* ignore */ }
    return Math.max(serverTotal, offset + parsed.items.length);
}

async function fetchAllProjects(includeArchived) {
    const showArchived = includeArchived || document.getElementById('projects-show-archived')?.checked;
    let all = [];
    const pageSize = 200;
    let offset = 0;
    let total = Infinity;
    while (all.length < total) {
        const params = new URLSearchParams({ limit: String(pageSize), offset: String(offset) });
        if (!showArchived) params.set('status', 'active');
        const res = await apiFetch(`/api/projects?${params}`);
        if (!res.ok) throw new Error(tp('projects.loadProjectsFailed'));
        const parsed = parseProjectsListResponse(await res.json());
        all = all.concat(parsed.items);
        total = parsed.total;
        if (!parsed.items.length) break;
        offset += parsed.items.length;
    }
    return all;
}

async function fetchProjectsList(includeArchived, opts = {}) {
    const showArchived = includeArchived || document.getElementById('projects-show-archived')?.checked;
    const page = opts.page ?? projectsListPagination.page;
    const pageSize = opts.pageSize ?? getProjectsListPageSize();
    const search = opts.search !== undefined ? opts.search : projectsListSearch;
    projectsListSearch = search;
    const offset = (page - 1) * pageSize;
    const params = new URLSearchParams({ limit: String(pageSize), offset: String(offset) });
    if (search) params.set('search', search);
    if (!showArchived) params.set('status', 'active');
    const res = await apiFetch(`/api/projects?${params}`);
    if (!res.ok) throw new Error(tp('projects.loadProjectsFailed'));
    const parsed = parseProjectsListResponse(await res.json());
    const total = await resolveProjectsListTotal(params, parsed, pageSize, offset);
    projectsCache = parsed.items;
    projectsListPagination = { page, pageSize: pageSize, total };
    rebuildProjectNameMap(projectsCacheAll.length ? projectsCacheAll : projectsCache);
    return projectsCache;
}

/** 对话页等项目选择器：确保全量列表已拉取（去重并发请求） */
async function ensureProjectsLoaded(force) {
    if (!force && _projectsListReady) return projectsCacheAll;
    if (!force && _projectsFetchPromise) return _projectsFetchPromise;
    _projectsFetchPromise = fetchAllProjects(false)
        .then((list) => {
            projectsCacheAll = list;
            rebuildProjectNameMap(projectsCacheAll);
            _projectsListReady = true;
            if (typeof window.refreshConversationProjectFilter === 'function') {
                window.refreshConversationProjectFilter();
            }
            return projectsCacheAll;
        })
        .catch((e) => {
            _projectsListReady = false;
            throw e;
        })
        .finally(() => {
            _projectsFetchPromise = null;
        });
    return _projectsFetchPromise;
}

function isProjectsCacheReady() {
    return _projectsListReady;
}

function prefetchProjectsForChat() {
    const id = (resolveChatProjectSelection() || '').trim();
    if (id && !projectNameById[id]) {
        fetchProjectSummary(id).catch(() => {});
    }
    ensureProjectsLoaded().catch(() => {});
}

/** 新对话沿用用户最近选择的项目；没有选择时才保持未绑定。 */
async function ensureDefaultActiveProjectForNewChat() {
    const id = getActiveProjectId();
    if (!id) return '';
    const project = await fetchProjectSummary(id).catch(() => null);
    if (project && project.id && project.status !== 'archived') return project.id;
    setActiveProjectId('');
    return '';
}

function getProjectName(id) {
    return projectNameById[id] || id || '';
}

function initProjectsModalEscape() {
    if (window._projectsModalEscapeBound) return;
    window._projectsModalEscapeBound = true;
    document.addEventListener('keydown', (e) => {
        if (e.key !== 'Escape') return;
        if (isProjectsOverlayVisible('project-modal')) closeProjectModal();
        else if (isProjectsOverlayVisible('fact-modal')) closeFactModal();
        else if (isProjectsOverlayVisible('fact-detail-modal')) closeFactDetailModal();
    });
}

async function initProjectsPage() {
    const page = document.getElementById('page-projects');
    if (!page || page.style.display === 'none') return;
    initProjectsModalEscape();
    refreshProjectsFilterSelects();
    if (typeof syncAppModalBodyLock === 'function') {
        syncAppModalBodyLock();
    }
    updateProjectsDetailVisibility();
    projectsListPagination.pageSize = getProjectsListPageSize();
    renderProjectsPagination();
    await loadProjectsList();
    if (!currentProjectId && projectsCache.length) {
        const fromHash = new URLSearchParams(window.location.hash.split('?')[1] || '').get('id');
        currentProjectId = fromHash || projectsCache[0].id;
    }
    renderProjectsSidebar();
    if (currentProjectId) {
        await selectProject(currentProjectId);
    }
}

async function loadProjectsList() {
    _projectsListReady = false;
    projectsCacheAll = [];
    projectsListPagination.pageSize = getProjectsListPageSize();
    await fetchProjectsList();
    renderProjectsSidebar();
    renderProjectsPagination();
    try {
        projectsCacheAll = await fetchAllProjects();
        rebuildProjectNameMap(projectsCacheAll);
        _projectsListReady = true;
    } catch (e) {
        console.warn(e);
    }
    if (typeof refreshChatProjectSelector === 'function') {
        refreshChatProjectSelector();
    }
    if (typeof refreshVulnerabilityProjectFilter === 'function') {
        refreshVulnerabilityProjectFilter();
    }
    if (typeof window.refreshAllProjectFilterSelects === 'function') {
        await window.refreshAllProjectFilterSelects();
    }
}

function projectInitial(name) {
    const s = (name || 'P').trim();
    return s ? s.charAt(0).toUpperCase() : 'P';
}

function updateProjectsDetailVisibility() {
    const main = document.getElementById('projects-detail-main');
    const placeholder = document.getElementById('projects-detail-placeholder');
    const inner = document.getElementById('projects-detail-inner');
    const show = !!currentProjectId;
    if (main) main.classList.toggle('has-project', show);
    if (placeholder) placeholder.hidden = show;
    if (inner) inner.hidden = !show;
}

function updateProjectsListCount() {
    const el = document.getElementById('projects-list-count');
    if (el) el.textContent = String(projectsListPagination.total || projectsCache.length);
}

/** 事实分类 → 徽章样式（与 fact_template.go 常量对齐） */
const FACT_CATEGORY_BADGE = {
    target: 'projects-category--target',
    auth: 'projects-category--auth',
    infra: 'projects-category--infra',
    business: 'projects-category--business',
    finding: 'projects-category--finding',
    chain: 'projects-category--chain',
    exploit: 'projects-category--exploit',
    poc: 'projects-category--poc',
    note: 'projects-category--note',
    vuln: 'projects-category--exploit',
};

function formatCategoryBadge(category) {
    const raw = (category || '').trim();
    const c = raw.toLowerCase() || 'note';
    const cls = FACT_CATEGORY_BADGE[c] || 'projects-category--custom';
    return `<span class="projects-category ${cls}">${escapeHtml(raw || '—')}</span>`;
}

function formatConfidenceBadge(confidence) {
    const c = (confidence || '').toLowerCase();
    let cls = 'projects-confidence--tentative';
    let label = c || '—';
    if (c === 'confirmed') {
        cls = 'projects-confidence--confirmed';
        label = tp('projects.confidenceConfirmed');
    } else if (c === 'deprecated') {
        cls = 'projects-confidence--deprecated';
        label = tp('projects.confidenceDeprecated');
    } else if (c === 'tentative') {
        label = tp('projects.confidenceTentative');
    }
    return `<span class="projects-confidence ${cls}">${escapeHtml(label)}</span>`;
}

function renderProjectFactActions(keyEsc, idEsc, confidence) {
    const isDeprecated = (confidence || '').toLowerCase() === 'deprecated';
    const toggleBtn = isDeprecated
        ? `<button type="button" class="projects-action-btn projects-action-btn--restore" data-fact-key="${keyEsc}" onclick="restoreProjectFactByKey(this.dataset.factKey)" title="${escapeHtml(tp('projects.restoreTitle'))}">${escapeHtml(tp('projects.restore'))}</button>`
        : `<button type="button" class="projects-action-btn projects-action-btn--mute" data-fact-key="${keyEsc}" onclick="deprecateProjectFactByKey(this.dataset.factKey)" title="${escapeHtml(tp('projects.deprecateTitle'))}">${escapeHtml(tp('projects.deprecate'))}</button>`;
    return `<div class="projects-table-actions">
        <button type="button" class="projects-action-btn projects-action-btn--edit" data-fact-key="${keyEsc}" onclick="showEditFactModal(this.dataset.factKey)" title="${escapeHtml(tp('projects.editTitle'))}">${escapeHtml(tp('common.edit'))}</button>
        <button type="button" class="projects-action-btn projects-action-btn--view" data-fact-key="${keyEsc}" onclick="viewProjectFactBody(this.dataset.factKey)" title="${escapeHtml(tp('projects.viewBodyTitle'))}">${escapeHtml(tp('projects.details'))}</button>
        ${toggleBtn}
        <button type="button" class="projects-action-btn projects-action-btn--danger" data-fact-id="${idEsc}" onclick="deleteProjectFact(this.dataset.factId)" title="${escapeHtml(tp('projects.deleteForeverTitle'))}">${escapeHtml(tp('common.delete'))}</button>
    </div>`;
}

function formatSeverityBadge(severity) {
    const s = (severity || 'info').toLowerCase();
    const cls = 'projects-severity--' + (['critical', 'high', 'medium', 'low', 'info'].includes(s) ? s : 'info');
    return `<span class="projects-severity ${cls}">${escapeHtml(severity || '—')}</span>`;
}

function formatVulnStatusBadge(status) {
    const s = (status || 'open').toLowerCase();
    const labelMap = {
        open: 'vulnerabilityPage.statusOpen',
        confirmed: 'vulnerabilityPage.statusConfirmed',
        fixed: 'vulnerabilityPage.statusFixed',
        false_positive: 'vulnerabilityPage.statusFalsePositive',
        ignored: 'vulnerabilityPage.statusIgnored',
    };
    const label = labelMap[s] ? tp(labelMap[s]) : status || '—';
    const cls = ['open', 'confirmed', 'fixed', 'false_positive', 'ignored'].includes(s) ? s : 'open';
    return `<span class="status-badge status-${escapeHtml(cls)}">${escapeHtml(label)}</span>`;
}

let _projectVulnsFilterDebounce = null;

function buildProjectVulnsQueryParams() {
    const params = new URLSearchParams();
    params.set('project_id', currentProjectId);
    params.set('limit', '200');
    const search = document.getElementById('project-vulns-search')?.value?.trim();
    const severity = document.getElementById('project-vulns-filter-severity')?.value?.trim();
    const status = document.getElementById('project-vulns-filter-status')?.value?.trim();
    if (search) params.set('q', search);
    if (severity) params.set('severity', severity);
    if (status) params.set('status', status);
    return params;
}

function projectVulnsHasActiveFilter() {
    return !!(
        document.getElementById('project-vulns-search')?.value?.trim() ||
        document.getElementById('project-vulns-filter-severity')?.value ||
        document.getElementById('project-vulns-filter-status')?.value
    );
}

function debouncedLoadProjectVulnerabilities() {
    if (_projectVulnsFilterDebounce) clearTimeout(_projectVulnsFilterDebounce);
    _projectVulnsFilterDebounce = setTimeout(() => {
        _projectVulnsFilterDebounce = null;
        loadProjectVulnerabilities();
    }, 280);
}

function getProjectsListFilter() {
    return (document.getElementById('projects-list-search')?.value || '').trim().toLowerCase();
}

function filterProjectsList() {
    if (_projectsListSearchDebounce) clearTimeout(_projectsListSearchDebounce);
    _projectsListSearchDebounce = setTimeout(() => {
        _projectsListSearchDebounce = null;
        const q = getProjectsListFilter();
        projectsListPagination.page = 1;
        fetchProjectsList(undefined, { page: 1, search: q })
            .then(() => {
                renderProjectsSidebar();
                renderProjectsPagination();
            })
            .catch((e) => console.warn(e));
    }, 280);
}

function goProjectsPage(page) {
    const totalPages = Math.max(1, Math.ceil((projectsListPagination.total || 0) / projectsListPagination.pageSize) || 1);
    const next = Math.min(Math.max(1, page), totalPages);
    if (next === projectsListPagination.page) return;
    fetchProjectsList(undefined, { page: next })
        .then(() => {
            renderProjectsSidebar();
            renderProjectsPagination();
            const listEl = document.getElementById('projects-list');
            if (listEl) listEl.scrollTop = 0;
        })
        .catch((e) => console.warn(e));
}

function changeProjectsPageSize() {
    const sel = document.getElementById('projects-page-size-pagination');
    const newSize = sel ? parseInt(sel.value, 10) : 50;
    if (![20, 50, 100].includes(newSize)) return;
    try {
        localStorage.setItem(PROJECTS_LIST_PAGE_SIZE_KEY, String(newSize));
    } catch (e) { /* ignore */ }
    projectsListPagination.pageSize = newSize;
    projectsListPagination.page = 1;
    fetchProjectsList(undefined, { page: 1, pageSize: newSize })
        .then(() => {
            renderProjectsSidebar();
            renderProjectsPagination();
        })
        .catch((e) => console.warn(e));
}

function renderProjectsPagination() {
    const el = document.getElementById('projects-pagination');
    if (!el) return;
    const { page, pageSize, total } = projectsListPagination;
    const totalPages = Math.max(1, Math.ceil(total / pageSize) || 1);
    const navDisabled = total === 0 || totalPages <= 1;
    el.hidden = false;
    const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const end = total === 0 ? 0 : Math.min(page * pageSize, total);
    const infoText = tpFmt('projects.paginationRange', `${start}-${end}/${total}`, { start, end, total });
    const pageText = tpFmt('projects.paginationPage', `${page}/${totalPages}`, { page, total: totalPages });
    el.innerHTML = `
        <div class="sidebar-list-pagination-inner sidebar-list-pagination-inner--compact">
            <span class="pagination-info">${escapeHtml(infoText)}</span>
            <div class="pagination-controls">
                <button type="button" class="btn-icon-pagination" onclick="goProjectsPage(${page - 1})" ${page <= 1 || navDisabled ? 'disabled' : ''} title="${escapeHtml(tp('projects.paginationPrev'))}" aria-label="${escapeHtml(tp('projects.paginationPrev'))}">‹</button>
                <span class="pagination-page">${escapeHtml(pageText)}</span>
                <button type="button" class="btn-icon-pagination" onclick="goProjectsPage(${page + 1})" ${page >= totalPages || navDisabled ? 'disabled' : ''} title="${escapeHtml(tp('projects.paginationNext'))}" aria-label="${escapeHtml(tp('projects.paginationNext'))}">›</button>
            </div>
            <label class="pagination-page-size">
                ${escapeHtml(tp('projects.paginationPerPage'))}
                <select id="projects-page-size-pagination" class="projects-filter-select-native">
                    <option value="20" ${pageSize === 20 ? 'selected' : ''}>20</option>
                    <option value="50" ${pageSize === 50 ? 'selected' : ''}>50</option>
                    <option value="100" ${pageSize === 100 ? 'selected' : ''}>100</option>
                </select>
            </label>
        </div>`;
    refreshProjectsFilterSelects();
}

function renderProjectsSidebar() {
    const el = document.getElementById('projects-list');
    if (!el) return;
    updateProjectsListCount();
    const list = projectsCache;
    if (!projectsCache.length) {
        const createBtn = (typeof hasPermission === 'function' && hasPermission('project:write'))
            ? `<button type="button" class="btn-primary btn-small projects-empty-btn" onclick="showNewProjectModal()">${escapeHtml(tp('projects.newProject'))}</button>`
            : '';
        el.innerHTML = `<div class="projects-empty">${escapeHtml(tp('projects.noProjects'))}${createBtn ? `<br>${createBtn}` : ''}</div>`;
        updateProjectsDetailVisibility();
        renderProjectsPagination();
        return;
    }
    if (!list.length) {
        el.innerHTML = `<div class="projects-empty">${escapeHtml(tp('projects.noMatchingProjects'))}</div>`;
        updateProjectsDetailVisibility();
        renderProjectsPagination();
        return;
    }
    el.innerHTML = list.map((p) => {
        const fullName = p.name || tp('common.untitled');
        const displayName = window.formatProjectNameForDisplay(fullName);
        const active = p.id === currentProjectId ? ' is-active' : '';
        const archived = p.status === 'archived' ? ' is-archived' : '';
        const badges = [
            p.pinned ? `<span class="projects-list-item-badge">${escapeHtml(tp('projects.pinned'))}</span>` : '',
            p.status === 'archived' ? `<span class="projects-list-item-badge">${escapeHtml(tp('projects.archived'))}</span>` : '',
        ].join('');
        return `<div class="projects-list-item${active}${archived}" data-id="${escapeAttr(p.id)}" onclick="selectProject(${escapeJsStringAttr(p.id)})">
            <div class="projects-list-item-body">
                <div class="projects-list-item-name" title="${escapeAttr(fullName)}">${escapeHtml(displayName)}${badges}</div>
                <div class="projects-list-item-meta">${formatProjectTime(p.updated_at)}</div>
            </div>
            <button type="button" class="projects-list-item-menu" title="${escapeHtml(tp('projects.projectActions'))}" aria-label="${escapeHtml(tp('projects.projectActions'))}" onclick="showProjectListActionMenu(event, ${escapeJsStringAttr(p.id)})">⋯</button>
        </div>`;
    }).join('');
    updateProjectsDetailVisibility();
    if (typeof applyRBACToUI === 'function') applyRBACToUI(el);
}

function clampProjectDescription(text) {
    const s = (text || '').trim();
    if (s.length <= PROJECT_DESCRIPTION_MAX_LENGTH) return s;
    return s.slice(0, PROJECT_DESCRIPTION_MAX_LENGTH);
}

function renderProjectDetailTitle(name) {
    const titleEl = document.getElementById('projects-detail-title');
    if (!titleEl) return;
    const text = (name || '').trim() || tp('projects.defaultProjectName');
    window.applyProjectNameDisplay(titleEl, text);
}

function renderProjectDetailDesc(desc) {
    const descEl = document.getElementById('projects-detail-desc');
    if (!descEl) return;
    const text = (desc || '').trim();
    if (!text) {
        descEl.hidden = true;
        descEl.textContent = '';
        descEl.removeAttribute('title');
        return;
    }
    descEl.textContent = text;
    descEl.title = text;
    descEl.hidden = false;
}

function updateProjectStatusPill(status) {
    const el = document.getElementById('projects-detail-status');
    if (!el) return;
    const archived = status === 'archived';
    el.textContent = archived ? tp('projects.statusArchived') : tp('projects.statusActive');
    el.className = 'projects-status-pill ' + (archived ? 'projects-status-pill--archived' : 'projects-status-pill--active');
}

function renderProjectDetailMeta(updatedAt) {
    const metaEl = document.getElementById('projects-detail-meta');
    const timeEl = document.getElementById('projects-detail-meta-time');
    if (!metaEl || !timeEl) return;
    const time = formatProjectTime(updatedAt);
    const full = tpFmt('projects.updatedPrefix', `Updated ${time}`, { time });
    timeEl.textContent = time;
    metaEl.title = full;
}

function refreshProjectDetailMetaI18n() {
    if (!currentProjectId) return;
    let updatedAt = currentProjectUpdatedAt;
    if (updatedAt == null) {
        const source = projectsCacheAll.length ? projectsCacheAll : projectsCache;
        const p = source.find((x) => x.id === currentProjectId);
        updatedAt = p?.updated_at;
    }
    renderProjectDetailMeta(updatedAt);
}

function updateProjectStats(stats) {
    const s = stats || {};
    const f = document.getElementById('project-stat-facts');
    const v = document.getElementById('project-stat-vulns');
    const c = document.getElementById('project-stat-conversations');
    const sparse = document.getElementById('project-stat-sparse');
    const fc = s.fact_count ?? s.factCount ?? 0;
    const vc = s.vuln_count ?? s.vulnCount ?? 0;
    const cc = s.conversation_count ?? s.conversationCount ?? 0;
    const sc = s.sparse_fact_count ?? s.sparseFactCount ?? 0;
    if (f) f.textContent = tpFmt('projects.statsFacts', `${fc} facts`, { count: fc });
    if (v) v.textContent = tpFmt('projects.statsVulns', `${vc} vulnerabilities`, { count: vc });
    if (c) c.textContent = tpFmt('projects.statsConversations', `${cc} conversations`, { count: cc });
    if (sparse) {
        if (sc > 0) {
            sparse.hidden = false;
            sparse.textContent = tpFmt('projects.statsSparse', `${sc} to complete`, { count: sc });
        } else {
            sparse.hidden = true;
        }
    }
}

async function selectProject(id) {
    currentProjectId = id;
    if (id) setActiveProjectId(id);
    projectAssetsPagination.page = 1;
    const searchEl = document.getElementById('project-facts-search');
    const catEl = document.getElementById('project-facts-filter-category');
    const confEl = document.getElementById('project-facts-filter-confidence');
    const sparseEl = document.getElementById('project-facts-filter-sparse');
    const vulnSearchEl = document.getElementById('project-vulns-search');
    const vulnSevEl = document.getElementById('project-vulns-filter-severity');
    const vulnStatusEl = document.getElementById('project-vulns-filter-status');
    if (searchEl) searchEl.value = '';
    if (catEl) catEl.value = '';
    if (confEl) confEl.value = '';
    if (sparseEl) sparseEl.checked = false;
    if (vulnSearchEl) vulnSearchEl.value = '';
    if (vulnSevEl) vulnSevEl.value = '';
    if (vulnStatusEl) vulnStatusEl.value = '';
    syncAllProjectsFilterSelects();
    renderProjectsSidebar();
    updateProjectsDetailVisibility();
    try {
        const res = await apiFetch(`/api/projects/${id}`);
        if (!res.ok) throw new Error(tp('projects.projectNotFound'));
        const p = await res.json();
        renderProjectDetailTitle(p.name);
        document.getElementById('project-edit-name').value = p.name || '';
        document.getElementById('project-edit-description').value = p.description || '';
        document.getElementById('project-edit-scope').value = p.scope_json || '';
        const statusEl = document.getElementById('project-edit-status');
        if (statusEl) statusEl.value = p.status || 'active';
        syncAllProjectsFilterSelects();
        const pinEl = document.getElementById('project-edit-pinned');
        if (pinEl) pinEl.checked = !!p.pinned;
        updateProjectStatusPill(p.status || 'active');
        currentProjectUpdatedAt = p.updated_at;
        renderProjectDetailMeta(currentProjectUpdatedAt);
        renderProjectDetailDesc(p.description);
        projectNameById[p.id] = p.name || p.id;
    } catch (e) {
        console.warn(e);
    }
    await refreshProjectHeaderStats();
    switchProjectTab(currentProjectTab);
}

function switchProjectTab(tab) {
    if (tab === 'assets' && typeof hasPermission === 'function' && !hasPermission('asset:read')) tab = 'facts';
    currentProjectTab = tab;
    ['facts', 'graph', 'assets', 'conversations', 'vulns', 'settings'].forEach((t) => {
        const btn = document.getElementById(`project-tab-${t}`);
        const panel = document.getElementById(`project-panel-${t}`);
        if (btn) btn.classList.toggle('is-active', t === tab);
        if (panel) panel.hidden = t !== tab;
    });
    if (tab === 'facts') loadProjectFacts();
    if (tab === 'graph') loadProjectFactGraph();
    if (tab === 'assets') loadProjectAssets();
    if (tab === 'conversations') loadProjectConversations();
    if (tab === 'vulns') loadProjectVulnerabilities();
}

async function loadProjectAssets(page) {
    const tbody = document.getElementById('project-assets-tbody');
    const countEl = document.getElementById('project-assets-count');
    if (!tbody || !currentProjectId) return;
    const requestedPage = Math.max(1, Number(page || projectAssetsPagination.page || 1));
    projectAssetsPagination.page = requestedPage;
    tbody.innerHTML = `<tr class="is-empty-row"><td colspan="7">${escapeHtml(tpFmt('common.loading', '加载中...'))}</td></tr>`;
    const qs = new URLSearchParams({
        project_id: currentProjectId,
        page: String(requestedPage),
        page_size: String(projectAssetsPagination.pageSize),
    });
    const res = await apiFetch(`/api/assets?${qs.toString()}`);
    if (!res.ok) {
        currentProjectAssets = [];
        projectAssetsPagination.total = 0;
        projectAssetsPagination.totalPages = 1;
        if (countEl) countEl.textContent = '0';
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="7">${escapeHtml(tpFmt('common.loadFailed', '加载失败'))}</td></tr>`;
        renderProjectAssetsPagination();
        return;
    }
    const data = await res.json();
    currentProjectAssets = data.assets || [];
    projectAssetsPagination.page = Number(data.page || requestedPage);
    projectAssetsPagination.total = Number(data.total || 0);
    projectAssetsPagination.totalPages = Math.max(1, Number(data.total_pages || 1));
    if (projectAssetsPagination.page > projectAssetsPagination.totalPages) {
        return loadProjectAssets(projectAssetsPagination.totalPages);
    }
    if (countEl) countEl.textContent = tpFmt('projects.assetCount', `${data.total || 0} 个资产`, { count: data.total || 0 });
    if (!currentProjectAssets.length) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="7">${escapeHtml(tpFmt('projects.noBoundAssets', '暂无绑定到此项目的资产'))}</td></tr>`;
        renderProjectAssetsPagination();
        return;
    }
    tbody.innerHTML = currentProjectAssets.map((asset, index) => {
        const target = asset.host || asset.domain || asset.ip || '-';
        const service = [asset.protocol, asset.port ? ':' + asset.port : ''].join('') || '-';
        const fingerprint = [asset.title, asset.server].filter(Boolean).join(' · ') || '-';
        const updated = asset.last_seen_at ? new Date(asset.last_seen_at).toLocaleString() : '-';
        const status = asset.status === 'inactive' ? tpFmt('assets.statusInactive', '停用') : tpFmt('assets.statusActive', '活跃');
        return `<tr>
            <td class="cell-summary"><button type="button" class="projects-asset-target" onclick="openProjectAssetDetail(${index})" title="${escapeHtml(target)}">${escapeHtml(target)}</button></td>
            <td><code>${escapeHtml(service)}</code></td>
            <td class="cell-summary" title="${escapeHtml(fingerprint)}">${escapeHtml(fingerprint)}</td>
            <td>${escapeHtml(asset.source || '-')}</td>
            <td>${escapeHtml(updated)}</td>
            <td><span class="asset-status asset-status--${escapeHtml(asset.status || 'active')}">${escapeHtml(status)}</span></td>
            <td class="col-actions"><div class="projects-table-actions"><button type="button" class="projects-action-btn projects-action-btn--mute" data-require-permission="asset:write" onclick="unbindAssetFromProject(${index})" title="${escapeHtml(tp('projects.unbindProjectTitle'))}">${escapeHtml(tp('projects.unbind'))}</button></div></td>
        </tr>`;
    }).join('');
    renderProjectAssetsPagination();
    const tableWrap = document.querySelector('#project-panel-assets .projects-table-wrap');
    if (tableWrap) tableWrap.scrollTop = 0;
}

function renderProjectAssetsPagination() {
    const root = document.getElementById('project-assets-pagination');
    if (!root) return;
    const { page, pageSize, total, totalPages } = projectAssetsPagination;
    const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const end = total === 0 ? 0 : Math.min(page * pageSize, total);
    const atFirst = page <= 1 || total === 0;
    const atLast = page >= totalPages || total === 0;
    root.innerHTML = `<div class="pagination">
        <div class="pagination-info">
            <span>${escapeHtml(tpFmt('projects.paginationShow', `显示 ${start}-${end} / 共 ${total}`, { start, end, total }))}</span>
            <label class="pagination-page-size">${escapeHtml(tpFmt('projects.paginationPerPage', '每页显示'))}
                <select id="project-assets-page-size" onchange="changeProjectAssetsPageSize(this.value)">
                    ${[10, 20, 50, 100].map(size => `<option value="${size}" ${size === pageSize ? 'selected' : ''}>${size}</option>`).join('')}
                </select>
            </label>
        </div>
        <div class="pagination-controls">
            <button type="button" class="btn-secondary" onclick="loadProjectAssets(1)" ${atFirst ? 'disabled' : ''}>${escapeHtml(tpFmt('skillsPage.firstPage', '首页'))}</button>
            <button type="button" class="btn-secondary" onclick="loadProjectAssets(${Math.max(1, page - 1)})" ${atFirst ? 'disabled' : ''}>${escapeHtml(tpFmt('projects.paginationPrev', '上一页'))}</button>
            <span class="pagination-page">${escapeHtml(tpFmt('skillsPage.pageOf', `第 ${page} / ${totalPages} 页`, { current: page, total: totalPages }))}</span>
            <button type="button" class="btn-secondary" onclick="loadProjectAssets(${Math.min(totalPages, page + 1)})" ${atLast ? 'disabled' : ''}>${escapeHtml(tpFmt('projects.paginationNext', '下一页'))}</button>
            <button type="button" class="btn-secondary" onclick="loadProjectAssets(${totalPages})" ${atLast ? 'disabled' : ''}>${escapeHtml(tpFmt('skillsPage.lastPage', '尾页'))}</button>
        </div>
    </div>`;
}

function changeProjectAssetsPageSize(value) {
    const size = Number(value);
    if (![10, 20, 50, 100].includes(size)) return;
    projectAssetsPagination.pageSize = size;
    projectAssetsPagination.page = 1;
    try {
        localStorage.setItem(PROJECT_ASSETS_PAGE_SIZE_KEY, String(size));
    } catch (e) { /* ignore */ }
    loadProjectAssets(1);
}

function openProjectAssetDetail(index) {
    const asset = currentProjectAssets[Number(index)];
    if (asset && typeof window.openAssetDetailRecord === 'function') window.openAssetDetailRecord(asset);
}

async function unbindAssetFromProject(index) {
    const asset = currentProjectAssets[Number(index)];
    if (!asset || !asset.id || !currentProjectId) return;
    const target = asset.host || asset.domain || asset.ip || asset.id;
    const message = tpFmt('projects.unbindAssetConfirm', `确定将“${target}”从当前项目解绑吗？资产不会被删除。`, { target });
    if (!confirm(message)) return;
    try {
        const res = await apiFetch('/api/assets/project-binding', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ asset_ids: [asset.id], project_id: '' })
        });
        if (!res.ok) throw new Error(await res.text());
        if (typeof showInlineToast === 'function') {
            showInlineToast(tpFmt('projects.unbindAssetDone', '已从项目解绑资产', { target }));
        }
        await loadProjectAssets(projectAssetsPagination.page);
        await refreshProjectHeaderStats();
    } catch (error) {
        alert(`${tp('projects.unbindFailed')}: ${error.message || error}`);
    }
}

let _selectedGraphFactKey = null;
let _selectedGraphEdgeId = null;
let _currentGraphData = null;
let _graphConnectMode = false;
let _graphConnectSource = null;

function toggleProjectFactGraphConnectMode() {
    _graphConnectMode = !_graphConnectMode;
    _graphConnectSource = null;
    const btn = document.getElementById('project-graph-connect-btn');
    if (btn) {
        btn.classList.toggle('is-active', _graphConnectMode);
        btn.textContent = _graphConnectMode ? tp('projects.graphConnectActive') : tp('projects.graphConnect');
        btn.classList.toggle('projects-graph-action-btn--connect-active', _graphConnectMode);
    }
    if (typeof ProjectFactGraph !== 'undefined') {
        ProjectFactGraph.setConnectMode(_graphConnectMode, handleGraphConnectNodePick);
    }
}

async function handleGraphConnectNodePick(factKey) {
    if (!factKey || String(factKey).startsWith('vuln:')) return;
    if (!requireProjectWrite()) return;
    if (!_graphConnectSource) {
        _graphConnectSource = factKey;
        if (typeof showNotification === 'function') {
            showNotification(tpFmt('projects.graphConnectPickTarget', `已选源节点 ${factKey}，请点击目标节点`, { source: factKey }), 'info');
        }
        return;
    }
    if (_graphConnectSource === factKey) return;
    const edgeType = window.prompt(tp('projects.graphEdgeTypePrompt'), 'leads_to');
    if (!edgeType) {
        _graphConnectSource = null;
        return;
    }
    const res = await apiFetch(`/api/projects/${currentProjectId}/fact-edges`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            source_fact_key: _graphConnectSource,
            target_fact_key: factKey,
            edge_type: edgeType.trim(),
        }),
    });
    _graphConnectSource = null;
    if (!(await notifyProjectApiFailure(res, 'projects.graphConnectFailed', '创建边失败'))) return;
    if (typeof showNotification === 'function') showNotification(tp('projects.graphConnectSuccess'), 'success');
    loadProjectFactGraph();
    loadProjectFacts();
}

function formatIncomingLinksForModal(links) {
    if (!links || !links.length) return '';
    return links
        .map((e) => `${e.edge_type || e.type}: ${e.source_fact_key || e.from}`)
        .join('\n');
}


async function loadProjectFactGraph() {
    const container = document.getElementById('project-fact-graph-container');
    const statsEl = document.getElementById('project-fact-graph-stats');
    if (!container || !currentProjectId) return;
    container.innerHTML = `<div class="loading-spinner">${escapeHtml(tp('common.loading'))}</div>`;
    closeProjectFactGraphSidebar();
    const view = document.getElementById('project-graph-view')?.value || 'path';
    const hideDeprecated = document.getElementById('project-facts-filter-hide-deprecated')?.checked !== false;
    const params = new URLSearchParams({ view });
    if (!hideDeprecated) params.set('exclude_deprecated', '0');
    try {
        const res = await apiFetch(`/api/projects/${currentProjectId}/fact-graph?${params}`);
        if (!res.ok) throw new Error(tp('common.loadFailed'));
        const data = await res.json();
        _currentGraphData = data;
        if (typeof ProjectFactGraph !== 'undefined') {
            ProjectFactGraph.render(container, data, {
                emptyText: tp('projects.graphEmpty'),
                emptyTitle: tp('projects.graphEmptyTitle'),
                emptySteps: [
                    tp('projects.graphEmptyStep1'),
                    tp('projects.graphEmptyStep2'),
                    tp('projects.graphEmptyStep3'),
                ],
                emptyActionLabel: tp('projects.graphEmptyCta'),
                onEmptyAction: () => showAddFactModal(),
                onNodeSelect: (factKey) => showProjectFactGraphNode(factKey, _currentGraphData),
                onEdgeSelect: (edgeId) => showProjectFactGraphEdge(edgeId, _currentGraphData),
            });
        }
        const nodeCount = (data.nodes || []).length;
        const edgeCount = (data.edges || []).length;
        if (statsEl) {
            statsEl.innerHTML =
                `<span class="projects-graph-stat-badge"><strong>${nodeCount}</strong> ${escapeHtml(tp('projects.graphStatsNodes'))}</span>` +
                `<span class="projects-graph-stat-badge"><strong>${edgeCount}</strong> ${escapeHtml(tp('projects.graphStatsEdges'))}</span>`;
        }
    } catch (e) {
        container.innerHTML = `<div class="error-message">${escapeHtml(e.message || tp('common.loadFailed'))}</div>`;
        if (statsEl) statsEl.textContent = '';
    }
}

function filterProjectFactGraph() {
    const q = document.getElementById('project-graph-search')?.value || '';
    if (typeof ProjectFactGraph !== 'undefined') {
        ProjectFactGraph.filterBySearch(q);
    }
}

function centerProjectFactGraph() {
    if (typeof ProjectFactGraph !== 'undefined') ProjectFactGraph.center();
}

function closeProjectFactGraphSidebar() {
    _selectedGraphFactKey = null;
    _selectedGraphEdgeId = null;
    if (typeof ProjectFactGraph !== 'undefined') ProjectFactGraph.clearEdgeSelection();
    const sidebar = document.getElementById('project-fact-graph-sidebar');
    if (sidebar) sidebar.hidden = true;
}

function isSyntheticGraphEdge(edge) {
    if (!edge) return true;
    const id = String(edge.id || '');
    const type = String(edge.type || '');
    return id.startsWith('vuln-link:') || type === 'links_vuln';
}

function getGraphEdgesForFact(factKey, graphData) {
    if (!factKey || !graphData?.edges) return [];
    return graphData.edges.filter((e) => e.source === factKey || e.target === factKey);
}

function renderGraphEdgesListHtml(factKey, graphData, selectedEdgeId) {
    const edges = getGraphEdgesForFact(factKey, graphData);
    if (!edges.length) {
        return `<p class="project-fact-graph-edges-empty">${escapeHtml(tp('projects.graphEdgesEmpty'))}</p>`;
    }
    return edges
        .map((e) => {
            const isOut = e.source === factKey;
            const dirLabel = isOut ? tp('projects.graphEdgeFromSelf') : tp('projects.graphEdgeToSelf');
            const src = e.source || '';
            const tgt = e.target || '';
            const selected = e.id === selectedEdgeId ? ' is-selected' : '';
            const synthetic = isSyntheticGraphEdge(e);
            const deleteBtn = synthetic
                ? `<span class="project-fact-graph-edge-synthetic" title="${escapeHtml(tp('projects.graphEdgeSynthetic'))}">—</span>`
                : `<button type="button" class="project-fact-graph-edge-delete" data-edge-id="${escapeAttr(e.id)}" onclick="event.stopPropagation(); deleteProjectFactEdge(this.dataset.edgeId)" title="${escapeAttr(tp('projects.graphDeleteEdge'))}"><svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg></button>`;
            return `<div class="project-fact-graph-edge-item${selected}" data-edge-id="${escapeAttr(e.id)}" onclick="focusProjectFactGraphEdge(${escapeJsStringAttr(e.id)})">
                <span class="project-fact-graph-edge-dir">${escapeHtml(dirLabel)}</span>
                <span class="project-fact-graph-edge-type">${escapeHtml(e.type || '')}</span>
                <span class="project-fact-graph-edge-peer" title="${escapeHtml(src + ' → ' + tgt)}">${escapeHtml(src)} → ${escapeHtml(tgt)}</span>
                ${deleteBtn}
            </div>`;
        })
        .join('');
}

function renderProjectFactGraphEdges(factKey, graphData, selectedEdgeId) {
    const wrap = document.getElementById('project-fact-graph-edges-wrap');
    const list = document.getElementById('project-fact-graph-edges-list');
    if (!wrap || !list) return;
    const edges = getGraphEdgesForFact(factKey, graphData);
    wrap.hidden = false;
    list.innerHTML = renderGraphEdgesListHtml(factKey, graphData, selectedEdgeId);
    if (selectedEdgeId) {
        const selectedEl = list.querySelector('[data-edge-id="' + String(selectedEdgeId).replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"]');
        if (selectedEl) selectedEl.scrollIntoView({ block: 'nearest' });
    }
    if (!edges.length) wrap.hidden = false;
}

function graphVulnIdFromKey(factKey) {
    const key = String(factKey || '');
    if (!key.startsWith('vuln:')) return null;
    return key.slice(5);
}

function showProjectFactGraphNode(factKey, graphData, selectedEdgeId) {
    if (!factKey) {
        closeProjectFactGraphSidebar();
        return;
    }
    _selectedGraphFactKey = factKey;
    _selectedGraphEdgeId = selectedEdgeId || null;
    const node = (graphData?.nodes || []).find((n) => n.fact_key === factKey || n.id === factKey);
    const vulnId = graphVulnIdFromKey(factKey);
    const isVulnNode = !!vulnId;
    const sidebar = document.getElementById('project-fact-graph-sidebar');
    const titleEl = document.getElementById('project-fact-graph-node-title');
    const metaEl = document.getElementById('project-fact-graph-node-meta');
    const categoryEl = document.getElementById('project-fact-graph-node-category');
    const detailBtn = document.getElementById('project-fact-graph-detail-btn');
    const editBtn = document.getElementById('project-fact-graph-edit-btn');
    if (!sidebar || !titleEl || !metaEl) return;
    titleEl.textContent = isVulnNode ? vulnId : factKey;
    titleEl.title = isVulnNode ? vulnId : factKey;
    if (categoryEl) {
        const visualType =
            typeof ProjectFactGraph !== 'undefined' && ProjectFactGraph.resolveGraphNodeType
                ? ProjectFactGraph.resolveGraphNodeType(node)
                : node?.type || node?.category || 'note';
        const theme =
            typeof ProjectFactGraph !== 'undefined' && ProjectFactGraph.nodeTheme
                ? ProjectFactGraph.nodeTheme(visualType)
                : { typeEn: String(visualType).toUpperCase(), typeLabel: visualType };
        categoryEl.textContent = theme.typeEn || String(visualType).toUpperCase();
        categoryEl.hidden = false;
        categoryEl.className = 'project-fact-graph-node-category project-fact-graph-node-category--' + visualType;
        categoryEl.title = theme.typeLabel || visualType;
    }
    const conf = node?.confidence || '';
    const summary = (node?.summary || node?.label || '').trim();
    if (summary || conf || isVulnNode) {
        const parts = [];
        if (summary) {
            parts.push(`<span class="project-fact-graph-node-summary">${escapeHtml(summary)}</span>`);
        }
        if (isVulnNode) {
            parts.push(
                `<span class="project-fact-graph-node-vuln-hint">${escapeHtml(tp('projects.graphVulnSidebarHint'))}</span>`,
            );
        }
        if (conf) {
            parts.push(formatConfidenceBadge(conf));
        }
        metaEl.innerHTML = parts.join('');
    } else {
        metaEl.textContent = '';
    }
    if (detailBtn) {
        detailBtn.textContent = isVulnNode ? tp('projects.viewVulnerability') : tp('projects.details');
    }
    if (editBtn) {
        editBtn.hidden = isVulnNode;
    }
    renderProjectFactGraphEdges(factKey, graphData, _selectedGraphEdgeId);
    if (_selectedGraphEdgeId && typeof ProjectFactGraph !== 'undefined') {
        ProjectFactGraph.selectEdge(_selectedGraphEdgeId);
    } else if (typeof ProjectFactGraph !== 'undefined') {
        ProjectFactGraph.clearEdgeSelection();
    }
    sidebar.hidden = false;
}

function showProjectFactGraphEdge(edgeId, graphData) {
    const edge = (graphData?.edges || []).find((e) => e.id === edgeId);
    if (!edge) return;
    const anchorKey = edge.source && !String(edge.source).startsWith('vuln:') ? edge.source : edge.target;
    showProjectFactGraphNode(anchorKey, graphData, edgeId);
}

function focusProjectFactGraphEdge(edgeId) {
    if (!edgeId || !_currentGraphData) return;
    showProjectFactGraphEdge(edgeId, _currentGraphData);
}

async function deleteProjectFactEdge(edgeId) {
    if (!edgeId || !currentProjectId) return;
    if (!requireProjectWrite()) return;
    const edge = (_currentGraphData?.edges || []).find((e) => e.id === edgeId);
    if (isSyntheticGraphEdge(edge)) return;
    if (!confirm(tp('projects.confirmDeleteGraphEdge'))) return;
    const res = await apiFetch(`/api/projects/${currentProjectId}/fact-edges/${encodeURIComponent(edgeId)}`, {
        method: 'DELETE',
    });
    if (!(await notifyProjectApiFailure(res, 'projects.graphEdgeDeleteFailed', '删除边失败'))) return;
    if (typeof showNotification === 'function') showNotification(tp('projects.graphEdgeDeleteSuccess'), 'success');
    const keepKey = _selectedGraphFactKey;
    await loadProjectFactGraph();
    if (keepKey) showProjectFactGraphNode(keepKey, _currentGraphData);
    loadProjectFacts();
}

function openSelectedGraphFactDetail() {
    if (!_selectedGraphFactKey) return;
    const vulnId = graphVulnIdFromKey(_selectedGraphFactKey);
    if (vulnId) {
        openVulnerabilityDetail(vulnId);
        return;
    }
    viewProjectFactBody(_selectedGraphFactKey);
}

function editSelectedGraphFact() {
    if (_selectedGraphFactKey) showEditFactModal(_selectedGraphFactKey);
}

function buildProjectFactsQueryParams() {
    const params = new URLSearchParams();
    params.set('limit', '200');
    params.set('include_link_counts', 'true');
    const search = document.getElementById('project-facts-search')?.value?.trim();
    const category = document.getElementById('project-facts-filter-category')?.value?.trim();
    const confidence = document.getElementById('project-facts-filter-confidence')?.value?.trim();
    const sparseOnly = document.getElementById('project-facts-filter-sparse')?.checked;
    const hideDeprecated = document.getElementById('project-facts-filter-hide-deprecated')?.checked;
    if (search) params.set('search', search);
    if (category) params.set('category', category);
    if (confidence) params.set('confidence', confidence);
    if (sparseOnly) params.set('sparse_only', 'true');
    if (hideDeprecated) params.set('exclude_deprecated', 'true');
    return params;
}

function debouncedLoadProjectFacts() {
    if (_projectFactsFilterDebounce) clearTimeout(_projectFactsFilterDebounce);
    _projectFactsFilterDebounce = setTimeout(() => {
        _projectFactsFilterDebounce = null;
        loadProjectFacts();
    }, 280);
}

async function loadProjectFacts() {
    const tbody = document.getElementById('project-facts-tbody');
    if (!tbody || !currentProjectId) return;
    tbody.innerHTML = `<tr class="is-empty-row"><td colspan="8">${escapeHtml(tp('common.loading'))}</td></tr>`;
    const qs = buildProjectFactsQueryParams().toString();
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts?${qs}`);
    if (!res.ok) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="8">${escapeHtml(tp('common.loadFailed'))}</td></tr>`;
        return;
    }
    const facts = await res.json();
    if (!facts.length) {
        const hasFilter =
            document.getElementById('project-facts-search')?.value?.trim() ||
            document.getElementById('project-facts-filter-category')?.value ||
            document.getElementById('project-facts-filter-confidence')?.value ||
            document.getElementById('project-facts-filter-sparse')?.checked;
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="8">${
            hasFilter ? tp('projects.noMatchingFacts') : tp('projects.noFacts')
        }</td></tr>`;
        refreshProjectHeaderStats();
        return;
    }
    tbody.innerHTML = facts.map((f) => {
        const keyEsc = escapeHtml(f.fact_key);
        const idEsc = escapeHtml(f.id);
        const vulnLink = f.related_vulnerability_id
            ? `<span class="projects-fact-vuln-link" title="${escapeHtml(tp('projects.relatedVulnIdTitle'))}">${escapeHtml(f.related_vulnerability_id.slice(0, 8))}…</span>`
            : '';
        const pinBadge = f.pinned
            ? `<span class="projects-list-item-badge" title="${escapeHtml(tp('projects.pinned'))}">${escapeHtml(tp('projects.pinned'))}</span>`
            : '';
        const lc = f.link_counts || {};
        const linkBadge =
            lc.outgoing || lc.incoming
                ? `<span class="projects-fact-link-badge" title="${escapeHtml(tp('projects.linkCountsTitle'))}">↑${lc.outgoing || 0} ↓${lc.incoming || 0}</span>`
                : '<span class="projects-fact-link-badge projects-fact-link-badge--empty">—</span>';
        return `<tr>
            <td class="cell-fact-key"><code class="projects-fact-key-chip" title="${keyEsc}">${keyEsc}</code>${pinBadge}${vulnLink}</td>
            <td class="cell-fact-category">${formatCategoryBadge(f.category)}</td>
            <td class="cell-summary" title="${escapeHtml(f.summary)}">${escapeHtml(f.summary)}</td>
            <td class="cell-fact-links">${linkBadge}</td>
            <td>${formatFactBodyBadge(f)}</td>
            <td>${formatConfidenceBadge(f.confidence)}</td>
            <td>${formatProjectTime(f.updated_at, f.created_at)}</td>
            <td class="col-actions">${renderProjectFactActions(keyEsc, idEsc, f.confidence)}</td>
        </tr>`;
    }).join('');
    refreshProjectHeaderStats();
}

async function refreshProjectHeaderStats() {
    if (!currentProjectId) return;
    try {
        const res = await apiFetch(`/api/projects/${currentProjectId}/stats`);
        if (!res.ok) return;
        const stats = await res.json();
        updateProjectStats(stats);
    } catch (e) {
        console.warn(e);
    }
}

async function loadProjectConversations() {
    const tbody = document.getElementById('project-conversations-tbody');
    if (!tbody || !currentProjectId) return;
    tbody.innerHTML = `<tr class="is-empty-row"><td colspan="3">${escapeHtml(tp('common.loading'))}</td></tr>`;
    const res = await apiFetch(`/api/projects/${currentProjectId}/conversations?limit=100`);
    if (!res.ok) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="3">${escapeHtml(tp('common.loadFailed'))}</td></tr>`;
        return;
    }
    const data = await res.json();
    const items = data.conversations || [];
    if (!items.length) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="3">${escapeHtml(tp('projects.noBoundConversations'))}</td></tr>`;
        return;
    }
    tbody.innerHTML = items
        .map((conv) => {
            const id = conv.id;
            const idEsc = escapeHtml(id);
            const title = escapeHtml(conv.title || tp('projects.untitledConversation'));
            const updated = formatProjectTime(conv.updatedAt || conv.updated_at, conv.createdAt || conv.created_at);
            return `<tr>
            <td class="cell-summary" title="${title}">${title}</td>
            <td>${escapeHtml(updated)}</td>
            <td class="col-actions">
                <div class="projects-table-actions">
                    <button type="button" class="projects-action-btn projects-action-btn--view" data-conv-id="${idEsc}" onclick="openProjectConversation(this.dataset.convId)">${escapeHtml(tp('projects.open'))}</button>
                    <button type="button" class="projects-action-btn" data-conv-id="${idEsc}" onclick="promoteConversationAttackChain(this.dataset.convId)" title="${escapeHtml(tp('projects.promoteAttackChainTitle'))}">${escapeHtml(tp('projects.promoteAttackChain'))}</button>
                    <button type="button" class="projects-action-btn projects-action-btn--mute" data-conv-id="${idEsc}" onclick="unbindConversationFromProject(this.dataset.convId)" title="${escapeHtml(tp('projects.unbindProjectTitle'))}">${escapeHtml(tp('projects.unbind'))}</button>
                </div>
            </td>
        </tr>`;
        })
        .join('');
}

function openProjectConversation(conversationId) {
    if (!conversationId) return;
    if (typeof switchPage === 'function') {
        switchPage('chat');
    }
    setTimeout(() => {
        if (typeof loadConversation === 'function') {
            loadConversation(conversationId);
        }
    }, 200);
}

async function promoteConversationAttackChain(conversationId) {
    if (!currentProjectId || !conversationId) return;
    if (!requireProjectWrite()) return;
    if (!confirm(tp('projects.confirmPromoteAttackChain'))) return;
    const res = await apiFetch(
        `/api/projects/${currentProjectId}/promote-attack-chain/${encodeURIComponent(conversationId)}`,
        { method: 'POST' },
    );
    if (!(await notifyProjectApiFailure(res, 'projects.promoteAttackChainFailed', '沉淀失败'))) return;
    const data = await res.json();
    if (typeof showNotification === 'function') {
        showNotification(
            tpFmt(
                'projects.promoteAttackChainSuccess',
                `已沉淀 ${data.facts_created || 0} 新 / ${data.facts_updated || 0} 更新 / ${data.edges_created || 0} 边`,
                data,
            ),
            'success',
        );
    }
    loadProjectFacts();
    if (currentProjectTab === 'graph') loadProjectFactGraph();
}

async function unbindConversationFromProject(conversationId) {
    if (!conversationId || !confirm(tp('projects.confirmUnbindConversation'))) return;
    const res = await apiFetch(`/api/conversations/${encodeURIComponent(conversationId)}/project`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ projectId: '' }),
    });
    if (!res.ok) return alert(tp('projects.unbindFailed'));
    loadProjectConversations();
    refreshProjectHeaderStats();
}

let _factDetailKey = null;
let _factDetailFact = null;
let _projectFactsFilterDebounce = null;

async function viewProjectFactBody(factKey) {
    document.getElementById('fact-detail-title').textContent = factKey;
    document.getElementById('fact-detail-meta').textContent = '…';
    document.getElementById('fact-detail-body').textContent = '';
    const warnEl = document.getElementById('fact-detail-sparse-warn');
    if (warnEl) {
        warnEl.hidden = true;
        warnEl.textContent = '';
    }
    const linkBtn = document.getElementById('fact-detail-link-vuln-btn');
    const createBtn = document.getElementById('fact-detail-create-vuln-btn');
    if (linkBtn) linkBtn.hidden = true;
    if (createBtn) createBtn.hidden = true;
    openProjectsOverlay('fact-detail-modal', { focus: false });
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts?fact_key=${encodeURIComponent(factKey)}`);
    if (!res.ok) {
        closeFactDetailModal();
        return alert(tp('common.loadFailed'));
    }
    const f = await res.json();
    _factDetailKey = f.fact_key;
    _factDetailFact = f;
    deferModalContent(() => {
        document.getElementById('fact-detail-title').textContent = `[${f.fact_key}]`;
        const metaParts = [
            tpFmt('projects.factMetaCategory', `Category: ${f.category}`, { value: f.category }),
            tpFmt('projects.factMetaConfidence', `Confidence: ${f.confidence}`, { value: f.confidence }),
            tpFmt('projects.factMetaUpdated', `Updated: ${formatProjectTime(f.updated_at, f.created_at)}`, {
                time: formatProjectTime(f.updated_at, f.created_at),
            }),
        ];
        if (f.related_vulnerability_id) metaParts.push(tpFmt('projects.factMetaRelatedVuln', `Related vulnerability: ${f.related_vulnerability_id}`, { value: f.related_vulnerability_id }));
        if (f.source_conversation_id) metaParts.push(tpFmt('projects.factMetaSourceConversation', `Source conversation: ${f.source_conversation_id}`, { value: f.source_conversation_id }));
        document.getElementById('fact-detail-meta').textContent = metaParts.join(' · ');
        document.getElementById('fact-detail-body').textContent = f.body || tp('projects.emptyBody');
        if (warnEl) {
            if (isSparseFactBody(f.category, f.fact_key, f.body)) {
                warnEl.hidden = false;
                warnEl.textContent = tp('projects.factSparseWarn');
            } else {
                warnEl.hidden = true;
                warnEl.textContent = '';
            }
        }
        if (linkBtn) linkBtn.hidden = false;
        if (createBtn) createBtn.hidden = false;
    });
}

function editFactFromDetail() {
    const key = _factDetailKey;
    closeFactDetailModal();
    if (key) showEditFactModal(key);
}

function closeFactDetailModal() {
    closeProjectsOverlay('fact-detail-modal');
    _factDetailKey = null;
    _factDetailFact = null;
}

async function linkFactToExistingVulnerability() {
    const f = _factDetailFact;
    if (!f || !currentProjectId) return;
    const res = await apiFetch(`/api/vulnerabilities?project_id=${encodeURIComponent(currentProjectId)}&limit=50`);
    if (!res.ok) return alert(tp('projects.loadVulnerabilityListFailed'));
    const data = await res.json();
    const items = data.Vulnerabilities || data.vulnerabilities || data.items || [];
    if (!items.length) return alert(tp('projects.noVulnerabilitiesInProject'));
    const lines = items.map((v, i) => `${i + 1}. [${v.severity}] ${v.title} (${v.id})`);
    const pick = prompt(
        tp('projects.promptLinkFactToVuln', {
            factKey: f.fact_key,
            lines: lines.join('\n'),
            interpolation: { escapeValue: false },
        }) || `Enter index to link fact "${f.fact_key}":\n\n${lines.join('\n')}`,
    );
    if (pick == null || pick === '') return;
    const idx = parseInt(pick, 10) - 1;
    if (Number.isNaN(idx) || idx < 0 || idx >= items.length) return alert(tp('projects.invalidIndex'));
    const vulnId = items[idx].id;
    const upd = await apiFetch(`/api/projects/${currentProjectId}/facts/${encodeURIComponent(f.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            fact_key: f.fact_key,
            category: f.category,
            summary: f.summary,
            body: f.body || '',
            confidence: f.confidence,
            related_vulnerability_id: vulnId,
        }),
    });
    if (!upd.ok) return alert(tp('projects.linkFailed'));
    alert(tp('projects.linkSuccess'));
    closeFactDetailModal();
    loadProjectFacts();
}

async function createVulnerabilityFromCurrentFact() {
    const f = _factDetailFact;
    if (!f || !currentProjectId) return;
    if (typeof requirePermission === 'function' && !requirePermission('vulnerability:write')) return;
    let convId =
        (f.source_conversation_id || '').trim() ||
        (typeof window.currentConversationId === 'string' ? window.currentConversationId.trim() : '');
    if (!convId) {
        convId = prompt(tp('projects.promptConversationIdForVulnCreate'), '')?.trim() || '';
    }
    if (!convId) return alert(tp('projects.cancelledNoConversationId'));
    const severity = inferSeverityFromFact(f);
    const body = {
        conversation_id: convId,
        project_id: currentProjectId,
        title: (f.summary || f.fact_key).slice(0, 200),
        description:
            tp('projects.generatedFromFact', {
                factKey: f.fact_key,
                interpolation: { escapeValue: false },
            }) || `Generated from project fact ${f.fact_key}`,
        severity,
        status: 'open',
        type: f.category || 'finding',
        target: '',
        proof: f.body || '',
        impact: '',
        recommendation: '',
    };
    const res = await apiFetch('/api/vulnerabilities', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });
    if (!(await notifyProjectApiFailure(res, 'projects.createVulnerabilityFailed', '创建漏洞失败'))) return;
    const vuln = await res.json();
    const upd = await apiFetch(`/api/projects/${currentProjectId}/facts/${encodeURIComponent(f.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            fact_key: f.fact_key,
            category: f.category,
            summary: f.summary,
            body: f.body || '',
            confidence: f.confidence,
            related_vulnerability_id: vuln.id,
        }),
    });
    if (!(await notifyProjectApiFailure(upd, 'projects.linkFailed', '关联失败'))) return;
    const createdVulnLabel = vuln.title || vuln.id;
    const successMsg = tp('projects.createVulnerabilityAndLinkSuccess', {
        value: createdVulnLabel,
        interpolation: { escapeValue: false },
    });
    alert(successMsg || `Created and linked vulnerability: ${createdVulnLabel}`);
    closeFactDetailModal();
    loadProjectFacts();
    if (currentProjectTab === 'vulns') loadProjectVulnerabilities();
}

function inferSeverityFromFact(f) {
    const c = (f.category || '').toLowerCase();
    const key = (f.fact_key || '').toLowerCase();
    if (c === 'exploit' || c === 'poc' || key.includes('rce') || key.includes('sqli')) return 'high';
    if (c === 'finding' || c === 'chain') return 'medium';
    return 'medium';
}

async function deprecateProjectFactByKey(factKey) {
    if (!requireProjectWrite()) return;
    if (!confirm(
        tp('projects.confirmDeprecateFact', {
            factKey,
            interpolation: { escapeValue: false },
        }) || `Deprecate fact ${factKey}?`,
    )) return;
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts/deprecate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fact_key: factKey }),
    });
    if (!(await notifyProjectApiFailure(res, 'projects.operationFailed', '操作失败'))) return;
    loadProjectFacts();
}

async function restoreProjectFactByKey(factKey) {
    if (!requireProjectWrite()) return;
    if (!confirm(
        tp('projects.confirmRestoreFact', {
            factKey,
            interpolation: { escapeValue: false },
        }) || `Restore fact ${factKey}? It will re-enter the board index with tentative status.`,
    )) return;
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts/restore`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fact_key: factKey, confidence: 'tentative' }),
    });
    if (!(await notifyProjectApiFailure(res, 'projects.operationFailed', '操作失败'))) return;
    loadProjectFacts();
}

function openVulnerabilitiesForProject(projectId) {
    const pid = projectId || currentProjectId;
    if (!pid) return;
    if (typeof switchPage === 'function') {
        switchPage('vulnerabilities');
    }
    if (typeof window.setVulnerabilityProjectFilter === 'function') {
        window.setVulnerabilityProjectFilter(pid);
    } else {
        window.location.hash = `vulnerabilities?project_id=${encodeURIComponent(pid)}`;
    }
}

async function loadProjectVulnerabilities() {
    const tbody = document.getElementById('project-vulns-tbody');
    if (!tbody || !currentProjectId) return;
    tbody.innerHTML = `<tr class="is-empty-row"><td colspan="4">${escapeHtml(tp('common.loading'))}</td></tr>`;
    const qs = buildProjectVulnsQueryParams().toString();
    const res = await apiFetch(`/api/vulnerabilities?${qs}`);
    if (!res.ok) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="4">${escapeHtml(tp('common.loadFailed'))}</td></tr>`;
        return;
    }
    const data = await res.json();
    const items = data.Vulnerabilities || data.vulnerabilities || data.items || (Array.isArray(data) ? data : []);
    if (!items.length) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="4">${
            projectVulnsHasActiveFilter() ? tp('projects.noMatchingVulns') : tp('projects.noVulnerabilityRecords')
        }</td></tr>`;
        refreshProjectHeaderStats();
        return;
    }
    tbody.innerHTML = items.map((v) => {
        const idEsc = escapeHtml(v.id);
        return `<tr>
            <td class="cell-summary" title="${escapeHtml(v.title)}">${escapeHtml(v.title)}</td>
            <td>${formatSeverityBadge(v.severity)}</td>
            <td>${formatVulnStatusBadge(v.status)}</td>
            <td class="col-actions">
                <div class="projects-table-actions">
                    <button type="button" class="projects-action-btn projects-action-btn--view" data-vuln-id="${idEsc}" onclick="openVulnerabilityDetail(this.dataset.vulnId)">${escapeHtml(tp('common.view'))}</button>
                    <button type="button" class="projects-action-btn projects-action-btn--view" data-vuln-id="${idEsc}" onclick="viewFactsForVulnerability(this.dataset.vulnId)" title="${escapeHtml(tp('projects.viewRelatedFactsTitle'))}">${escapeHtml(tp('projects.facts'))}</button>
                </div>
            </td>
        </tr>`;
    }).join('');
    refreshProjectHeaderStats();
}

function openVulnerabilityDetail(vulnId) {
    openVulnerabilitiesForProject(currentProjectId);
    if (typeof window.setVulnerabilityIdFilter === 'function') {
        setTimeout(() => window.setVulnerabilityIdFilter(vulnId), 300);
    }
}

async function viewFactsForVulnerability(vulnId) {
    if (!currentProjectId) return;
    switchProjectTab('facts');
    const searchEl = document.getElementById('project-facts-search');
    const catEl = document.getElementById('project-facts-filter-category');
    const confEl = document.getElementById('project-facts-filter-confidence');
    const sparseEl = document.getElementById('project-facts-filter-sparse');
    const hideDepEl = document.getElementById('project-facts-filter-hide-deprecated');
    if (searchEl) searchEl.value = '';
    if (catEl) catEl.value = '';
    if (confEl) confEl.value = '';
    if (sparseEl) sparseEl.checked = false;
    if (hideDepEl) hideDepEl.checked = true;
    const params = new URLSearchParams({ limit: '50', related_vulnerability_id: vulnId });
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts?${params}`);
    if (!res.ok) return alert(tp('projects.loadRelatedFactsFailed'));
    const facts = await res.json();
    if (!facts.length) {
        alert(tp('projects.noFactsForVulnerability'));
        loadProjectFacts();
        return;
    }
    if (facts.length === 1) {
        viewProjectFactBody(facts[0].fact_key);
        return;
    }
    const pick = prompt(
        tp('projects.promptChooseFactByIndex', {
            count: facts.length,
            lines: facts.map((f, i) => `${i + 1}. ${f.fact_key}`).join('\n'),
            interpolation: { escapeValue: false },
        }) || `This vulnerability is linked to ${facts.length} facts. Enter index to view:\n${facts.map((f, i) => `${i + 1}. ${f.fact_key}`).join('\n')}`,
    );
    if (pick == null || pick === '') {
        loadProjectFacts();
        return;
    }
    const idx = parseInt(pick, 10) - 1;
    if (facts[idx]) viewProjectFactBody(facts[idx].fact_key);
    else loadProjectFacts();
}

function openProjectsOverlay(id, opts) {
    openAppModal(id, opts);
}

function isProjectsOverlayVisible(id) {
    return isAppModalOpen(id);
}

function closeProjectsOverlay(id) {
    closeAppModal(id);
}

function showNewProjectModal() {
    if (!requireProjectWrite()) return;
    document.getElementById('project-modal-title').textContent = tp('projects.modalNewTitle');
    const sub = document.getElementById('project-modal-subtitle');
    if (sub) sub.textContent = tp('projects.modalNewSubtitle');
    const submitBtn = document.getElementById('project-modal-submit-btn');
    if (submitBtn) submitBtn.textContent = tp('projects.createProject');
    document.getElementById('project-modal-name').value = '';
    document.getElementById('project-modal-description').value = '';
    window._projectModalEditId = null;
    openProjectsOverlay('project-modal');
}

async function showEditProjectModal(projectId, options = {}) {
    if (!projectId) return;
    if (!requireProjectWrite()) return;
    window._projectModalFromChat = false;
    window._projectModalFromChatSidebar = options.fromChatSidebar === true;
    window._projectModalEditId = projectId;
    document.getElementById('project-modal-title').textContent = tp('projects.modalEditTitle');
    const sub = document.getElementById('project-modal-subtitle');
    if (sub) sub.textContent = tp('projects.modalEditSubtitle');
    const submitBtn = document.getElementById('project-modal-submit-btn');
    if (submitBtn) submitBtn.textContent = tp('projects.saveChanges');
    const nameEl = document.getElementById('project-modal-name');
    const descEl = document.getElementById('project-modal-description');
    if (nameEl) nameEl.value = '';
    if (descEl) descEl.value = '';
    openProjectsOverlay('project-modal', { focus: false });
    let p = findProjectById(projectId);
    if (!p) {
        try {
            const res = await apiFetch(`/api/projects/${encodeURIComponent(projectId)}`);
            if (!res.ok) throw new Error(tp('projects.projectNotFound'));
            p = await res.json();
        } catch (e) {
            closeProjectModal();
            alert(e.message || tp('projects.projectNotFound'));
            window._projectModalEditId = null;
            return;
        }
    }
    const name = (p.name || '').slice(0, PROJECT_NAME_MAX_LENGTH);
    const description = clampProjectDescription(p.description || '');
    deferModalContent(() => {
        if (nameEl) nameEl.value = name;
        if (descEl) descEl.value = description;
        nameEl?.focus();
    });
}

/** 从对话区「选择项目」面板打开新建项目，创建成功后自动绑定当前对话 */
function showNewProjectModalFromChat() {
    closeChatProjectPanel();
    window._projectModalFromChat = true;
    showNewProjectModal();
}

/** 从对话侧栏新建项目，保持当前对话的项目绑定不变。 */
function showNewProjectModalFromChatSidebar() {
    if (!requireProjectWrite()) return;
    window._projectModalFromChat = false;
    window._projectModalFromChatSidebar = true;
    showNewProjectModal();
}

async function saveProjectModal() {
    if (!requireProjectWrite()) return;
    const name = document.getElementById('project-modal-name').value.trim().slice(0, PROJECT_NAME_MAX_LENGTH);
    if (!name) return alert(tp('projects.enterProjectName'));
    const body = {
        name,
        description: clampProjectDescription(document.getElementById('project-modal-description').value),
    };
    const editId = window._projectModalEditId;
    const submitBtn = document.getElementById('project-modal-submit-btn');
    if (submitBtn) submitBtn.disabled = true;
    try {
        const res = editId
            ? await apiFetch(`/api/projects/${editId}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
            : await apiFetch('/api/projects', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        if (!(await notifyProjectApiFailure(res, 'projects.saveFailed', '保存失败'))) return;
        const fromChat = !!window._projectModalFromChat;
        const fromChatSidebar = !!window._projectModalFromChatSidebar;
        const fromWebshellConnId = window._projectModalFromWebshellConnId || '';
        window._projectModalFromChat = false;
        window._projectModalFromChatSidebar = false;
        window._projectModalFromWebshellConnId = '';
        closeProjectModal();
        const saved = await res.json();
        await loadProjectsList();
        if (saved.id) {
            if (fromWebshellConnId && !editId) {
                if (typeof applyWebshellAiProjectSelection === 'function') {
                    await applyWebshellAiProjectSelection(saved.id);
                }
            } else if (fromChat && !editId) {
                await applyChatProjectSelection(saved.id);
            } else if (!fromChatSidebar) {
                await selectProject(saved.id);
            }
        }
    } catch (error) {
        if (typeof notifyApiError === 'function') {
            notifyApiError(error?.message || tpFmt('projects.saveFailed', '保存失败'));
        } else {
            alert(error?.message || tpFmt('projects.saveFailed', '保存失败'));
        }
    } finally {
        if (submitBtn) submitBtn.disabled = false;
    }
}

function closeProjectModal() {
    window._projectModalFromChat = false;
    window._projectModalFromChatSidebar = false;
    window._projectModalEditId = null;
    closeProjectsOverlay('project-modal');
}

function formatProjectScopeJson() {
    const el = document.getElementById('project-edit-scope');
    if (!el) return;
    const raw = el.value.trim();
    if (!raw) return;
    try {
        el.value = JSON.stringify(JSON.parse(raw), null, 2);
    } catch (e) {
        alert(tp('projects.invalidJson') + ': ' + (e.message || String(e)));
    }
}

function insertProjectScopeExample() {
    const el = document.getElementById('project-edit-scope');
    if (!el) return;
    const example = {
        targets: ['https://example.com'],
        exclude: ['*.cdn.example.com'],
        notes: tp('projects.scopeNoteAuthorizedWebOnly'),
    };
    el.value = JSON.stringify(example, null, 2);
    el.focus();
}

async function saveProjectSettings() {
    if (!currentProjectId || !requireProjectWrite()) return;
    const scopeRaw = document.getElementById('project-edit-scope').value.trim();
    if (scopeRaw) {
        try {
            JSON.parse(scopeRaw);
        } catch (e) {
            alert(tp('projects.invalidScopeJson') + ': ' + (e.message || String(e)));
            return;
        }
    }
    const body = {
        name: document.getElementById('project-edit-name').value.trim(),
        description: clampProjectDescription(document.getElementById('project-edit-description').value),
        scope_json: scopeRaw,
        status: document.getElementById('project-edit-status')?.value || 'active',
        pinned: !!document.getElementById('project-edit-pinned')?.checked,
    };
    const res = await apiFetch(`/api/projects/${currentProjectId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });
    if (!(await notifyProjectApiFailure(res, 'projects.saveFailed', '保存失败'))) return;
    await loadProjectsList();
    await selectProject(currentProjectId);
    if (typeof notifyApiError === 'function') {
        notifyApiError(tp('projects.saved'), 'success');
    } else {
        alert(tp('projects.saved'));
    }
}

function findProjectById(projectId) {
    return projectsCache.find((p) => p.id === projectId) || projectsCacheAll.find((p) => p.id === projectId);
}

let _projectListMenuTargetId = null;
let _projectListMenuSource = '';
let _projectListMenuDocClickBound = false;

function closeProjectListActionMenu() {
    const menu = document.getElementById('projects-list-action-menu');
    if (!menu) return;
    menu.style.display = 'none';
    _projectListMenuTargetId = null;
    _projectListMenuSource = '';
}

function positionProjectListActionMenu(event) {
    const menu = document.getElementById('projects-list-action-menu');
    if (!menu) return;
    menu.style.display = 'block';
    menu.style.visibility = 'visible';
    menu.style.opacity = '1';
    void menu.offsetHeight;
    const menuRect = menu.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    let left = event.clientX;
    let top = event.clientY;
    if (left + menuRect.width > viewportWidth) {
        left = Math.max(8, event.clientX - menuRect.width);
    }
    if (top + menuRect.height > viewportHeight) {
        top = Math.max(8, event.clientY - menuRect.height);
    }
    menu.style.left = `${left}px`;
    menu.style.top = `${top}px`;
}

function showProjectListActionMenu(event, projectId, source = '') {
    event.stopPropagation();
    event.preventDefault();
    const menu = document.getElementById('projects-list-action-menu');
    if (!menu) return;
    if (_projectListMenuTargetId === projectId && menu.style.display === 'block') {
        closeProjectListActionMenu();
        return;
    }
    closeProjectListActionMenu();
    const p = findProjectById(projectId);
    if (!p) return;
    _projectListMenuTargetId = projectId;
    _projectListMenuSource = source;
    const editText = document.getElementById('projects-list-menu-edit-text');
    const pinText = document.getElementById('projects-list-menu-pin-text');
    const archiveText = document.getElementById('projects-list-menu-archive-text');
    const deleteText = document.getElementById('projects-list-menu-delete-text');
    if (editText) {
        editText.textContent = source === 'chat'
            ? pickerMessage(tp, 'projects.renameProject', '重命名')
            : tp('projects.editProject');
    }
    if (pinText) {
        pinText.textContent = p.pinned
            ? pickerMessage(tp, 'projects.unpinProjectAction', '取消置顶')
            : pickerMessage(tp, 'projects.pinProjectAction', '置顶项目');
    }
    if (archiveText) {
        archiveText.textContent = p.status === 'archived'
            ? tp('projects.restoreProjectActive')
            : tp('projects.archiveProject');
    }
    if (deleteText) deleteText.textContent = tp('projects.deleteProject');
    positionProjectListActionMenu(event);
    if (typeof applyRBACToUI === 'function') applyRBACToUI(menu);
}

function updateCachedProjectPinnedState(projectId, pinned) {
    const update = (project) => {
        if (project?.id === projectId) project.pinned = !!pinned;
        return project;
    };
    projectsCache = sortProjectsForPicker(projectsCache.map(update));
    projectsCacheAll = sortProjectsForPicker(projectsCacheAll.map(update));
    renderProjectsSidebar();
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    }
}

async function toggleProjectPinnedFromListMenu() {
    if (!requireProjectWrite()) return;
    const projectId = _projectListMenuTargetId;
    const project = findProjectById(projectId);
    closeProjectListActionMenu();
    if (!projectId || !project) return;

    const nextPinned = !project.pinned;
    const res = await apiFetch(`/api/projects/${projectId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pinned: nextPinned }),
    });
    if (!(await notifyProjectApiFailure(res, 'projects.operationFailed', '操作失败'))) return;

    updateCachedProjectPinnedState(projectId, nextPinned);
    await loadProjectsList();
}

function initProjectListActionMenu() {
    if (_projectListMenuDocClickBound) return;
    _projectListMenuDocClickBound = true;
    document.addEventListener('click', (event) => {
        const menu = document.getElementById('projects-list-action-menu');
        if (!menu || menu.style.display === 'none') return;
        if (menu.contains(event.target)) return;
        if (event.target.closest('.projects-list-item-menu, .project-folder-menu')) return;
        closeProjectListActionMenu();
    });
    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') closeProjectListActionMenu();
    });
}

async function toggleProjectArchiveById(projectId) {
    if (!requireProjectWrite()) return;
    const p = findProjectById(projectId);
    if (!p) return;
    const cur = p.status || 'active';
    const next = cur === 'archived' ? 'active' : 'archived';
    if (!confirm(next === 'archived' ? tp('projects.confirmArchiveProject') : tp('projects.confirmRestoreProjectActive'))) return;
    const res = await apiFetch(`/api/projects/${projectId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: next }),
    });
    if (!(await notifyProjectApiFailure(res, 'projects.operationFailed', '操作失败'))) return;
    await loadProjectsList();
    if (currentProjectId === projectId && projectsCache.some((item) => item.id === projectId)) {
        await selectProject(projectId);
    } else if (currentProjectId === projectId) {
        currentProjectId = null;
        updateProjectsDetailVisibility();
        if (projectsCache.length) await selectProject(projectsCache[0].id);
    }
}

async function deleteProjectById(projectId) {
    if (!requireProjectDelete()) return;
    if (!projectId || !confirm(tp('projects.confirmDeleteProject'))) return;
    const deletedIndex = projectsCache.findIndex((p) => p.id === projectId);
    const res = await apiFetch(`/api/projects/${projectId}`, { method: 'DELETE' });
    if (!(await notifyProjectApiFailure(res, 'projects.deleteFailed', '删除失败'))) return;
    if (getActiveProjectId() === projectId) setActiveProjectId('');
    if (currentProjectId === projectId) currentProjectId = null;
    await loadProjectsList();
    if (projectsCache.length) {
        const nextIndex = Math.min(deletedIndex >= 0 ? deletedIndex : 0, projectsCache.length - 1);
        await selectProject(projectsCache[nextIndex].id);
    } else {
        updateProjectsDetailVisibility();
    }
}

async function toggleProjectArchiveFromListMenu() {
    const projectId = _projectListMenuTargetId;
    closeProjectListActionMenu();
    if (!projectId) return;
    await toggleProjectArchiveById(projectId);
}

function editProjectFromListMenu() {
    const projectId = _projectListMenuTargetId;
    const fromChatSidebar = _projectListMenuSource === 'chat';
    closeProjectListActionMenu();
    if (!projectId) return;
    showEditProjectModal(projectId, { fromChatSidebar });
}

async function deleteProjectFromListMenu() {
    const projectId = _projectListMenuTargetId;
    closeProjectListActionMenu();
    if (!projectId) return;
    await deleteProjectById(projectId);
}

async function archiveCurrentProject() {
    if (!currentProjectId) return;
    await toggleProjectArchiveById(currentProjectId);
}

async function deleteCurrentProject() {
    if (!currentProjectId) return;
    await deleteProjectById(currentProjectId);
}

function resetFactModalForm() {
    window._factModalEditId = null;
    const keyEl = document.getElementById('fact-modal-key');
    if (keyEl) keyEl.disabled = false;
    document.getElementById('fact-modal-title').textContent = tp('projects.addFact');
    document.getElementById('fact-modal-submit-btn').textContent = tp('projects.saveFact');
    document.getElementById('fact-modal-key').value = '';
    document.getElementById('fact-modal-category').value = 'note';
    document.getElementById('fact-modal-summary').value = '';
    document.getElementById('fact-modal-body').value = '';
    document.getElementById('fact-modal-confidence').value = 'tentative';
    const pinEl = document.getElementById('fact-modal-pinned');
    if (pinEl) pinEl.checked = false;
    const rel = document.getElementById('fact-modal-related-vuln');
    if (rel) rel.value = '';
    const linksEl = document.getElementById('fact-modal-links');
    if (linksEl) linksEl.value = '';
    const incomingWrap = document.getElementById('fact-modal-incoming-links-wrap');
    if (incomingWrap) incomingWrap.hidden = true;
    updateFactFormHints();
}

function fillFactModalForm(f) {
    window._factModalEditId = f.id;
    document.getElementById('fact-modal-title').textContent = tp('projects.editFact');
    document.getElementById('fact-modal-submit-btn').textContent = tp('projects.saveChanges');
    document.getElementById('fact-modal-key').value = f.fact_key || '';
    const catEl = document.getElementById('fact-modal-category');
    const cat = (f.category || 'note').trim().toLowerCase();
    if (catEl) {
        const known = Array.from(catEl.options).some((o) => o.value === cat);
        if (known) catEl.value = cat;
        else {
            const opt = document.createElement('option');
            opt.value = f.category;
            opt.textContent = tpFmt('projects.customCategoryOption', `${f.category} (custom)`, { value: f.category });
            catEl.appendChild(opt);
            catEl.value = f.category;
        }
    }
    document.getElementById('fact-modal-summary').value = f.summary || '';
    document.getElementById('fact-modal-body').value = f.body || '';
    const conf = (f.confidence || 'tentative').toLowerCase();
    const confEl = document.getElementById('fact-modal-confidence');
    if (confEl) {
        const allowed = ['tentative', 'confirmed', 'deprecated'];
        confEl.value = allowed.includes(conf) ? conf : 'tentative';
    }
    const rel = document.getElementById('fact-modal-related-vuln');
    if (rel) rel.value = f.related_vulnerability_id || '';
    const linksEl = document.getElementById('fact-modal-links');
    if (linksEl) linksEl.value = formatIncomingLinksForModal(f.incoming_links);
    const pinEl = document.getElementById('fact-modal-pinned');
    if (pinEl) pinEl.checked = !!f.pinned;
    updateFactFormHints();
}

function showAddFactModal() {
    if (!currentProjectId) return alert(tp('projects.selectProjectFirst'));
    if (!requireProjectWrite()) return;
    resetFactModalForm();
    openProjectsOverlay('fact-modal');
}

async function showEditFactModal(factKey) {
    if (!currentProjectId) return alert(tp('projects.selectProjectFirst'));
    if (!requireProjectWrite()) return;
    resetFactModalForm();
    openProjectsOverlay('fact-modal', { focus: false });
    const res = await apiFetch(
        `/api/projects/${currentProjectId}/facts?fact_key=${encodeURIComponent(factKey)}&include_links=true`,
    );
    if (!res.ok) {
        closeFactModal();
        return alert(tp('projects.loadFactFailed'));
    }
    const f = await res.json();
    deferModalContent(() => {
        fillFactModalForm(f);
        document.getElementById('fact-modal-key')?.focus();
    });
}

function closeFactModal() {
    closeProjectsOverlay('fact-modal');
    resetFactModalForm();
}

async function saveFactModal() {
    if (!requireProjectWrite()) return;
    const fact_key = document.getElementById('fact-modal-key').value.trim();
    const summary = document.getElementById('fact-modal-summary').value.trim();
    const category = document.getElementById('fact-modal-category').value.trim() || 'note';
    const body = document.getElementById('fact-modal-body').value;
    if (!fact_key || !summary) return alert(tp('projects.factKeySummaryRequired'));
    if (isSparseFactBody(category, fact_key, body)) {
        const ok = confirm(
            tp('projects.confirmSaveSparseFact'),
        );
        if (!ok) return;
    }
    const payload = {
        fact_key,
        category,
        summary,
        body,
        confidence: document.getElementById('fact-modal-confidence').value,
        pinned: !!document.getElementById('fact-modal-pinned')?.checked,
        related_vulnerability_id: document.getElementById('fact-modal-related-vuln')?.value?.trim() || '',
        links_text: document.getElementById('fact-modal-links')?.value || '',
    };
    const editId = window._factModalEditId;
    const res = editId
        ? await apiFetch(`/api/projects/${currentProjectId}/facts/${editId}`, {
              method: 'PUT',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(payload),
          })
        : await apiFetch(`/api/projects/${currentProjectId}/facts`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(payload),
          });
    if (!(await notifyProjectApiFailure(res, 'projects.saveFailed', '保存失败'))) return;
    closeFactModal();
    loadProjectFacts();
    if (currentProjectTab === 'graph') loadProjectFactGraph();
}

async function deleteProjectFact(id) {
    if (!requireProjectWrite()) return;
    if (!confirm(tp('projects.confirmDeleteFact'))) return;
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts/${id}`, { method: 'DELETE' });
    if (!(await notifyProjectApiFailure(res, 'projects.operationFailed', '操作失败'))) return;
    loadProjectFacts();
    if (currentProjectTab === 'graph') loadProjectFactGraph();
}

function parseProjectDate(t) {
    if (t == null || t === '') return null;
    if (typeof t === 'number' && Number.isFinite(t)) {
        const d = new Date(t);
        return isNaN(d.getTime()) || d.getFullYear() < 2000 ? null : d;
    }
    let s = String(t).trim();
    if (!s || s.startsWith('0001-01-01')) return null;
    let d = new Date(s);
    if (!isNaN(d.getTime()) && d.getFullYear() >= 2000) return d;
    const m = s.match(
        /^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(?:([Zz]|([+-])(\d{2}):?(\d{2}))?)?$/,
    );
    if (m) {
        const ms = m[7] ? parseInt(String(m[7]).slice(0, 3).padEnd(3, '0'), 10) : 0;
        let offMin = 0;
        if (m[8] && m[9] && m[10]) {
            offMin = parseInt(m[10], 10) * 60 + parseInt(m[11] || '0', 10);
            if (m[9] === '-') offMin = -offMin;
        }
        d = new Date(
            Date.UTC(
                parseInt(m[1], 10),
                parseInt(m[2], 10) - 1,
                parseInt(m[3], 10),
                parseInt(m[4], 10),
                parseInt(m[5], 10),
                parseInt(m[6], 10),
                ms,
            ) - offMin * 60 * 1000,
        );
        if (!isNaN(d.getTime()) && d.getFullYear() >= 2000) return d;
    }
    return null;
}

function formatProjectTime(t, fallback) {
    const d = parseProjectDate(t) || (fallback != null ? parseProjectDate(fallback) : null);
    if (!d) return tp('projects.notUpdatedYet');
    const now = Date.now();
    const diff = now - d.getTime();
    if (diff < 60000) return tp('common.justNow');
    if (diff < 3600000) return tp('common.minutesAgo', { n: Math.floor(diff / 60000) });
    if (diff < 86400000) return tp('common.hoursAgo', { n: Math.floor(diff / 3600000) });
    if (diff < 604800000) return tp('common.daysAgo', { n: Math.floor(diff / 86400000) });
    return d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function escapeHtml(s) {
    if (s == null) return '';
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function escapeAttr(s) {
    return escapeHtml(s).replace(/'/g, '&#39;');
}

function escapeJsString(text) {
    return JSON.stringify(String(text == null ? '' : text));
}

function escapeJsStringAttr(text) {
    return escapeAttr(escapeJsString(text));
}

function getChatProjectSelection() {
    const convId = window.currentConversationId;
    if (convId) {
        return window._loadedConversationProjectId || '';
    }
    return getActiveProjectId();
}

/** 用于 UI：返回当前选中的项目 ID（有效性由 normalizeStaleChatProjectSelection 异步校验） */
function resolveChatProjectSelection() {
    return getChatProjectSelection() || '';
}

let _normalizingStaleProject = false;

/** 清除 localStorage 或对话上残留的失效项目 ID */
async function normalizeStaleChatProjectSelection() {
    if (_normalizingStaleProject) return;
    const raw = (getChatProjectSelection() || '').trim();
    if (!raw) return;
    const project = await fetchProjectSummary(raw);
    if (project && project.id && project.status !== 'archived') return;

    _normalizingStaleProject = true;
    try {
        if (window.currentConversationId) {
            window._loadedConversationProjectId = '';
            try {
                const res = await apiFetch(
                    `/api/conversations/${encodeURIComponent(window.currentConversationId)}/project`,
                    {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ projectId: '' }),
                    }
                );
                if (!res.ok) console.warn(tp('projects.clearStaleProjectBindingFailed'));
            } catch (e) {
                console.warn(e);
            }
        } else {
            setActiveProjectId('');
        }
    } finally {
        _normalizingStaleProject = false;
    }
}

const PROJECT_PICKER_DEBOUNCE_MS = 100;
const projectPickerPanelState = {
    chat: { seq: 0, timer: null },
    webshell: { seq: 0, timer: null },
};

let chatProjectFolderSearchQuery = '';
let chatProjectFolderRenderSeq = 0;
let chatProjectFolderContextLoadSeq = 0;
const CHAT_PROJECT_FOLDER_PAGE_SIZE = 6;
let chatProjectFolderVisibleCount = CHAT_PROJECT_FOLDER_PAGE_SIZE;
let chatProjectFolderLastQuery = '';
const chatProjectFolderExpandedIds = new Set();
let chatProjectFolderLastSelectionId = null;
const CHAT_UNASSIGNED_PROJECT_FOLDER_ID = '__chat_unassigned_project__';
const PROJECT_FOLDER_COMPLETION_SEEN_KEY = 'cyberstrike-project-folder-completion-seen';
const chatProjectFolderContext = {
    ready: false,
    conversations: [],
    runningIds: new Set(),
    completedByConversation: new Map(),
    pendingApprovalByConversation: new Map(),
};
const PROJECT_FOLDER_PREVIEW_OPEN_DELAY_MS = 160;
const PROJECT_FOLDER_PREVIEW_CLOSE_DELAY_MS = 120;
const PROJECT_APPROVAL_TICK_INTERVAL_MS = 1000;
const projectApprovalTickerEntries = new Set();
let projectApprovalTickerId = 0;
let projectFolderPreviewOpenTimer = null;
let projectFolderPreviewCloseTimer = null;
let projectFolderPreviewAnchor = null;
let projectConversationPreviewOpenTimer = null;
let projectConversationPreviewCloseTimer = null;
let projectConversationPreviewAnchor = null;
let projectConversationPreviewSuppressedUntil = 0;

function readProjectFolderCompletionSeen() {
    try {
        const parsed = JSON.parse(localStorage.getItem(PROJECT_FOLDER_COMPLETION_SEEN_KEY) || '{}');
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch (e) {
        return {};
    }
}

function markProjectConversationViewed(conversationId, completedAt) {
    const id = String(conversationId || '').trim();
    if (!id) return;
    const seen = readProjectFolderCompletionSeen();
    const timestamp = completedAt || new Date().toISOString();
    seen[id] = timestamp;
    try {
        localStorage.setItem(PROJECT_FOLDER_COMPLETION_SEEN_KEY, JSON.stringify(seen));
    } catch (e) { /* ignore */ }
}

function markCurrentProjectConversationViewed() {
    const conversationId = String(window.currentConversationId || '').trim();
    if (!conversationId || !isProjectConversationUnread(conversationId)) return false;
    const completed = chatProjectFolderContext.completedByConversation.get(conversationId);
    markProjectConversationViewed(conversationId, completed?.completedAt);
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    }
    return true;
}

function initProjectConversationReadTracking() {
    if (window._projectConversationReadTrackingInited) return;
    const chatContainer = document.querySelector('#page-chat .chat-container');
    if (!chatContainer) return;
    window._projectConversationReadTrackingInited = true;
    const markViewed = () => markCurrentProjectConversationViewed();
    chatContainer.addEventListener('pointerdown', markViewed, { passive: true });
    chatContainer.addEventListener('focusin', markViewed);
    document.getElementById('chat-input')?.addEventListener('input', markViewed);
}

function isProjectConversationUnread(conversationId) {
    const completed = chatProjectFolderContext.completedByConversation.get(conversationId);
    if (!completed || String(completed.status || '').toLowerCase() !== 'completed') return false;
    const completedAt = Date.parse(completed.completedAt || '');
    if (!Number.isFinite(completedAt)) return false;
    const seenAt = Date.parse(readProjectFolderCompletionSeen()[conversationId] || '');
    return !Number.isFinite(seenAt) || completedAt > seenAt;
}

function getProjectApprovalTiming(details) {
    if (!details || typeof details !== 'object') return { timeoutSeconds: 0, expiresAt: 0 };
    let payload = details.payload;
    if (typeof payload === 'string') {
        try { payload = JSON.parse(payload); } catch (e) { payload = {}; }
    }
    if (!payload || typeof payload !== 'object') payload = {};
    const approval = payload.hitlApproval && typeof payload.hitlApproval === 'object' ? payload.hitlApproval : {};
    const timeout = Number(details.timeoutSeconds != null ? details.timeoutSeconds : approval.timeoutSeconds);
    const timeoutSeconds = Number.isFinite(timeout) && timeout > 0 ? Math.floor(timeout) : 0;
    const createdAt = Date.parse(details.createdAt || approval.createdAt || '');
    let expiresAt = Date.parse(details.expiresAt || approval.expiresAt || '');
    if (!Number.isFinite(expiresAt) && timeoutSeconds > 0 && Number.isFinite(createdAt)) {
        expiresAt = createdAt + timeoutSeconds * 1000;
    }
    return { timeoutSeconds, expiresAt: Number.isFinite(expiresAt) ? expiresAt : 0 };
}

function formatProjectApprovalRemaining(milliseconds) {
    const seconds = Math.max(0, Math.ceil(milliseconds / 1000));
    const minutes = Math.floor(seconds / 60);
    return minutes + ':' + String(seconds % 60).padStart(2, '0');
}

function registerProjectApprovalTicker(status, update) {
    const entry = { status, update };
    if (update() === false) return;
    projectApprovalTickerEntries.add(entry);
    if (projectApprovalTickerId) return;
    projectApprovalTickerId = window.setInterval(() => {
        projectApprovalTickerEntries.forEach((candidate) => {
            if (!candidate.status.isConnected || candidate.update() === false) {
                projectApprovalTickerEntries.delete(candidate);
            }
        });
        if (!projectApprovalTickerEntries.size) {
            window.clearInterval(projectApprovalTickerId);
            projectApprovalTickerId = 0;
        }
    }, PROJECT_APPROVAL_TICK_INTERVAL_MS);
}

function bindProjectApprovalProgress(status, details) {
    const timing = getProjectApprovalTiming(details);
    if (!timing.timeoutSeconds || !timing.expiresAt) return;
    const time = status.querySelector('.project-approval-time');
    const value = status.querySelector('.project-approval-progress-value');
    const update = () => {
        const remaining = Math.max(0, timing.expiresAt - Date.now());
        const percent = Math.max(0, Math.min(100, remaining / (timing.timeoutSeconds * 1000) * 100));
        if (time) time.textContent = formatProjectApprovalRemaining(remaining);
        if (value) value.style.width = `${percent.toFixed(2)}%`;
        status.setAttribute('aria-valuenow', String(Math.round(percent)));
        if (remaining <= 0) {
            status.classList.add('is-expired');
            return false;
        }
        return true;
    };
    status.setAttribute('role', 'progressbar');
    status.setAttribute('aria-valuemin', '0');
    status.setAttribute('aria-valuemax', '100');
    registerProjectApprovalTicker(status, update);
}

const PROJECT_APPROVAL_URGENCY_CLASSES = [
    'is-urgency-normal',
    'is-urgency-warning',
    'is-urgency-urgent',
    'is-urgency-critical',
];

function projectApprovalUrgencyLevel(remainingMilliseconds, hasDeadline) {
    if (!hasDeadline) return 'normal';
    const remaining = Math.max(0, Number(remainingMilliseconds) || 0);
    if (remaining <= 60 * 1000) return 'critical';
    if (remaining <= 3 * 60 * 1000) return 'warning';
    return 'normal';
}

function getProjectApprovalUrgency(details) {
    const timing = getProjectApprovalTiming(details);
    if (!timing.timeoutSeconds || !timing.expiresAt) {
        return {
            level: 'normal',
            label: pickerMessage(tp, 'hitl.approvalUrgencyUnlimited', '审批不限时'),
            remaining: 0,
        };
    }
    const remaining = Math.max(0, timing.expiresAt - Date.now());
    const level = projectApprovalUrgencyLevel(remaining, true);
    const urgencyLabels = {
        critical: pickerMessage(tp, 'hitl.approvalUrgencyWithinOne', '最早审批将在 1 分钟内到期'),
        warning: pickerMessage(tp, 'hitl.approvalUrgencyOneToThree', '最早审批将在 1–3 分钟内到期'),
        normal: pickerMessage(tp, 'hitl.approvalUrgencyMoreThanThree', '最早审批将在 3 分钟后到期'),
    };
    return {
        level,
        label: urgencyLabels[level],
        remaining,
    };
}

function bindProjectApprovalUrgency(status, details, baseLabel) {
    const timing = getProjectApprovalTiming(details);
    const update = () => {
        const urgency = getProjectApprovalUrgency(details);
        status.classList.remove(...PROJECT_APPROVAL_URGENCY_CLASSES);
        status.classList.add(`is-urgency-${urgency.level}`);
        status.dataset.approvalUrgency = urgency.level;
        status.setAttribute('aria-label', `${baseLabel}，${urgency.label}`);
        status.title = `${baseLabel} · ${urgency.label}`;
        return !(timing.expiresAt && urgency.remaining <= 0);
    };
    if (timing.timeoutSeconds && timing.expiresAt) {
        registerProjectApprovalTicker(status, update);
    } else {
        update();
    }
}

function createProjectTaskStatus(kind, details, options) {
    if (!kind) return null;
    const config = options && typeof options === 'object' ? options : {};
    const isApprovalSummary = kind === 'approval' && config.aggregate === true;
    const approvalCount = Math.max(0, Math.floor(Number(config.count) || 0));
    const status = document.createElement('span');
    status.className = `project-task-status project-task-status--${kind}`;
    const label = isApprovalSummary
        ? tpFmt('hitl.waitingApprovalCount', `等待批准 ${approvalCount}`, { count: approvalCount })
        : (kind === 'approval'
        ? pickerMessage(tp, 'hitl.waitingApprovalShort', '等待批准')
        : (kind === 'running'
            ? pickerMessage(tp, 'tasks.statusRunning', '运行中')
            : pickerMessage(tp, 'chat.completedUnread', '已完成，尚未查看')));
    if (kind === 'approval') {
        const timing = getProjectApprovalTiming(details);
        status.innerHTML = '<span class="project-approval-label"></span>' +
            (!isApprovalSummary && timing.timeoutSeconds && timing.expiresAt
                ? '<span class="project-approval-time"></span><span class="project-approval-progress"><span class="project-approval-progress-value"></span></span>'
                : '');
        status.querySelector('.project-approval-label').textContent = label;
        if (isApprovalSummary) {
            // 项目文件夹只汇总待审批数量，始终使用绿色；
            // 紧急程度仅属于具体对话，避免多个审批让项目颜色来回跳变。
            status.classList.add('project-task-status--approval-summary', 'is-urgency-normal');
            status.dataset.approvalCount = String(approvalCount);
            status.dataset.approvalUrgency = 'normal';
            status.setAttribute('aria-label', label);
            status.title = label;
        } else {
            bindProjectApprovalProgress(status, details);
            bindProjectApprovalUrgency(status, details, label);
        }
    }
    if (!isApprovalSummary) {
        status.setAttribute('aria-label', label);
        status.title = label;
    }
    return status;
}

function appendProjectTaskStatuses(container, kinds, detailsByKind, optionsByKind) {
    const normalizedKinds = Array.from(new Set((Array.isArray(kinds) ? kinds : [kinds]).filter(Boolean)));
    if (!container || !normalizedKinds.length) return;
    const group = document.createElement('span');
    group.className = 'project-task-status-group';
    normalizedKinds.forEach((kind) => {
        const status = createProjectTaskStatus(
            kind,
            detailsByKind && detailsByKind[kind],
            optionsByKind && optionsByKind[kind]
        );
        if (status) group.appendChild(status);
    });
    if (!group.childElementCount) return null;
    container.appendChild(group);
    return group;
}

function getProjectFolderStatuses(conversations) {
    const kinds = [];
    if (conversations.some((conversation) => chatProjectFolderContext.pendingApprovalByConversation.has(conversation.id))) {
        kinds.push('approval');
    }
    if (conversations.some((conversation) => isProjectConversationUnread(conversation.id))) {
        kinds.push('unread');
    }
    if (conversations.some((conversation) => chatProjectFolderContext.runningIds.has(conversation.id))) {
        kinds.push('running');
    }
    return kinds;
}

function projectFolderDisclosureMarkup(isExpanded) {
    const path = isExpanded ? 'M3.5 5.5 7.5 9.5l4-4' : 'm5.5 3.5 4 4-4 4';
    return `<svg width="15" height="15" viewBox="0 0 15 15" fill="none" aria-hidden="true"><path d="${path}" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
}

function projectFolderIconMarkup(isExpanded) {
    // Codex 风格：折叠时为闭合文件夹，展开时露出向前翻开的文件夹盖。
    const path = isExpanded
        ? 'M3.5 18V6.5a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v1.25M3.5 18l1.75-6.25A2 2 0 0 1 7.18 10.3H20.5l-2 7.7H3.5Z'
        : 'M3.5 7a2 2 0 0 1 2-2h4l2 2H18.5a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2V7Zm0 2.5h17';
    return `<svg width="19" height="19" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="${path}" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
}

function clampProjectPreviewText(value, maxLength = 220) {
    const text = String(value || '').replace(/\s+/g, ' ').trim();
    return text.length > maxLength ? `${text.slice(0, maxLength - 1)}…` : text;
}

function getProjectScopePreview(project) {
    const raw = String(project?.scope_json || project?.scopeJson || '').trim();
    if (!raw) return '';
    try {
        const parsed = JSON.parse(raw);
        const values = [];
        const appendValue = (value) => {
            if (typeof value === 'string' || typeof value === 'number') {
                const text = String(value).trim();
                if (text) values.push(text);
            } else if (Array.isArray(value)) {
                value.slice(0, 4).forEach(appendValue);
            }
        };
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            ['targets', 'target', 'domains', 'ips', 'urls', 'scope'].forEach((key) => appendValue(parsed[key]));
            if (!values.length) appendValue(parsed.notes);
        } else {
            appendValue(parsed);
        }
        return clampProjectPreviewText(values.slice(0, 4).join(' · '));
    } catch (e) {
        return clampProjectPreviewText(raw);
    }
}

function ensureProjectFolderPreview() {
    let preview = document.getElementById('project-folder-preview');
    if (preview) return preview;

    preview = document.createElement('div');
    preview.id = 'project-folder-preview';
    preview.className = 'project-folder-preview';
    preview.hidden = true;
    preview.setAttribute('role', 'dialog');
    preview.setAttribute('aria-label', pickerMessage(tp, 'chat.projectPreviewLabel', '项目信息'));
    preview.innerHTML = `
        <div class="project-folder-preview-header">
            <span class="project-folder-preview-icon" aria-hidden="true">
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none"><path d="M3 6.5h6l2 2h10v9.5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round"/></svg>
            </span>
            <span class="project-folder-preview-title"></span>
        </div>
        <div class="project-folder-preview-stats">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><circle cx="12" cy="12" r="8" stroke="currentColor" stroke-width="1.5"/></svg>
            <span></span>
        </div>
        <div class="project-folder-preview-details">
            <div class="project-folder-preview-detail project-folder-preview-description">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true"><circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.7"/><path d="M12 11v5M12 8h.01" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"/></svg>
                <span></span>
            </div>
            <div class="project-folder-preview-detail project-folder-preview-scope">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true"><circle cx="12" cy="12" r="8" stroke="currentColor" stroke-width="1.7"/><circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.7"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/></svg>
                <span></span>
            </div>
        </div>
        <button type="button" class="project-folder-preview-edit" data-require-permission="project:write">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z" stroke="currentColor" stroke-width="1.7"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.83 2.83-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.03 1.56V21h-4v-.08A1.7 1.7 0 0 0 8.95 19.4a1.7 1.7 0 0 0-1.88.34l-.06.06-2.83-2.83.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.55-1H3v-4h.08A1.7 1.7 0 0 0 4.6 8.95a1.7 1.7 0 0 0-.34-1.88l-.06-.06 2.83-2.83.06.06A1.7 1.7 0 0 0 8.95 4.6 1.7 1.7 0 0 0 10 3.08V3h4v.08a1.7 1.7 0 0 0 1.03 1.55 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.83 2.83-.06.06A1.7 1.7 0 0 0 19.4 9c.14.61.69 1.04 1.32 1.04H21v4h-.28A1.7 1.7 0 0 0 19.4 15Z" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"/></svg>
            <span></span>
        </button>
    `;
    document.body.appendChild(preview);

    preview.addEventListener('mouseenter', () => {
        clearTimeout(projectFolderPreviewCloseTimer);
    });
    preview.addEventListener('mouseleave', scheduleHideProjectFolderPreview);
    preview.querySelector('.project-folder-preview-edit')?.addEventListener('click', () => {
        const projectId = preview.dataset.projectId || '';
        hideProjectFolderPreview(true);
        if (projectId) showEditProjectModal(projectId, { fromChatSidebar: true });
    });
    window.addEventListener('resize', () => hideProjectFolderPreview(true));
    window.addEventListener('scroll', () => hideProjectFolderPreview(true), true);
    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape' && !preview.hidden) hideProjectFolderPreview(true);
    });
    return preview;
}

function positionProjectFolderPreview(preview, row) {
    if (!preview || !row?.isConnected) return;
    const anchorRect = row.getBoundingClientRect();
    const previewRect = preview.getBoundingClientRect();
    const gap = 10;
    const edge = 12;
    let left = anchorRect.right + gap;
    if (left + previewRect.width > window.innerWidth - edge) {
        left = Math.max(edge, anchorRect.left - previewRect.width - gap);
    }
    const top = Math.min(
        Math.max(edge, anchorRect.top - 10),
        Math.max(edge, window.innerHeight - previewRect.height - edge)
    );
    preview.style.left = `${Math.round(left)}px`;
    preview.style.top = `${Math.round(top)}px`;
}

function showProjectFolderPreview(project, row, conversations) {
    if (!project || !row?.isConnected || window.matchMedia('(max-width: 900px), (hover: none)').matches) return;
    hideProjectConversationPreview(true);
    const preview = ensureProjectFolderPreview();
    const isUnassigned = project._isUnassigned === true;
    clearTimeout(projectFolderPreviewCloseTimer);
    if (projectFolderPreviewAnchor && projectFolderPreviewAnchor !== row) {
        projectFolderPreviewAnchor.querySelector('.project-folder-item')?.removeAttribute('aria-controls');
    }
    projectFolderPreviewAnchor = row;
    const title = project.name || tp('common.untitled');
    const total = conversations.length;
    const active = conversations.filter((conversation) => chatProjectFolderContext.runningIds.has(conversation.id)).length;
    const fallbackStats = `${total} 个任务 · ${active} 个已开启`;
    const description = clampProjectPreviewText(project.description)
        || pickerMessage(tp, 'chat.projectPreviewNoDescription', '暂无项目说明');
    const scope = getProjectScopePreview(project);

    preview.dataset.projectId = project.id || '';
    preview.classList.toggle('is-unassigned', isUnassigned);
    preview.querySelector('.project-folder-preview-title').textContent = title;
    preview.querySelector('.project-folder-preview-stats span').textContent = tpFmt(
        'chat.projectPreviewStats',
        fallbackStats,
        { total, active }
    );
    const descriptionRow = preview.querySelector('.project-folder-preview-description');
    descriptionRow.querySelector('span').textContent = description;
    descriptionRow.classList.toggle('is-empty', !String(project.description || '').trim());
    const scopeRow = preview.querySelector('.project-folder-preview-scope');
    scopeRow.hidden = isUnassigned || !scope;
    scopeRow.querySelector('span').textContent = scope
        ? tpFmt('chat.projectPreviewScope', `测试范围：${scope}`, { scope })
        : '';
    const editButton = preview.querySelector('.project-folder-preview-edit');
    editButton.hidden = isUnassigned;
    editButton.querySelector('span').textContent = tpFmt(
        'chat.projectPreviewEdit',
        '编辑项目'
    );
    preview.hidden = false;
    row.querySelector('.project-folder-item')?.setAttribute('aria-controls', preview.id);
    positionProjectFolderPreview(preview, row);
    if (typeof applyRBACToUI === 'function') applyRBACToUI(preview);
}

function scheduleShowProjectFolderPreview(project, row, conversations, immediate = false) {
    clearTimeout(projectFolderPreviewOpenTimer);
    clearTimeout(projectFolderPreviewCloseTimer);
    projectFolderPreviewOpenTimer = setTimeout(
        () => showProjectFolderPreview(project, row, conversations),
        immediate ? 0 : PROJECT_FOLDER_PREVIEW_OPEN_DELAY_MS
    );
}

function hideProjectFolderPreview(immediate = false) {
    clearTimeout(projectFolderPreviewOpenTimer);
    clearTimeout(projectFolderPreviewCloseTimer);
    const close = () => {
        const preview = document.getElementById('project-folder-preview');
        if (preview) preview.hidden = true;
        projectFolderPreviewAnchor?.querySelector('.project-folder-item')?.removeAttribute('aria-controls');
        projectFolderPreviewAnchor = null;
    };
    if (immediate) close();
    else projectFolderPreviewCloseTimer = setTimeout(close, PROJECT_FOLDER_PREVIEW_CLOSE_DELAY_MS);
}

function scheduleHideProjectFolderPreview() {
    hideProjectFolderPreview(false);
}

function formatProjectConversationPreviewAge(value) {
    const timestamp = Date.parse(value || '');
    if (!Number.isFinite(timestamp)) return '';
    const date = new Date(timestamp);
    const pad = (part) => String(part).padStart(2, '0');
    const parts = {
        year: String(date.getFullYear()),
        month: pad(date.getMonth() + 1),
        day: pad(date.getDate()),
        hour: pad(date.getHours()),
        minute: pad(date.getMinutes()),
    };
    return tpFmt(
        'chat.conversationPreviewDateTime',
        `${parts.year}年${parts.month}月${parts.day}日 ${parts.hour}:${parts.minute}`,
        parts
    );
}

function getProjectConversationModeLabel(conversation) {
    const mode = String(conversation?.agentMode || conversation?.agent_mode || '').trim().toLowerCase();
    const labels = {
        eino_single: ['chat.agentModeEinoSingle', 'Eino 单代理（ADK）'],
        deep: ['chat.agentModeDeep', 'Deep（DeepAgent）'],
        plan_execute: ['chat.agentModePlanExecuteLabel', 'Plan-Execute'],
        supervisor: ['chat.agentModeSupervisorLabel', 'Supervisor（专家路由）'],
    };
    if (labels[mode]) return tpFmt(labels[mode][0], labels[mode][1]);
    return clampProjectPreviewText(
        conversation?.roleName || conversation?.role_name || pickerMessage(tp, 'chat.conversationPreviewDefaultMode', '默认'),
        80
    );
}

function getProjectConversationModeIconClass(conversation) {
    const mode = String(conversation?.agentMode || conversation?.agent_mode || '').trim().toLowerCase();
    if (mode === 'eino_single') return 'eino';
    if (mode === 'deep') return 'deep';
    if (mode === 'plan_execute') return 'plan';
    if (mode === 'supervisor') return 'supervisor';
    return 'default';
}

function ensureProjectConversationPreview() {
    let preview = document.getElementById('project-conversation-preview');
    if (preview) return preview;
    preview = document.createElement('div');
    preview.id = 'project-conversation-preview';
    preview.className = 'project-conversation-preview';
    preview.hidden = true;
    preview.setAttribute('role', 'tooltip');
    preview.innerHTML = `
        <div class="project-conversation-preview-header">
            <span class="project-conversation-preview-title"></span>
            <span class="project-conversation-preview-age"></span>
        </div>
        <div class="project-conversation-preview-meta">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M3 6.5h6l2 2h10v9.5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round"/></svg>
            <span class="project-conversation-preview-project"></span>
        </div>
        <div class="project-conversation-preview-meta">
            <span class="project-conversation-preview-mode-icon agent-mode-logo agent-mode-logo--default" aria-hidden="true"><svg class="agent-mode-logo__svg" viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="3" y="11" width="18" height="10" rx="2"/><circle cx="12" cy="5" r="2"/><path d="M12 7v4"/><path d="M8 16h.01"/><path d="M16 16h.01"/></svg></span>
            <span class="project-conversation-preview-mode"></span>
            <span class="project-conversation-preview-separator" aria-hidden="true">·</span>
            <span class="project-conversation-preview-status"></span>
        </div>
    `;
    document.body.appendChild(preview);
    window.addEventListener('resize', () => hideProjectConversationPreview(true));
    window.addEventListener('scroll', () => hideProjectConversationPreview(true), true);
    return preview;
}

function positionProjectConversationPreview(preview, row) {
    if (!preview || !row?.isConnected) return;
    const anchorRect = row.getBoundingClientRect();
    const previewRect = preview.getBoundingClientRect();
    const gap = 10;
    const edge = 12;
    let left = anchorRect.right + gap;
    if (left + previewRect.width > window.innerWidth - edge) {
        left = Math.max(edge, anchorRect.left - previewRect.width - gap);
    }
    const top = Math.min(
        Math.max(edge, anchorRect.top - 8),
        Math.max(edge, window.innerHeight - previewRect.height - edge)
    );
    preview.style.left = `${Math.round(left)}px`;
    preview.style.top = `${Math.round(top)}px`;
}

function showProjectConversationPreview(conversation, project, row) {
    if (!conversation || !row?.isConnected || window.matchMedia('(max-width: 900px), (hover: none)').matches) return;
    hideProjectFolderPreview(true);
    const preview = ensureProjectConversationPreview();
    clearTimeout(projectConversationPreviewCloseTimer);
    if (projectConversationPreviewAnchor && projectConversationPreviewAnchor !== row) {
        projectConversationPreviewAnchor.querySelector('.project-conversation-item')?.removeAttribute('aria-describedby');
    }
    projectConversationPreviewAnchor = row;
    const isRunning = chatProjectFolderContext.runningIds.has(conversation.id);
    const isWaitingApproval = chatProjectFolderContext.pendingApprovalByConversation.has(conversation.id);
    const completed = chatProjectFolderContext.completedByConversation.get(conversation.id);
    const isUnread = !isRunning && isProjectConversationUnread(conversation.id);
    const status = isWaitingApproval
        ? pickerMessage(tp, 'hitl.waitingApprovalShort', '等待批准')
        : (isRunning
        ? pickerMessage(tp, 'tasks.statusRunning', '执行中')
        : (isUnread
            ? pickerMessage(tp, 'chat.conversationPreviewUnread', '有未读更新')
            : (completed
                ? pickerMessage(tp, 'chat.conversationPreviewViewed', '已查看')
                : pickerMessage(tp, 'chat.conversationPreviewConversation', '对话'))));
    const statusEl = preview.querySelector('.project-conversation-preview-status');

    preview.querySelector('.project-conversation-preview-title').textContent = conversation.title
        || pickerMessage(tp, 'projects.untitledConversation', '未命名对话');
    const ageEl = preview.querySelector('.project-conversation-preview-age');
    ageEl.textContent = formatProjectConversationPreviewAge(
        conversation.updatedAt || conversation.updated_at || conversation.createdAt || conversation.created_at
    );
    ageEl.hidden = !ageEl.textContent;
    preview.querySelector('.project-conversation-preview-project').textContent = project?.name
        || pickerMessage(tp, 'chat.conversationPreviewNoProject', '未绑定项目');
    const modeIcon = preview.querySelector('.project-conversation-preview-mode-icon');
    if (modeIcon) {
        modeIcon.className = 'project-conversation-preview-mode-icon agent-mode-logo agent-mode-logo--' + getProjectConversationModeIconClass(conversation);
    }
    preview.querySelector('.project-conversation-preview-mode').textContent = getProjectConversationModeLabel(conversation);
    statusEl.textContent = status;
    statusEl.className = 'project-conversation-preview-status'
        + (isWaitingApproval ? ' is-approval' : (isRunning ? ' is-running' : (isUnread ? ' is-unread' : '')));
    preview.hidden = false;
    row.querySelector('.project-conversation-item')?.setAttribute('aria-describedby', preview.id);
    positionProjectConversationPreview(preview, row);
}

function scheduleShowProjectConversationPreview(conversation, project, row, immediate = false) {
    if (Date.now() < projectConversationPreviewSuppressedUntil) return;
    clearTimeout(projectConversationPreviewOpenTimer);
    clearTimeout(projectConversationPreviewCloseTimer);
    projectConversationPreviewOpenTimer = setTimeout(
        () => showProjectConversationPreview(conversation, project, row),
        immediate ? 0 : PROJECT_FOLDER_PREVIEW_OPEN_DELAY_MS
    );
}

function hideProjectConversationPreview(immediate = false) {
    clearTimeout(projectConversationPreviewOpenTimer);
    clearTimeout(projectConversationPreviewCloseTimer);
    const close = () => {
        const preview = document.getElementById('project-conversation-preview');
        if (preview) preview.hidden = true;
        projectConversationPreviewAnchor?.querySelector('.project-conversation-item')?.removeAttribute('aria-describedby');
        projectConversationPreviewAnchor = null;
    };
    if (immediate) close();
    else projectConversationPreviewCloseTimer = setTimeout(close, PROJECT_FOLDER_PREVIEW_CLOSE_DELAY_MS);
}

function appendChatProjectFolderItem(list, project, expandedIds, conversations) {
    const row = document.createElement('div');
    const isUnassigned = project._isUnassigned === true;
    const folderId = isUnassigned ? CHAT_UNASSIGNED_PROJECT_FOLDER_ID : project.id;
    const isExpanded = expandedIds.has(folderId);
    const statusKinds = getProjectFolderStatuses(conversations);
    row.className = 'project-folder-row'
        + (isExpanded ? ' is-expanded' : '')
        + (isUnassigned ? ' is-unassigned' : '');

    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'project-folder-item';
    button.dataset.projectId = project.id || '';
    button.dataset.folderId = folderId;
    button.setAttribute('aria-label', project.name || tp('common.untitled'));
    button.setAttribute('aria-expanded', isExpanded ? 'true' : 'false');

    const disclosure = document.createElement('span');
    disclosure.className = 'project-folder-disclosure';
    disclosure.setAttribute('aria-hidden', 'true');
    disclosure.innerHTML = projectFolderDisclosureMarkup(isExpanded);

    const icon = document.createElement('span');
    icon.className = 'project-folder-icon';
    icon.setAttribute('aria-hidden', 'true');
    icon.innerHTML = projectFolderIconMarkup(isExpanded);

    const title = document.createElement('span');
    title.className = 'project-folder-title';
    window.applyProjectNameDisplay(title, project.name, tp('common.untitled'));

    const label = document.createElement('span');
    label.className = 'project-folder-label';
    label.appendChild(title);
    if (!isUnassigned && project.pinned) {
        const pinIcon = document.createElement('span');
        pinIcon.className = 'project-folder-pinned';
        pinIcon.textContent = '📌';
        pinIcon.title = pickerMessage(tp, 'projects.pinned', '已置顶');
        pinIcon.setAttribute('aria-label', pinIcon.title);
        label.appendChild(pinIcon);
    }
    const folderApprovals = conversations
        .map((conversation) => chatProjectFolderContext.pendingApprovalByConversation.get(conversation.id))
        .filter(Boolean);
    const folderStatusGroup = appendProjectTaskStatuses(
        label,
        statusKinds,
        { approval: null },
        { approval: { aggregate: true, count: folderApprovals.length } }
    );
    if (folderStatusGroup) folderStatusGroup.classList.add('project-task-status-group--folder');

    button.appendChild(disclosure);
    button.appendChild(icon);
    button.appendChild(label);
    button.addEventListener('click', () => {
        if (isExpanded) {
            chatProjectFolderExpandedIds.delete(folderId);
        } else {
            chatProjectFolderExpandedIds.add(folderId);
        }
        renderChatProjectFolders(projectsCacheAll);
    });
    row.addEventListener('mouseenter', () => scheduleShowProjectFolderPreview(project, row, conversations));
    row.addEventListener('mouseleave', scheduleHideProjectFolderPreview);
    button.addEventListener('focus', () => scheduleShowProjectFolderPreview(project, row, conversations, true));
    row.addEventListener('focusout', (event) => {
        const preview = document.getElementById('project-folder-preview');
        if (row.contains(event.relatedTarget) || preview?.contains(event.relatedTarget)) return;
        scheduleHideProjectFolderPreview();
    });

    const actions = document.createElement('div');
    actions.className = 'project-folder-actions';

    if (!isUnassigned) {
        const menuButton = document.createElement('button');
        menuButton.type = 'button';
        menuButton.className = 'project-folder-action project-folder-menu';
        menuButton.setAttribute('aria-label', tp('projects.projectActions'));
        menuButton.title = menuButton.getAttribute('aria-label');
        menuButton.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/></svg>';
        menuButton.addEventListener('click', (event) => {
            showProjectListActionMenu(event, project.id, 'chat');
        });
        actions.appendChild(menuButton);
    }

    const newConversationButton = document.createElement('button');
    newConversationButton.type = 'button';
    newConversationButton.className = 'project-folder-action project-folder-new-conversation';
    newConversationButton.setAttribute(
        'aria-label',
        isUnassigned
            ? pickerMessage(tp, 'chat.newUnassignedConversation', '新建无项目对话')
            : pickerMessage(tp, 'chat.newConversationInProject', '在此项目中新建对话')
    );
    newConversationButton.title = newConversationButton.getAttribute('aria-label');
    newConversationButton.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>';
    newConversationButton.addEventListener('click', async (event) => {
        event.preventDefault();
        event.stopPropagation();
        chatProjectFolderExpandedIds.add(folderId);
        if (typeof window.startNewConversation === 'function') {
            await window.startNewConversation({ projectId: isUnassigned ? '' : project.id });
        }
        renderChatProjectFolders(projectsCacheAll);
    });

    actions.appendChild(newConversationButton);
    row.appendChild(button);
    row.appendChild(actions);
    list.appendChild(row);
}

function appendChatProjectConversationItem(list, conversation, project) {
    const row = document.createElement('div');
    const button = document.createElement('button');
    const isSelected = window.currentConversationId === conversation.id;
    const isRunning = chatProjectFolderContext.runningIds.has(conversation.id);
    const isWaitingApproval = chatProjectFolderContext.pendingApprovalByConversation.has(conversation.id);
    const completed = chatProjectFolderContext.completedByConversation.get(conversation.id);
    const isUnread = !isRunning && isProjectConversationUnread(conversation.id);
    row.className = 'project-conversation-row' + (isSelected ? ' is-selected' : '');
    button.type = 'button';
    button.className = 'project-conversation-item' + (isSelected ? ' is-selected' : '');
    button.dataset.conversationId = conversation.id;
    if (isSelected) button.setAttribute('aria-current', 'true');

    const title = document.createElement('span');
    title.className = 'project-conversation-title';
    title.textContent = conversation.title || pickerMessage(tp, 'projects.untitledConversation', '未命名对话');

    const label = document.createElement('span');
    label.className = 'project-conversation-label';
    label.appendChild(title);
    if (conversation.pinned) {
        const pinIcon = document.createElement('span');
        pinIcon.className = 'conversation-item-pinned project-conversation-pinned';
        pinIcon.textContent = '📌';
        pinIcon.title = pickerMessage(tp, 'projects.pinned', '已置顶');
        pinIcon.setAttribute('aria-label', pinIcon.title);
        label.appendChild(pinIcon);
    }
    const statusKinds = [];
    if (isWaitingApproval) statusKinds.push('approval');
    if (isRunning) statusKinds.push('running');
    else if (isUnread) statusKinds.push('unread');
    if (statusKinds.length) appendProjectTaskStatuses(label, statusKinds, {
        approval: chatProjectFolderContext.pendingApprovalByConversation.get(conversation.id)
    });
    button.appendChild(label);

    button.addEventListener('click', async (event) => {
        const targetConversationId = String(event.currentTarget && event.currentTarget.dataset.conversationId || '').trim();
        if (!targetConversationId) return;
        projectConversationPreviewSuppressedUntil = Date.now() + 700;
        hideProjectConversationPreview(true);
        selectChatProjectConversationItem(targetConversationId);
        if (typeof window.loadConversation === 'function') {
            await window.loadConversation(targetConversationId);
        }
        if (window.currentConversationId === targetConversationId && completed) {
            markProjectConversationViewed(targetConversationId, completed.completedAt);
            renderChatProjectFolders(projectsCacheAll);
        }
    });
    row.addEventListener('mouseenter', () => scheduleShowProjectConversationPreview(conversation, project, row));
    row.addEventListener('mouseleave', () => hideProjectConversationPreview(false));
    button.addEventListener('focus', () => scheduleShowProjectConversationPreview(conversation, project, row, true));
    row.addEventListener('focusout', (event) => {
        if (row.contains(event.relatedTarget)) return;
        hideProjectConversationPreview(false);
    });
    const menuButton = document.createElement('button');
    menuButton.type = 'button';
    menuButton.className = 'project-conversation-menu';
    menuButton.setAttribute(
        'aria-label',
        pickerMessage(tp, 'chat.conversationActions', '对话操作')
    );
    menuButton.title = menuButton.getAttribute('aria-label');
    menuButton.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/></svg>';
    menuButton.addEventListener('click', (event) => {
        if (typeof window.openConversationContextMenuForId === 'function') {
            window.openConversationContextMenuForId(event, conversation.id, conversation.title || '');
        }
    });

    row.appendChild(button);
    row.appendChild(menuButton);
    list.appendChild(row);
}

function selectChatProjectConversationItem(conversationId) {
    const id = String(conversationId || '').trim();
    document.querySelectorAll('.project-conversation-row').forEach((row) => {
        const button = row.querySelector('.project-conversation-item');
        const selected = !!id && button?.dataset.conversationId === id;
        row.classList.toggle('is-selected', selected);
        if (!button) return;
        button.classList.toggle('is-selected', selected);
        if (selected) button.setAttribute('aria-current', 'true');
        else button.removeAttribute('aria-current');
    });
}

window.selectChatProjectConversationItem = selectChatProjectConversationItem;

async function loadChatProjectFolderContext() {
    const loadSeq = ++chatProjectFolderContextLoadSeq;
    const conversationsParams = new URLSearchParams({ limit: '1000', offset: '0', sort_by: 'updated_at' });
    const [conversationResponse, activeResponse, completedResponse, pendingResponse] = await Promise.all([
        apiFetch(`/api/conversations?${conversationsParams}`),
        apiFetch('/api/agent-loop/tasks'),
        apiFetch('/api/agent-loop/tasks/completed'),
        apiFetch('/api/hitl/pending?page=1&pageSize=200'),
    ]);
    if (!conversationResponse.ok) throw new Error(tp('projects.loadProjectsFailed'));
    const conversationData = await conversationResponse.json();
    const activeData = activeResponse.ok ? await activeResponse.json() : { tasks: [] };
    const completedData = completedResponse.ok ? await completedResponse.json() : { tasks: [] };
    const pendingData = pendingResponse.ok ? await pendingResponse.json() : { items: [] };
    if (loadSeq !== chatProjectFolderContextLoadSeq) return false;
    const conversations = Array.isArray(conversationData)
        ? conversationData
        : (conversationData.conversations || conversationData.items || []);
    chatProjectFolderContext.conversations = Array.isArray(conversations) ? conversations : [];
    chatProjectFolderContext.runningIds = new Set(
        (activeData.tasks || [])
            .filter((task) => task && !['completed', 'failed', 'timeout', 'cancelled'].includes(String(task.status || '').toLowerCase()))
            .map((task) => task.conversationId)
            .filter(Boolean)
    );
    chatProjectFolderContext.completedByConversation = new Map();
    (completedData.tasks || []).forEach((task) => {
        if (task?.conversationId && !chatProjectFolderContext.completedByConversation.has(task.conversationId)) {
            chatProjectFolderContext.completedByConversation.set(task.conversationId, task);
        }
    });
    chatProjectFolderContext.pendingApprovalByConversation = new Map();
    (pendingData.items || []).filter(isHumanProjectPendingApproval).forEach((item) => {
        const conversationId = String(item?.conversationId || '').trim();
        // pending 审批必须属于当前进程仍在运行的任务；服务重启/取消后的旧记录
        // 即使在并发窗口内被读到，也不能重新显示倒计时徽标。
        if (conversationId && chatProjectFolderContext.runningIds.has(conversationId) &&
            !chatProjectFolderContext.pendingApprovalByConversation.has(conversationId)) {
            chatProjectFolderContext.pendingApprovalByConversation.set(conversationId, item);
        }
    });
    chatProjectFolderContext.ready = true;
    return true;
}

function isHumanProjectPendingApproval(item) {
    if (!item) return false;
    const reviewer = String(item.reviewer || item.decidedBy || item.decided_by || '').trim().toLowerCase();
    const status = String(item.status || '').trim().toLowerCase();
    return reviewer !== 'audit_agent' && reviewer !== 'agent' && reviewer !== 'ai' && status !== 'audit_running';
}

function getProjectConversationSortTime(conversation) {
    const value = conversation?.updatedAt || conversation?.updated_at
        || conversation?.createdAt || conversation?.created_at || '';
    const timestamp = Date.parse(value);
    return Number.isFinite(timestamp) ? timestamp : 0;
}

function sortProjectFolderConversations(conversations) {
    return [...conversations].sort((a, b) => {
        const pinnedDelta = Number(!!b?.pinned) - Number(!!a?.pinned);
        if (pinnedDelta) return pinnedDelta;
        return getProjectConversationSortTime(b) - getProjectConversationSortTime(a);
    });
}

function updateChatProjectConversationPinnedState(conversationId, pinned) {
    const id = String(conversationId || '').trim();
    const conversation = chatProjectFolderContext.conversations.find(
        (item) => String(item?.id || '').trim() === id
    );
    if (!conversation) return false;
    conversation.pinned = !!pinned;
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    }
    return true;
}

function removeChatProjectConversation(conversationId) {
    const id = String(conversationId || '').trim();
    if (!id) return false;
    const previousLength = chatProjectFolderContext.conversations.length;
    chatProjectFolderContext.conversations = chatProjectFolderContext.conversations.filter(
        (item) => String(item?.id || '').trim() !== id
    );
    chatProjectFolderContext.runningIds.delete(id);
    chatProjectFolderContext.completedByConversation.delete(id);
    chatProjectFolderContext.pendingApprovalByConversation.delete(id);
    if (projectConversationPreviewAnchor?.querySelector('.project-conversation-item')?.dataset.conversationId === id) {
        hideProjectConversationPreview(true);
    }
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    }
    return chatProjectFolderContext.conversations.length !== previousLength;
}

function refreshChatProjectFoldersAfterAction() {
    Promise.resolve().then(() => refreshChatProjectFolders()).catch((error) => {
        console.warn('刷新项目对话列表失败:', error);
    });
}

function resolveChatProjectFolderSelection() {
    const conversationId = String(window.currentConversationId || '').trim();
    if (!conversationId) return resolveChatProjectSelection();
    if (!chatProjectFolderContext.ready) return null;

    const conversation = chatProjectFolderContext.conversations.find(
        (item) => String(item?.id || '').trim() === conversationId
    );
    if (!conversation) return null;
    return String(conversation.projectId || conversation.project_id || '').trim();
}

function appendChatProjectFoldersLoadMore(list, remainingCount) {
    if (!list || remainingCount <= 0) return;
    const button = document.createElement('button');
    const label = pickerMessage(tp, 'common.loadMore', '加载更多');
    button.type = 'button';
    button.className = 'project-folders-load-more';
    button.setAttribute('aria-label', tpFmt(
        'chat.projectFoldersLoadMoreRemaining',
        `${label}，剩余 ${remainingCount} 个项目`,
        { count: remainingCount }
    ));
    button.innerHTML = `<span>${escapeHtml(label)}</span><span class="project-folders-load-more-count">${remainingCount}</span>`;
    button.addEventListener('click', loadMoreChatProjectFolders);
    list.appendChild(button);
}

function loadMoreChatProjectFolders() {
    chatProjectFolderVisibleCount += CHAT_PROJECT_FOLDER_PAGE_SIZE;
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    } else {
        refreshChatProjectFolders();
    }
}

function renderChatProjectFolders(projects) {
    const list = document.getElementById('project-folders-list');
    if (!list) return;
    hideProjectFolderPreview(true);
    hideProjectConversationPreview(true);
    const selectedId = resolveChatProjectFolderSelection();
    if (selectedId !== null && chatProjectFolderLastSelectionId !== selectedId) {
        chatProjectFolderExpandedIds.add(selectedId || CHAT_UNASSIGNED_PROJECT_FOLDER_ID);
        chatProjectFolderLastSelectionId = selectedId;
    }
    const filtered = filterActiveProjectsLocal(projects, chatProjectFolderSearchQuery);
    const unassignedProject = {
        id: '',
        name: tp('projects.noProject'),
        description: tp('projects.noProjectDescription'),
        _isUnassigned: true,
    };
    const includeUnassigned = matchProjectSearchQuery(unassignedProject, chatProjectFolderSearchQuery);
    const pinnedProjects = filtered.filter((project) => !!project.pinned);
    const regularProjects = filtered.filter((project) => !project.pinned);
    const folders = includeUnassigned
        ? [...pinnedProjects, unassignedProject, ...regularProjects]
        : filtered;
    const queryKey = chatProjectFolderSearchQuery.toLocaleLowerCase();
    if (queryKey !== chatProjectFolderLastQuery) {
        chatProjectFolderLastQuery = queryKey;
        chatProjectFolderVisibleCount = CHAT_PROJECT_FOLDER_PAGE_SIZE;
    }
    const selectedFolderId = selectedId === null
        ? null
        : (selectedId || CHAT_UNASSIGNED_PROJECT_FOLDER_ID);
    if (selectedFolderId !== null) {
        const selectedIndex = folders.findIndex((project) => (
            project._isUnassigned ? CHAT_UNASSIGNED_PROJECT_FOLDER_ID : project.id
        ) === selectedFolderId);
        if (selectedIndex >= chatProjectFolderVisibleCount) {
            chatProjectFolderVisibleCount = selectedIndex + 1;
        }
    }
    const visibleFolders = folders.slice(0, chatProjectFolderVisibleCount);
    list.innerHTML = '';
    if (!folders.length) {
        const empty = document.createElement('div');
        empty.className = 'project-folders-empty';
        empty.textContent = chatProjectFolderSearchQuery
            ? pickerMessage(tp, 'chat.filterProjectSearchEmpty', '没有匹配的项目')
            : pickerMessage(tp, 'projects.noProjects', '暂无项目');
        list.appendChild(empty);
        return;
    }
    visibleFolders.forEach((project) => {
        const folderId = project._isUnassigned ? CHAT_UNASSIGNED_PROJECT_FOLDER_ID : project.id;
        const conversations = sortProjectFolderConversations(
            chatProjectFolderContext.conversations
                .filter((conversation) => (conversation.projectId || conversation.project_id || '') === project.id)
        );
        appendChatProjectFolderItem(list, project, chatProjectFolderExpandedIds, conversations);
        if (chatProjectFolderExpandedIds.has(folderId)) {
            conversations.forEach((conversation) => appendChatProjectConversationItem(list, conversation, project));
        }
    });
    appendChatProjectFoldersLoadMore(list, folders.length - visibleFolders.length);
}

async function refreshChatProjectFolders() {
    const list = document.getElementById('project-folders-list');
    if (!list) return;
    const seq = ++chatProjectFolderRenderSeq;
    if (!isProjectsCacheReady()) {
        list.innerHTML = '';
        appendChatProjectPanelMessage(list, 'project-folders-empty', pickerMessage(tp, 'common.loading', '加载中…'));
    }
    try {
        const [projects] = await Promise.all([
            ensureProjectsLoaded(),
            loadChatProjectFolderContext(),
        ]);
        if (seq !== chatProjectFolderRenderSeq) return;
        renderChatProjectFolders(projects);
    } catch (e) {
        if (seq !== chatProjectFolderRenderSeq) return;
        list.innerHTML = '';
        appendChatProjectPanelMessage(
            list,
            'project-folders-empty',
            pickerMessage(tp, 'projects.loadProjectsFailed', '加载项目失败')
        );
    }
}

function handleProjectFolderSearch(value) {
    chatProjectFolderSearchQuery = String(value || '').trim();
    const clearButton = document.getElementById('conversation-search-clear');
    if (clearButton) clearButton.style.display = chatProjectFolderSearchQuery ? 'flex' : 'none';
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    } else {
        refreshChatProjectFolders();
    }
}

function clearProjectFolderSearch() {
    const input = document.getElementById('conversation-search-input');
    if (input) input.value = '';
    handleProjectFolderSearch('');
    input?.focus();
}

function updateProjectFolderTaskStatuses(tasks) {
    const previous = chatProjectFolderContext.runningIds;
    const next = new Set(
        (Array.isArray(tasks) ? tasks : [])
            .filter((task) => task && !['completed', 'failed', 'timeout', 'cancelled'].includes(String(task.status || '').toLowerCase()))
            .map((task) => task.conversationId)
            .filter(Boolean)
    );
    const taskFinished = [...previous].some((conversationId) => !next.has(conversationId));
    let approvalChanged = false;
    chatProjectFolderContext.pendingApprovalByConversation.forEach((_details, conversationId) => {
        if (next.has(conversationId)) return;
        chatProjectFolderContext.pendingApprovalByConversation.delete(conversationId);
        approvalChanged = true;
    });
    const changed = previous.size !== next.size || [...previous].some((conversationId) => !next.has(conversationId));
    if (!changed && !approvalChanged) return;
    chatProjectFolderContext.runningIds = next;
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    }
    if (taskFinished) refreshChatProjectFolders();
}

function setProjectConversationApprovalStatus(conversationId, pending, details) {
    const id = String(conversationId || '').trim();
    if (!id) return;
    if (pending && isHumanProjectPendingApproval(details || {})) chatProjectFolderContext.pendingApprovalByConversation.set(id, details || { conversationId: id });
    else chatProjectFolderContext.pendingApprovalByConversation.delete(id);
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    }
}

function syncProjectConversationApprovalStatuses(items) {
    const next = new Map();
    (Array.isArray(items) ? items : []).forEach((details) => {
        if (!isHumanProjectPendingApproval(details)) return;
        const id = String(details && details.conversationId || '').trim();
        if (id && chatProjectFolderContext.runningIds.has(id) && !next.has(id)) {
            next.set(id, details);
        }
    });
    let changed = next.size !== chatProjectFolderContext.pendingApprovalByConversation.size;
    if (!changed) {
        next.forEach((details, id) => {
            const previous = chatProjectFolderContext.pendingApprovalByConversation.get(id);
            const previousInterruptId = String(previous && (previous.interruptId || previous.id) || '');
            const nextInterruptId = String(details && (details.interruptId || details.id) || '');
            if (!previous || previousInterruptId !== nextInterruptId) changed = true;
        });
    }
    if (!changed) return;
    chatProjectFolderContext.pendingApprovalByConversation = next;
    if (isProjectsCacheReady() && chatProjectFolderContext.ready) {
        renderChatProjectFolders(projectsCacheAll);
    }
}

window.setProjectConversationApprovalStatus = setProjectConversationApprovalStatus;
window.syncProjectConversationApprovalStatuses = syncProjectConversationApprovalStatuses;

if (!window._projectConversationActionEventsBound) {
    window._projectConversationActionEventsBound = true;
    document.addEventListener('conversation-pinned-changed', (event) => {
        const details = event?.detail || {};
        updateChatProjectConversationPinnedState(details.conversationId, details.pinned);
        refreshChatProjectFoldersAfterAction();
    });
    document.addEventListener('conversation-deleted', (event) => {
        const conversationId = event?.detail?.conversationId;
        removeChatProjectConversation(conversationId);
        refreshChatProjectFoldersAfterAction();
    });
}

if (!window._projectApprovalStatusEventsBound) {
    window._projectApprovalStatusEventsBound = true;
    window.addEventListener('hitl-interrupt', (event) => {
        const details = event && event.detail ? event.detail : {};
        setProjectConversationApprovalStatus(details.conversationId || window.currentConversationId || '', true, details);
    });
    window.addEventListener('hitl-resolved', (event) => {
        const details = event && event.detail ? event.detail : {};
        setProjectConversationApprovalStatus(details.conversationId || window.currentConversationId || '', false);
    });
}

function appendChatProjectPanelItem(list, project, selectedId, onSelect, tFn) {
    const t = tFn || tp;
    const isNone = !project.id;
    const isSelected = isNone ? !selectedId : selectedId === project.id;
    const fullDesc = isNone
        ? (project.description || '')
        : (project.description || '').trim() || t('projects.sharedFactBoard');
    const desc = isNone
        ? (project.description || '')
        : fullDesc.slice(0, 80);
    const fullName = project.name || t('common.untitled');
    const displayName = window.formatProjectNameForDisplay(fullName);
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'role-selection-item-main' + (isSelected ? ' selected' : '');
    btn.setAttribute('role', 'option');
    btn.setAttribute('aria-label', fullName);
    btn.setAttribute('data-selection-detail', fullDesc);
    btn.onclick = () => onSelect(project.id || '');
    btn.innerHTML = `
        <div class="role-selection-item-icon-main">${isNone ? '—' : '📁'}</div>
        <div class="role-selection-item-content-main">
            <div class="role-selection-item-name-main" title="${escapeAttr(fullName)}">${escapeHtml(displayName)}</div>
            <div class="role-selection-item-description-main">${escapeHtml(desc)}</div>
        </div>
        ${isSelected ? '<div class="role-selection-checkmark-main">✓</div>' : ''}
    `;
    list.appendChild(btn);
}

function appendChatProjectPanelMessage(list, className, text) {
    const el = document.createElement('div');
    el.className = className;
    el.textContent = text;
    list.appendChild(el);
    return el;
}

function pickerMessage(t, key, fallback) {
    const value = t(key);
    if (!value || value === key) return fallback;
    return value;
}

async function renderProjectPickerPanel(panelKey, config) {
    const state = projectPickerPanelState[panelKey];
    const list = document.getElementById(config.listId);
    if (!list || !state) return;
    const query = (document.getElementById(config.searchInputId)?.value || '').trim();
    const seq = ++state.seq;
    const selectedId = config.getSelectedId();
    const t = config.t || tp;

    const renderPinned = () => {
        appendChatProjectPanelItem(
            list,
            {
                id: '',
                name: t('projects.noProject'),
                description: t('projects.noProjectDescription'),
            },
            selectedId,
            config.onSelect,
            t
        );
    };

    const needsFetch = !isProjectsCacheReady();
    let loadingEl = null;
    if (needsFetch) {
        list.innerHTML = '';
        renderPinned();
        loadingEl = appendChatProjectPanelMessage(
            list,
            'chat-project-panel-loading',
            pickerMessage(t, 'common.loading', '加载中…')
        );
    }

    try {
        const all = await ensureProjectsLoaded();
        if (seq !== state.seq) return;

        list.innerHTML = '';
        renderPinned();
        const projects = filterActiveProjectsLocal(all, query);
        projects.forEach((p) => {
            appendChatProjectPanelItem(list, p, selectedId, config.onSelect, t);
        });

        if (query && projects.length === 0) {
            appendChatProjectPanelMessage(
                list,
                'chat-project-panel-empty',
                pickerMessage(t, 'chat.filterProjectSearchEmpty', '没有匹配的项目')
            );
        }
    } catch (e) {
        if (seq !== state.seq) return;
        list.innerHTML = '';
        renderPinned();
        appendChatProjectPanelMessage(
            list,
            'chat-project-panel-empty',
            pickerMessage(t, 'chat.filterProjectSearchFailed', '加载项目失败，请重试')
        );
    } finally {
        if (loadingEl && loadingEl.parentNode) loadingEl.remove();
    }
}

function initProjectPickerPanelSearch(panelKey, searchInputId, onSearch) {
    const input = document.getElementById(searchInputId);
    if (!input || input.dataset.pickerBound === panelKey) return;
    input.dataset.pickerBound = panelKey;
    input.addEventListener('input', onSearch);
    input.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            if (panelKey === 'chat' && typeof closeChatProjectPanel === 'function') {
                closeChatProjectPanel();
            } else if (panelKey === 'webshell' && typeof wsCloseProjectPanel === 'function') {
                wsCloseProjectPanel();
            }
        }
    });
}

function clearProjectPickerPanelSearch(panelKey, searchInputId) {
    const state = projectPickerPanelState[panelKey];
    if (!state) return;
    state.seq += 1;
    if (state.timer) {
        clearTimeout(state.timer);
        state.timer = null;
    }
    const input = document.getElementById(searchInputId);
    if (input) input.value = '';
}

function scheduleProjectPickerPanelSearch(panelKey, loadFn) {
    const state = projectPickerPanelState[panelKey];
    if (!state) return;
    if (state.timer) clearTimeout(state.timer);
    state.timer = setTimeout(() => {
        state.timer = null;
        loadFn();
    }, PROJECT_PICKER_DEBOUNCE_MS);
}

async function loadChatProjectPanelList() {
    await renderProjectPickerPanel('chat', {
        listId: 'chat-project-list',
        searchInputId: 'chat-project-search',
        getSelectedId: resolveChatProjectSelection,
        onSelect: (projectId) => selectChatProject(projectId),
    });
}

async function ensureChatProjectButtonLabel() {
    const id = (resolveChatProjectSelection() || '').trim();
    if (id && !projectNameById[id]) {
        await fetchProjectSummary(id);
    }
    updateChatProjectButtonLabel();
}

function updateChatProjectButtonLabel() {
    const textEl = document.getElementById('chat-project-text');
    if (!textEl) return;
    const id = resolveChatProjectSelection();
    window.applyProjectNameDisplay(
        textEl,
        id && projectNameById[id] ? projectNameById[id] : tp('projects.noProject')
    );
    if (typeof window.refreshChatWelcomeEmptyState === 'function') {
        window.refreshChatWelcomeEmptyState();
    }
}

async function renderChatProjectPanel() {
    initProjectPickerPanelSearch('chat', 'chat-project-search', () => {
        scheduleProjectPickerPanelSearch('chat', () => loadChatProjectPanelList());
    });
    clearProjectPickerPanelSearch('chat', 'chat-project-search');
    await loadChatProjectPanelList();
    const panel = document.getElementById('chat-project-panel');
    if (panel && typeof applyRBACToUI === 'function') applyRBACToUI(panel);
    requestAnimationFrame(() => document.getElementById('chat-project-search')?.focus());
}

function closeChatProjectPanel() {
    const panel = document.getElementById('chat-project-panel');
    const btn = document.getElementById('chat-project-btn');
    if (panel) panel.style.display = 'none';
    if (btn) {
        btn.classList.remove('active');
        btn.setAttribute('aria-expanded', 'false');
    }
    clearProjectPickerPanelSearch('chat', 'chat-project-search');
}

async function toggleChatProjectPanel() {
    const panel = document.getElementById('chat-project-panel');
    const btn = document.getElementById('chat-project-btn');
    if (!panel) return;
    const isHidden = panel.style.display === 'none' || !panel.style.display;
    if (!isHidden) {
        closeChatProjectPanel();
        return;
    }
    if (typeof closeRoleSelectionPanel === 'function') closeRoleSelectionPanel();
    if (typeof closeAgentModePanel === 'function') closeAgentModePanel();
    if (typeof closeChatReasoningPanel === 'function') closeChatReasoningPanel();
    panel.style.display = 'flex';
    if (btn) {
        btn.classList.add('active');
        btn.setAttribute('aria-expanded', 'true');
    }
    await renderChatProjectPanel();
}

async function selectChatProject(projectId) {
    closeChatProjectPanel();
    await applyChatProjectSelection(projectId || '');
}

async function applyChatProjectSelection(projectId) {
    const prev = getChatProjectSelection();
    if (projectId === prev) {
        updateChatProjectButtonLabel();
        await refreshChatProjectFolders();
        return;
    }
    if (window.currentConversationId) {
        try {
            const res = await apiFetch(`/api/conversations/${encodeURIComponent(window.currentConversationId)}/project`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ projectId }),
            });
            if (!res.ok) {
                const err = await res.json().catch(() => ({}));
                throw new Error(err.error || res.statusText);
            }
            window._loadedConversationProjectId = projectId;
            if (typeof showNotification === 'function') {
                showNotification(projectId ? tp('projects.projectBound') : tp('projects.projectUnbound'), 'success');
            }
        } catch (e) {
            console.error(e);
            alert(tp('projects.updateProjectBindingFailed') + ': ' + (e.message || e));
            updateChatProjectButtonLabel();
            return;
        }
    } else {
        setActiveProjectId(projectId);
    }
    updateChatProjectButtonLabel();
    await refreshChatProjectFolders();
    if (typeof window.onConversationProjectBindingChanged === 'function') {
        window.onConversationProjectBindingChanged(projectId);
    }
}

/** 对话页项目选择器：同步按钮文案；若浮层已打开则刷新列表 */
async function refreshChatProjectSelector(options = {}) {
    if (!document.getElementById('chat-project-btn')) return;
    try {
        await normalizeStaleChatProjectSelection();
        await ensureChatProjectButtonLabel();
    } catch (e) {
        console.warn(e);
    }
    if (options.renderFolders !== false) {
        const reloadFolders = options.reloadFolders !== false || !isProjectsCacheReady() || !chatProjectFolderContext.ready;
        if (reloadFolders) await refreshChatProjectFolders();
        else renderChatProjectFolders(projectsCacheAll);
    }
    const panel = document.getElementById('chat-project-panel');
    if (panel && panel.style.display === 'flex') {
        await loadChatProjectPanelList();
    }
}

async function onChatProjectChange() {
    /* 兼容旧调用；新 UI 使用 selectChatProject */
    await applyChatProjectSelection(getChatProjectSelection());
}

function initChatProjectSelector() {
    if (window._chatProjectSelectorInited) return;
    window._chatProjectSelectorInited = true;
    if (!window._projectsLanguageListenerBound) {
        window._projectsLanguageListenerBound = true;
        document.addEventListener('languagechange', () => {
            renderProjectsSidebar();
            renderProjectsPagination();
            syncAllProjectsFilterSelects();
            updateChatProjectButtonLabel();
            refreshChatProjectFolders();
            const panel = document.getElementById('chat-project-panel');
            if (panel && panel.style.display === 'flex') loadChatProjectPanelList();
            if (currentProjectId) {
                refreshProjectDetailMetaI18n();
                const source = projectsCacheAll.length ? projectsCacheAll : projectsCache;
                const p = source.find((x) => x.id === currentProjectId);
                if (p) updateProjectStatusPill(p.status || 'active');
                refreshProjectHeaderStats().catch(() => {});
                switchProjectTab(currentProjectTab || 'facts');
            }
        });
    }
    refreshChatProjectSelector().catch(() => {});
    document.addEventListener('click', (e) => {
        const panel = document.getElementById('chat-project-panel');
        const wrapper = document.querySelector('.project-selector-wrapper');
        if (!panel || panel.style.display === 'none' || !panel.style.display) return;
        if (!wrapper?.contains(e.target)) {
            closeChatProjectPanel();
        }
    });
}

function initProjectGraphFooterWheelScroll() {
    const footer = document.querySelector('.project-fact-graph-footer');
    if (!footer || footer.dataset.wheelScrollBound === 'true') return;
    footer.dataset.wheelScrollBound = 'true';
    footer.addEventListener('wheel', (event) => {
        if (footer.scrollWidth <= footer.clientWidth || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return;
        const maxScrollLeft = footer.scrollWidth - footer.clientWidth;
        const nextScrollLeft = Math.max(0, Math.min(maxScrollLeft, footer.scrollLeft + event.deltaY));
        if (nextScrollLeft === footer.scrollLeft) return;
        footer.scrollLeft = nextScrollLeft;
        event.preventDefault();
    }, { passive: false });
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        initChatProjectSelector();
        initProjectConversationReadTracking();
        initProjectListActionMenu();
        initProjectGraphFooterWheelScroll();
        refreshProjectsFilterSelects();
    });
} else {
    initChatProjectSelector();
    initProjectConversationReadTracking();
    initProjectListActionMenu();
    initProjectGraphFooterWheelScroll();
    refreshProjectsFilterSelects();
}

window.initProjectsPage = initProjectsPage;
window.showNewProjectModal = showNewProjectModal;
window.showEditProjectModal = showEditProjectModal;
window.showNewProjectModalFromChat = showNewProjectModalFromChat;
window.showNewProjectModalFromChatSidebar = showNewProjectModalFromChatSidebar;
window.saveProjectModal = saveProjectModal;
window.closeProjectModal = closeProjectModal;
window.selectProject = selectProject;
window.switchProjectTab = switchProjectTab;
window.showAddFactModal = showAddFactModal;
window.showEditFactModal = showEditFactModal;
window.editFactFromDetail = editFactFromDetail;
window.saveFactModal = saveFactModal;
window.closeFactModal = closeFactModal;
window.closeFactDetailModal = closeFactDetailModal;
window.saveProjectSettings = saveProjectSettings;
window.archiveCurrentProject = archiveCurrentProject;
window.deleteCurrentProject = deleteCurrentProject;
window.showProjectListActionMenu = showProjectListActionMenu;
window.editProjectFromListMenu = editProjectFromListMenu;
window.toggleProjectPinnedFromListMenu = toggleProjectPinnedFromListMenu;
window.toggleProjectArchiveFromListMenu = toggleProjectArchiveFromListMenu;
window.deleteProjectFromListMenu = deleteProjectFromListMenu;
window.refreshChatProjectSelector = refreshChatProjectSelector;
window.onChatProjectChange = onChatProjectChange;
window.toggleChatProjectPanel = toggleChatProjectPanel;
window.closeChatProjectPanel = closeChatProjectPanel;
window.selectChatProject = selectChatProject;
window.renderProjectPickerPanel = renderProjectPickerPanel;
window.initProjectPickerPanelSearch = initProjectPickerPanelSearch;
window.clearProjectPickerPanelSearch = clearProjectPickerPanelSearch;
window.scheduleProjectPickerPanelSearch = scheduleProjectPickerPanelSearch;
window.loadChatProjectPanelList = loadChatProjectPanelList;
window.refreshChatProjectFolders = refreshChatProjectFolders;
window.handleProjectFolderSearch = handleProjectFolderSearch;
window.clearProjectFolderSearch = clearProjectFolderSearch;
window.loadMoreChatProjectFolders = loadMoreChatProjectFolders;
window.updateProjectFolderTaskStatuses = updateProjectFolderTaskStatuses;
window.markCurrentProjectConversationViewed = markCurrentProjectConversationViewed;
window.prefetchProjectsForChat = prefetchProjectsForChat;
window.ensureDefaultActiveProjectForNewChat = ensureDefaultActiveProjectForNewChat;
window.getActiveProjectId = getActiveProjectId;
window.getProjectName = getProjectName;
window.viewProjectFactBody = viewProjectFactBody;
window.insertFactBodyTemplate = insertFactBodyTemplate;
window.updateFactFormHints = updateFactFormHints;
window.deprecateProjectFactByKey = deprecateProjectFactByKey;
window.restoreProjectFactByKey = restoreProjectFactByKey;
window.openVulnerabilitiesForProject = openVulnerabilitiesForProject;
window.openVulnerabilityDetail = openVulnerabilityDetail;
window.filterProjectsList = filterProjectsList;
window.goProjectsPage = goProjectsPage;
window.changeProjectsPageSize = changeProjectsPageSize;
window.parseProjectsListResponse = parseProjectsListResponse;
window.fetchAllProjects = fetchAllProjects;
window.debouncedLoadProjectFacts = debouncedLoadProjectFacts;
window.debouncedLoadProjectVulnerabilities = debouncedLoadProjectVulnerabilities;
window.loadProjectVulnerabilities = loadProjectVulnerabilities;
window.linkFactToExistingVulnerability = linkFactToExistingVulnerability;
window.createVulnerabilityFromCurrentFact = createVulnerabilityFromCurrentFact;
window.viewFactsForVulnerability = viewFactsForVulnerability;
window.openProjectConversation = openProjectConversation;
window.unbindConversationFromProject = unbindConversationFromProject;
window.loadProjectConversations = loadProjectConversations;
window.loadProjectAssets = loadProjectAssets;
window.openProjectAssetDetail = openProjectAssetDetail;
window.unbindAssetFromProject = unbindAssetFromProject;
window.loadProjectFactGraph = loadProjectFactGraph;
window.filterProjectFactGraph = filterProjectFactGraph;
window.centerProjectFactGraph = centerProjectFactGraph;
window.closeProjectFactGraphSidebar = closeProjectFactGraphSidebar;
window.openSelectedGraphFactDetail = openSelectedGraphFactDetail;
window.editSelectedGraphFact = editSelectedGraphFact;
window.promoteConversationAttackChain = promoteConversationAttackChain;
window.deleteProjectFactEdge = deleteProjectFactEdge;
window.focusProjectFactGraphEdge = focusProjectFactGraphEdge;
window.toggleProjectFactGraphConnectMode = toggleProjectFactGraphConnectMode;
window.rebuildProjectNameMap = rebuildProjectNameMap;
window.rememberProjectsInNameMap = rememberProjectsInNameMap;
window.searchActiveProjects = searchActiveProjects;
window.filterActiveProjectsLocal = filterActiveProjectsLocal;
window.fetchProjectSummary = fetchProjectSummary;
window.projectNameById = projectNameById;
window.ensureProjectsLoaded = ensureProjectsLoaded;
window.isProjectsCacheReady = isProjectsCacheReady;
