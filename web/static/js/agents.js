// Multi-agent sub-agent Markdown (agents/*.md) management
function _agentsT(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

// Seed agent frontmatter (built-in .md names/descriptions in agents/*.md). Only when the value
// exactly matches the repo seed is it translated for display by id; names/descriptions the user
// edited themselves are shown at their original values.
const SEED_AGENTS = {
    'cyberstrike-deep': {
        name: 'Orchestrator / Coordinator Main Agent',
        description: 'The Deep orchestrator in multi-agent mode: collaborates with MCP tools and `task` sub-agents in already-authorized security scenarios, responsible for planning, delegation, consolidation, and delivery to the user; before dispatching, it must provide the complete target and scope to the sub-agent.'
    },
    'cyberstrike-plan-execute': {
        name: 'Plan-Execute Planning Main Agent',
        description: 'The planning/replanning-side main agent in plan_execute mode: decomposes the target and revises the plan, while the executor calls MCP tools to land it (does not use the Deep `task` sub-agent); each step in the plan must carry the complete target and scope, and the executor is forbidden from filling in URL/IP by guessing.'
    },
    'cyberstrike-supervisor': {
        name: 'Supervisor Overseer Main Agent',
        description: 'The coordinator in supervisor mode: delegates to specialist sub-agents via transfer, calling MCP itself where necessary; ends with exit when the goal is met (the runtime appends the specialist list and exit explanation at the end); before transfer, the complete target and scope must be provided.'
    },
    'recon': {
        name: 'Reconnaissance Specialist',
        description: 'Responsible for information gathering, asset mapping, and initial attack-surface analysis; requires the main agent to provide the complete target (URL/IP:Port/domain+path) and scope when delegating.'
    },
    'penetration': {
        name: 'Penetration Testing Specialist',
        description: 'Vulnerability verification within the authorized scope, exploit-chain construction, privilege escalation, and impact proof; performs deep exploitation and reproduction given reconnaissance/intelligence input, and requires the main agent to provide the complete target and scope.'
    },
    'attack-surface-enumeration': {
        name: 'Attack Surface Enumeration Specialist',
        description: 'Based on reconnaissance/intelligence input, organizes services, technology stacks, dependencies, and potential entry points; outputs a structured attack-surface map with verification priorities, and requires the main agent to provide the complete target and scope.'
    },
    'cleanup-rollback': {
        name: 'Cleanup and Rollback Specialist',
        description: 'Designs cleanup/rollback verification checklists for authorized tests, ensuring minimal residue that is auditable and reviewable, and requires the main agent to provide the complete target and change context.'
    },
    'engagement-planning': {
        name: 'Engagement Planning Specialist',
        description: 'Defines the engagement scope, rules of engagement (ROE), and success criteria; produces an iterative test blueprint and an evidence checklist (does not execute intrusion), and requires the main agent to provide the complete target and constraint information.'
    },
    'impact-exfiltration': {
        name: 'Impact and Data Exfiltration Proof Specialist',
        description: 'Designs "business impact / data reachability" proof schemes in a minimal-impact way; emphasizes data masking, minimal data exposure, and rollback, and requires the main agent to provide the complete target and scope.'
    },
    'intel-collection': {
        name: 'Intelligence Collection Specialist',
        description: 'Public intelligence, asset fingerprinting, leak leads, directory and interface discovery, and third-party exposure mapping; suited to broad-range intelligence consolidation within the authorized scope, and requires the main agent to provide the complete target and scope.'
    },
    'lateral-movement': {
        name: 'Internal Network Lateral Movement Specialist',
        description: 'Internal network discovery, credential and session exploitation, lateral movement, and access-maintenance ideas after an initial foothold is obtained (authorized exercises/pentest environments only), and requires the main agent to provide the complete target and network-segment scope.'
    },
    'opsec-evasion': {
        name: 'Operational Security and Interference Minimization Specialist',
        description: 'Designs "low-interference verification strategies" and evidence-collection methods from the angles of test noise, observability, blue-team alerting, and rollback risk (does not provide bypass means), and requires the main agent to provide the complete target and scope.'
    },
    'persistence-maintenance': {
        name: 'Persistence and Access Maintenance Specialist',
        description: 'Assesses persistence/access-maintenance ideas in the authorized environment, risk weighing, and rollback verification; proves feasibility in a minimal-impact way, and requires the main agent to provide the complete target and boundaries.'
    },
    'privilege-escalation': {
        name: 'Privilege Escalation Specialist',
        description: 'Given initial access/restricted privileges already obtained, assesses privilege-escalation possibilities, evidence needs, and safe verification methods (authorized environments only), and requires the main agent to provide the complete target and current-privilege context.'
    },
    'reporting-remediation': {
        name: 'Report Writing and Remediation Specialist',
        description: 'Consolidates the collected evidence into a deliverable report structure, and gives remediation-oriented recommendations and regression-verification essentials; requires the main agent to provide the complete target and evidence context.'
    },
    'vulnerability-triage': {
        name: 'Vulnerability Triage Specialist',
        description: 'Based on the attack surface and evidence leads, performs vulnerability candidate filtering, priority ranking, and "verification path" design (evidence-centered, not directly weaponizing), and requires the main Agent to provide the complete target and input evidence.'
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
    // After a language switch, re-render the Agent list so seed frontmatter names/descriptions display in the current language.
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
