// 多代理子 Agent Markdown（agents/*.md）管理
function _agentsT(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

// Seed agent frontmatter（agents/*.md 的内置 .md 名/描述）。仅当值与仓库种子
// 精确匹配时按 id 翻译展示；用户自行编辑后的名/描述按原值展示。
const SEED_AGENTS = {
    'cyberstrike-deep': {
        name: '协调主代理',
        description: '多代理模式下的 Deep 编排者：在已授权安全场景中与 MCP 工具、task 子代理协同，负责规划、委派、汇总与对用户交付；派单前必须向子代理提供完整目标与范围。'
    },
    'cyberstrike-plan-execute': {
        name: 'Plan-Execute 规划主代理',
        description: 'plan_execute 模式下的规划/重规划侧主代理：拆解目标、修订计划，由执行器调用 MCP 工具落地（不使用 Deep 的 task 子代理）；计划中每步须含完整目标与范围，禁止让执行器凭猜测补全 URL/IP。'
    },
    'cyberstrike-supervisor': {
        name: 'Supervisor 监督主代理',
        description: 'supervisor 模式下的协调者：通过 transfer 委派专家子代理，必要时亲自使用 MCP；完成目标时用 exit 结束（运行时会追加专家列表与 exit 说明）；transfer 前必须提供完整目标与范围。'
    },
    'recon': {
        name: '侦察专员',
        description: '负责信息收集、资产测绘与初始攻击面分析；要求主 Agent 在委派时提供完整目标（URL/IP:Port/域名+路径）与范围。'
    },
    'penetration': {
        name: '渗透测试专员',
        description: '授权范围内的漏洞验证、利用链构造、权限提升与影响证明；在得到侦察/情报输入后做深度利用与复现，并要求主 Agent 提供完整目标与范围。'
    },
    'attack-surface-enumeration': {
        name: '攻击面枚举专员',
        description: '基于侦察/情报输入，梳理服务、技术栈、依赖与潜在入口；输出结构化攻击面图谱与验证优先级，并要求主 Agent 提供完整目标与范围。'
    },
    'cleanup-rollback': {
        name: '清理与回滚专员',
        description: '为授权测试设计清理/回滚验证清单，确保最小残留与可审计可复核，并要求主 Agent 提供完整目标与变更上下文。'
    },
    'engagement-planning': {
        name: '参与规划专员',
        description: '定义参与范围、规则(ROE)与成功标准；产出迭代式测试蓝图与证据清单（不执行入侵），并要求主 Agent 提供完整目标与约束信息。'
    },
    'impact-exfiltration': {
        name: '影响与数据外泄证明专员',
        description: '以最小影响方式设计“业务影响/数据可达性”的证明方案；强调脱敏、最小化数据暴露与回滚，并要求主 Agent 提供完整目标与范围。'
    },
    'intel-collection': {
        name: '信息收集专员',
        description: '公开情报、资产指纹、泄露线索、目录与接口发现、第三方暴露面梳理；适合在授权范围内做大范围情报汇总，并要求主 Agent 提供完整目标与范围。'
    },
    'lateral-movement': {
        name: '内网横向专员',
        description: '已获得初始据点后的内网发现、凭证与会话利用、横向移动与权限维持思路（仅授权演练/渗透环境），并要求主 Agent 提供完整目标与网段范围。'
    },
    'opsec-evasion': {
        name: '运维安全与干扰最小化专员',
        description: '从测试噪声、可观测性、蓝队告警与回滚风险角度，设计“低干扰验证策略”和证据采集方式（不提供绕过手段），并要求主 Agent 提供完整目标与范围。'
    },
    'persistence-maintenance': {
        name: '持久化与后续通道专员',
        description: '评估授权环境下的持久化/维持访问思路、风险权衡与回滚验证；以最小影响方式证明可行性，并要求主 Agent 提供完整目标与边界。'
    },
    'privilege-escalation': {
        name: '权限提升专员',
        description: '在已获得初始访问/受限权限的前提下，评估权限提升可能性、证据需求与安全验证方法（仅限授权环境），并要求主 Agent 提供完整目标与当前权限上下文。'
    },
    'reporting-remediation': {
        name: '报告撰写与修复建议专员',
        description: '将已收集的证据汇总为可交付报告结构，并给出面向修复的建议与回归验证要点；要求主 Agent 提供完整目标与证据上下文。'
    },
    'vulnerability-triage': {
        name: '漏洞分诊专员',
        description: '基于攻击面与证据线索进行漏洞候选筛选、优先级排序与“验证路径”设计（以证据为中心，不直接武器化），并要求主 Agent 提供完整目标与输入证据。'
    }
};

function seedAgentField(id, field, raw) {
    const seedId = String(id || '').trim();
    const seed = SEED_AGENTS[seedId];
    if (!seed) return null;
    const value = String(raw || '').trim();
    if (value !== seed[field]) return null;
    const key = `agentsPage.seed.${seedId.replace(/-(.)/g, (_, c) => c.toUpperCase())}.${field}`;
    if (typeof window.t === 'function') {
        const translated = window.t(key);
        if (translated && translated !== key) return translated;
    }
    return null;
}

function agentDisplayName(agent, fallback) {
    if (!agent) return fallback || '';
    const seedTranslated = seedAgentField(agent.id, 'name', agent.name);
    if (seedTranslated) return seedTranslated;
    return String(agent.name || '').trim() || fallback || '';
}

function agentDisplayDescription(agent, fallback) {
    if (!agent) return fallback || '';
    const raw = String(agent.description || '').trim();
    if (!raw) return fallback || '';
    const seedTranslated = seedAgentField(agent.id, 'description', agent.description);
    if (seedTranslated) return seedTranslated;
    return raw;
}

let markdownAgentsEditingFilename = null;
let markdownAgentsEditingIsOrchestrator = false;

function bindAgentsMdListDelegation() {
    const listEl = document.getElementById('agents-md-list');
    if (!listEl || listEl.dataset.agentsClickBound === '1') return;
    listEl.dataset.agentsClickBound = '1';
    listEl.addEventListener('click', function (e) {
        var t = e.target;
        if (!t || !t.closest) return;
        var editBtn = t.closest('[data-action="edit-agent-md"]');
        var delBtn = t.closest('[data-action="delete-agent-md"]');
        if (editBtn) {
            var f = editBtn.getAttribute('data-agent-file');
            if (f) {
                try { editMarkdownAgent(decodeURIComponent(f)); } catch (err) { console.warn(err); }
            }
            return;
        }
        if (delBtn) {
            var f2 = delBtn.getAttribute('data-agent-file');
            if (f2) {
                try { deleteMarkdownAgent(decodeURIComponent(f2)); } catch (err2) { console.warn(err2); }
            }
        }
    });
}

async function loadMarkdownAgents() {
    const listEl = document.getElementById('agents-md-list');
    const dirEl = document.getElementById('agents-md-dir');
    if (!listEl) return;
    bindAgentsMdListDelegation();
    listEl.innerHTML = '<div class="loading-spinner">' + _agentsT('agentsPage.loading') + '</div>';
    try {
        const r = await apiFetch('/api/multi-agent/markdown-agents');
        const data = await r.json();
        if (!r.ok) {
            throw new Error(data.error || r.statusText);
        }
        if (dirEl) {
            const d = data.dir || '';
            dirEl.textContent = d ? (_agentsT('agentsPage.dirLabel') + ': ' + d) : '';
        }
        const agents = data.agents || [];
        if (agents.length === 0) {
            listEl.innerHTML = '<div class="empty-state">' + _agentsT('agentsPage.empty') + '</div>';
            return;
        }
        agents.sort(function (x, y) {
            var ox = x.is_orchestrator ? 1 : 0;
            var oy = y.is_orchestrator ? 1 : 0;
            return oy - ox;
        });
        listEl.innerHTML = agents.map(function (a) {
            const rawFn = a.filename || '';
            const fn = escapeHtml(rawFn);
            const id = escapeHtml(a.id || '');
            const name = escapeHtml(agentDisplayName(a, a.name || '') || _agentsT('agentsPage.noName'));
            const desc = escapeHtml(agentDisplayDescription(a, _agentsT('agentsPage.noDesc')));
            const orch = !!a.is_orchestrator;
            const badgeLabel = orch ? _agentsT('agentsPage.badgeOrchestrator') : _agentsT('agentsPage.badgeSub');
            const badgeClass = orch ? 'agent-role-badge agent-role-badge--orchestrator' : 'agent-role-badge agent-role-badge--sub';
            return (
                '<div class="skill-card">' +
                '<div class="skill-card-header">' +
                '<h3 class="skill-card-title">' + name + '<span class="' + badgeClass + '">' + escapeHtml(badgeLabel) + '</span></h3>' +
                '<div class="skill-card-description"><code>' + fn + '</code> · id: <code>' + id + '</code><br>' + desc + '</div>' +
                '</div>' +
                '<div class="skill-card-actions">' +
                '<button type="button" class="btn-secondary btn-small" data-action="edit-agent-md" data-agent-file="' + encodeURIComponent(rawFn) + '">' + escapeHtml(_agentsT('common.edit')) + '</button>' +
                '<button type="button" class="btn-secondary btn-small btn-danger" data-action="delete-agent-md" data-agent-file="' + encodeURIComponent(rawFn) + '">' + escapeHtml(_agentsT('common.delete')) + '</button>' +
                '</div></div>'
            );
        }).join('');
    } catch (e) {
        console.error(e);
        listEl.innerHTML = '<div class="empty-state">' + escapeHtml(e.message || String(e)) + '</div>';
        showNotification(_agentsT('agentsPage.loadFailed') + ': ' + e.message, 'error');
    }
}

document.addEventListener('languagechange', () => {
    // 语言切换后重新渲染 Agent 列表，让种子 frontmatter 名/描述按当前语言展示。
    const currentPage = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || '');
    if (currentPage === 'agents-management') {
        loadMarkdownAgents();
    }
});

function showAddMarkdownAgentModal() {
    if (typeof requirePermission === 'function' && !requirePermission('agents:write')) return;
    markdownAgentsEditingFilename = null;
    markdownAgentsEditingIsOrchestrator = false;
    const modal = document.getElementById('agent-md-modal');
    const title = document.getElementById('agent-md-modal-title');
    const row = document.getElementById('agent-md-filename-row');
    if (title) title.textContent = _agentsT('agentsPage.createTitle');
    if (row) row.style.display = '';
    document.getElementById('agent-md-filename-current').value = '';
    document.getElementById('agent-md-filename').value = '';
    document.getElementById('agent-md-filename').disabled = false;
    var roleEl = document.getElementById('agent-md-role');
    if (roleEl) roleEl.value = 'sub';
    document.getElementById('agent-md-id').value = '';
    document.getElementById('agent-md-name').value = '';
    document.getElementById('agent-md-description').value = '';
    document.getElementById('agent-md-tools').value = '';
    document.getElementById('agent-md-bind-role').value = '';
    document.getElementById('agent-md-max-iter').value = '0';
    document.getElementById('agent-md-instruction').value = '';
    openAppModal('agent-md-modal');
}

async function editMarkdownAgent(filename) {
    if (!filename) return;
    const title = document.getElementById('agent-md-modal-title');
    const row = document.getElementById('agent-md-filename-row');
    markdownAgentsEditingFilename = null;
    markdownAgentsEditingIsOrchestrator = false;
    if (title) title.textContent = _agentsT('agentsPage.editTitle');
    if (row) row.style.display = 'none';
    document.getElementById('agent-md-instruction').value = '';
    openAppModal('agent-md-modal', { focus: false });
    try {
        const r = await apiFetch('/api/multi-agent/markdown-agents/' + encodeURIComponent(filename));
        const data = await r.json();
        if (!r.ok) throw new Error(data.error || r.statusText);
        markdownAgentsEditingFilename = data.filename || filename;
        markdownAgentsEditingIsOrchestrator = !!data.is_orchestrator;
        deferModalContent(function () {
            document.getElementById('agent-md-filename-current').value = data.filename || filename;
            document.getElementById('agent-md-filename').value = data.filename || filename;
            document.getElementById('agent-md-filename').disabled = true;
            var roleEl2 = document.getElementById('agent-md-role');
            if (roleEl2) roleEl2.value = data.is_orchestrator ? 'orchestrator' : 'sub';
            document.getElementById('agent-md-id').value = data.id || '';
            document.getElementById('agent-md-name').value = data.name || '';
            document.getElementById('agent-md-description').value = data.description || '';
            document.getElementById('agent-md-tools').value = Array.isArray(data.tools) ? data.tools.join(', ') : '';
            document.getElementById('agent-md-bind-role').value = data.bind_role || '';
            document.getElementById('agent-md-max-iter').value = String(data.max_iterations != null ? data.max_iterations : 0);
            document.getElementById('agent-md-instruction').value = data.instruction || '';
            document.getElementById('agent-md-name')?.focus();
        });
    } catch (e) {
        closeMarkdownAgentModal();
        showNotification(_agentsT('agentsPage.loadOneFailed') + ': ' + e.message, 'error');
    }
}

function closeMarkdownAgentModal() {
    closeAppModal('agent-md-modal');
    markdownAgentsEditingFilename = null;
    markdownAgentsEditingIsOrchestrator = false;
}

function parseToolsInput(s) {
    if (!s || !String(s).trim()) return [];
    return String(s).split(/[,;|]/).map(function (x) { return x.trim(); }).filter(Boolean);
}

async function saveMarkdownAgent() {
    if (typeof requirePermission === 'function' && !requirePermission('agents:write')) return;
    const name = document.getElementById('agent-md-name').value.trim();
    if (!name) {
        showNotification(_agentsT('agentsPage.nameRequired'), 'error');
        return;
    }
    const roleSel = document.getElementById('agent-md-role');
    const roleVal = roleSel ? roleSel.value : 'sub';
    const fnDraft = (document.getElementById('agent-md-filename') && document.getElementById('agent-md-filename').value.trim().toLowerCase()) || '';
    const isOrchestratorAgent = markdownAgentsEditingIsOrchestrator ||
        roleVal === 'orchestrator' ||
        fnDraft === 'orchestrator.md';
    const instruction = document.getElementById('agent-md-instruction').value.trim();
    if (!isOrchestratorAgent && !instruction) {
        showNotification(_agentsT('agentsPage.instructionRequired'), 'error');
        return;
    }
    const body = {
        id: document.getElementById('agent-md-id').value.trim(),
        name: name,
        description: document.getElementById('agent-md-description').value.trim(),
        tools: parseToolsInput(document.getElementById('agent-md-tools').value),
        instruction: instruction,
        bind_role: document.getElementById('agent-md-bind-role').value.trim(),
        max_iterations: parseInt(document.getElementById('agent-md-max-iter').value, 10) || 0,
        kind: roleVal === 'orchestrator' ? 'orchestrator' : ''
    };
    const isEdit = !!markdownAgentsEditingFilename;
    let url;
    let method;
    if (isEdit) {
        url = '/api/multi-agent/markdown-agents/' + encodeURIComponent(markdownAgentsEditingFilename);
        method = 'PUT';
    } else {
        url = '/api/multi-agent/markdown-agents';
        method = 'POST';
        const fn = document.getElementById('agent-md-filename').value.trim();
        if (fn && !/^[a-zA-Z0-9][a-zA-Z0-9_.-]*\.md$/.test(fn)) {
            showNotification(_agentsT('agentsPage.filenameInvalid'), 'error');
            return;
        }
        body.filename = fn;
    }
    try {
        const r = await apiFetch(url, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        const data = await r.json().catch(function () { return {}; });
        if (!r.ok) throw new Error(data.error || r.statusText);
        showNotification(isEdit ? _agentsT('agentsPage.saveOk') : _agentsT('agentsPage.createOk'), 'success');
        closeMarkdownAgentModal();
        await loadMarkdownAgents();
    } catch (e) {
        showNotification(_agentsT('agentsPage.saveFailed') + ': ' + e.message, 'error');
    }
}

async function deleteMarkdownAgent(filename) {
    if (!filename) return;
    if (!confirm(_agentsT('agentsPage.deleteConfirm', { name: filename }))) return;
    try {
        const r = await apiFetch('/api/multi-agent/markdown-agents/' + encodeURIComponent(filename), { method: 'DELETE' });
        const data = await r.json().catch(function () { return {}; });
        if (!r.ok) throw new Error(data.error || r.statusText);
        showNotification(_agentsT('agentsPage.deleteOk'), 'success');
        await loadMarkdownAgents();
    } catch (e) {
        showNotification(_agentsT('agentsPage.deleteFailed') + ': ' + e.message, 'error');
    }
}
