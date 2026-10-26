// Skills管理相关功能
function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

// 仓库内置技能包的种子描述（skills/*/SKILL.md frontmatter）。仅与种子值精确
// 匹配时按 id 翻译展示；用户自行编辑的描述按原值展示。
const SEED_SKILL_DESCRIPTIONS = {
    'active-directory-attack': "内网域攻击:BloodHound,Kerberoast,ADCS ESC1/ESC8,NTLM Relay,Coerce,DACL,DCSync,Zerologon/NoPac/PrintNightmare,mitm6,LLMNR,Linux内网。Use when attacking Active Directory, ADCS, NTLM relay, or internal domain.",
    'ai-llm-app-attack': "AI/LLM应用攻击:提示注入,Agent工具滥用RCE,RAG投毒,MCP供应链,torch.load pickle RCE。Use when testing LLM apps, agents, RAG, MCP plugins, or AI model file risks.",
    'attack-surface-recon': "侦察/攻击面测绘:被动whois/amass/crt.sh/FOFA/Shodan,主动subfinder/httpx/naabu/katana/nuclei,DNS地域/CDN/Nginx catch-all/宝塔/UniApp指纹。开局第一动作,认知写入项目黑板。Use when starting recon, asset mapping, fingerprinting, or CDN/DNS bypass discovery.",
    'binary-mobile-reversing': "APK/EXE/二进制:UniApp/DCloud/Flutter逆向,证书固定绕过,导出组件,内存破坏exploit链,IoT固件。Use when reversing APK/EXE, UniApp/Flutter, native .so, or memory-corruption exploits.",
    'blockchain-contract-attack': "区块链/智能合约:Etherscan,slither/mythril,重入/访问控制/预言机/闪电贷,跨链桥,RPC暴露。Use when auditing smart contracts, DeFi, or blockchain attack surfaces.",
    'capability-primitive-search': "能力原语+状态空间搜索:read/write/exec/ssrf等原语凑RCE等式A-F,低危映射,正反向搜索,跨域兑现。Use when no single RCE, chaining low-severity vulns, or deriving novel attack chains.",
    'cloud-attack-methods': "云攻击:元数据API,S3/K8s,AWS/Azure/GCP身份提权,MinIO矩阵,阿里云FC,ChengZi SDK解密。Use when attacking cloud metadata, IAM, K8s, MinIO, Aliyun FC, or cloud post-ex.",
    'component-vuln-intel': "联网情报收集:识别组件后必做CVE/搜索引擎/中文社区/GitHub PoC/资产引擎/即时情报/依赖扩展+受阻换路。Use when a framework/component/version is identified and must search before exploit.",
    'cyberstrike-eino-demo': "满配示例技能包：SKILL.md + scripts/、references/、assets/ 等可选目录；验证 Eino skill 与 HTTP 包内路径（仅授权安全测试与教学）。",
    'initial-access-phishing': "初始访问/钓鱼/社工:凭据喷洒,AiTM,设备码,OAuth同意钓鱼,载荷,vishing。Use when needing initial access, phishing, AiTM, device code, or social engineering.",
    'pentest-agent-os': "渗透测试Agent操作系统。把渗透抽象成状态空间搜索:不预设固定路径,以项目黑板 Fact 图 (upsert_project_fact)+漏洞记录沉淀认知,路径从已验证事实上涌现。覆盖全杀伤链攻击手法 (联网情报/Web/认证/服务端/源码/社工/后渗透/二进制/内网域/云/区块链/AI/无线/硬件)+0day+ 组合拳+代理自举。核心:全网搜不到洞时现场推导独属于目标的攻击链。本文件为套件索引。 Use when starting a full-chain pentest engagement or needing the skill map for this suite.",
    'pentest-blackboard': "CyberStrikeAI 项目黑板：跨会话 Fact 图（SQLite）+ upsert_project_fact/record_vulnerability 边渗透边记录节奏、关系边 links、confidence、与多代理协调落库。Use when managing project facts, blackboard index, writing evidence, or avoiding context-loss after compression.",
    'pentest-output-standards': "输出规范:中文分析,思维链,漏洞报告模板,负结果,黑板状态总览,改动台账,死锁突破。 Use when reporting findings, maintaining change ledger, or formatting pentest output.",
    'pentest-verification': "验证铁律:搜索≠漏洞,confirmed Fact须证据,tentative表线索,禁止空泛推测,负结果也落库, 想象力拉满+单步验证零容忍。Use when writing project facts, validating findings, or avoiding hallucination.",
    'post-exploitation': "后渗透/提权+凭据破解+密码学:反弹shell,Linux/Windows提权,横向,隧道,免杀,C2持久化,hashcat/Padding Oracle/hash长度扩展。Use when post-exploitation, privilege escalation, lateral movement, or cracking crypto.",
    'proxy-tool-bootstrap': "自找代理+工具自举:SOCKS5/HTTP/Tor换路序列,工具Python自举,字典自生成,OOB基础设施。Use when blocked by 403/429/WAF/timeout, missing tools, or needing OOB confirmation.",
    'redteam-opsec': "OPSEC隐蔽作战纪律:IP黑名单绕过,速率时序,流量混淆,最小足迹,反取证,渐进暴露。Use when maintaining stealth, bypassing IP bans, or planning covert red-team ops.",
    'source-code-hunting': "源码狩猎:.git泄露,危险函数grep,JS RC4解混淆,semgrep/CodeQL,trufflehog,patch diff,供应链/CI。Use when hunting source leaks, secrets, JS deobfuscation, or supply-chain issues.",
    'specialized-attack-playbooks': "专题实战利用:GoEdge私钥导出,灰产CDN取证,ARP MITM,CDN→S3 STS链,宝塔+UniApp,AI IDE API反代,OCS+MinIO;含references/scripts支持文件索引。Use when applying specialized playbooks for GoEdge, CDN, ARP MITM, BT Panel, OCS, MinIO.",
    'unlimited-attack-scope': "不设限原则:搜索/验证/串联/记录一切,全领域适用,被拦换路,不达不休。Use when reminding scope is unlimited across Web/APK/cloud/AI/wireless/social eng.",
    'web-attack-methods': "Web全栈攻击:SQLi/命令注入/SSTI/XSS/SSRF/NoSQL,认证JWT/OAuth/SAML,LFI/上传,Tomcat/WS/STOMP/XFF/PATH_INFO/CDN502/网宿JS挑战绕过。Use when testing Web injection, auth bypass, server-side, WAF/CDN bypass.",
    'wireless-hardware-attack': "无线/硬件:WiFi PMKID/WPS/Evil Twin,BLE,Zigbee,NFC,SDR,UART/JTAG/SPI,侧信道,故障注入。Use when attacking WiFi, BLE, RFID, SDR, or hardware interfaces.",
    'zero-day-discovery': "0day自主发现引擎:变体分析/补丁间隙/差分/Fuzzing/污点推理/N-day武器化/猎人思维。Use when public vulns not found and need to discover 0day or weaponize N-day."
};

function skillDisplayDescription(skill, fallback) {
    if (!skill) return fallback || '';
    const id = String(skill.id || skill.name || '').trim();
    const raw = String(skill.description || '').trim();
    if (!raw) return fallback || '';
    if (SEED_SKILL_DESCRIPTIONS[id] === raw) {
        const key = `skills.seedDescriptions.${id}`;
        if (typeof window.t === 'function') {
            const translated = window.t(key);
            if (translated && translated !== key) return translated;
        }
    }
    return raw;
}

let skillsList = [];
let currentEditingSkillName = null;
let skillModalAddMode = true;
let skillActivePath = 'SKILL.md';
let skillFileDirty = false;
let skillDescriptionDirty = false;
let skillPackageFiles = [];
let skillModalControlsWired = false;
let isSavingSkill = false; // 防止重复提交
let skillsSearchKeyword = '';
let skillsSearchTimeout = null; // 搜索防抖定时器
let skillsAutoRefreshTimer = null;
let isAutoRefreshingSkills = false;
const SKILLS_AUTO_REFRESH_INTERVAL_MS = 5000;
let skillsPagination = {
    currentPage: 1,
    pageSize: 20, // 每页20条（默认值，实际从localStorage读取）
    total: 0
};
let skillsStats = {
    total: 0,
    totalCalls: 0,
    totalSuccess: 0,
    totalFailed: 0,
    skillsDir: '',
    stats: []
};

function isSkillsManagementPageActive() {
    const page = document.getElementById('page-skills-management');
    return !!(page && page.classList.contains('active'));
}

function shouldSkipSkillsAutoRefresh() {
    if (isSavingSkill || currentEditingSkillName) {
        return true;
    }

    const modal = document.getElementById('skill-modal');
    if (modal && isAppModalOpen('skill-modal')) {
        return true;
    }

    const searchInput = document.getElementById('skills-search');
    if (skillsSearchKeyword || (searchInput && searchInput.value.trim())) {
        return true;
    }

    return false;
}

function startSkillsAutoRefresh() {
    if (skillsAutoRefreshTimer) return;

    skillsAutoRefreshTimer = setInterval(async () => {
        if (!isSkillsManagementPageActive() || shouldSkipSkillsAutoRefresh()) {
            return;
        }
        if (isAutoRefreshingSkills) {
            return;
        }

        isAutoRefreshingSkills = true;
        try {
            await loadSkills(skillsPagination.currentPage, skillsPagination.pageSize);
        } finally {
            isAutoRefreshingSkills = false;
        }
    }, SKILLS_AUTO_REFRESH_INTERVAL_MS);
}

// 获取保存的每页显示数量
function getSkillsPageSize() {
    try {
        const saved = localStorage.getItem('skillsPageSize');
        if (saved) {
            const size = parseInt(saved);
            if ([10, 20, 50, 100].includes(size)) {
                return size;
            }
        }
    } catch (e) {
        console.warn('无法从localStorage读取分页设置:', e);
    }
    return 20; // 默认20
}

// 初始化分页设置
function initSkillsPagination() {
    const savedPageSize = getSkillsPageSize();
    skillsPagination.pageSize = savedPageSize;
}

// 加载skills列表（支持分页）
async function loadSkills(page = 1, pageSize = null) {
    try {
        // 如果没有指定pageSize，使用保存的值或默认值
        if (pageSize === null) {
            pageSize = getSkillsPageSize();
        }
        
        // 更新分页状态（确保使用正确的pageSize）
        skillsPagination.currentPage = page;
        skillsPagination.pageSize = pageSize;
        
        // 清空搜索关键词（正常分页加载时）
        skillsSearchKeyword = '';
        const searchInput = document.getElementById('skills-search');
        if (searchInput) {
            searchInput.value = '';
        }
        
        // 构建URL（支持分页）
        const offset = (page - 1) * pageSize;
        const url = `/api/skills?limit=${pageSize}&offset=${offset}`;
        
        const response = await apiFetch(url);
        if (!response.ok) {
            throw new Error(_t('skills.loadListFailed'));
        }
        const data = await response.json();
        skillsList = data.skills || [];
        skillsPagination.total = data.total || 0;
        
        renderSkillsList();
        renderSkillsPagination();
        updateSkillsManagementStats();
    } catch (error) {
        console.error('加载skills列表失败:', error);
        showNotification(_t('skills.loadListFailed') + ': ' + error.message, 'error');
        const skillsListEl = document.getElementById('skills-list');
        if (skillsListEl) {
            skillsListEl.innerHTML = '<div class="empty-state">' + _t('skills.loadFailedShort') + ': ' + escapeHtml(error.message) + '</div>';
        }
    }
}

// 渲染skills列表
function renderSkillsList() {
    const skillsListEl = document.getElementById('skills-list');
    if (!skillsListEl) return;

    // 后端已经完成搜索过滤，直接使用skillsList
    const filteredSkills = skillsList;

    if (filteredSkills.length === 0) {
        skillsListEl.innerHTML = '<div class="empty-state">' + 
            (skillsSearchKeyword ? _t('skills.noMatch') : _t('skills.noSkills')) + 
            '</div>';
        // 搜索时隐藏分页
        const paginationContainer = document.getElementById('skills-pagination');
        if (paginationContainer) {
            paginationContainer.innerHTML = '';
        }
        return;
    }

    skillsListEl.innerHTML = filteredSkills.map(skill => {
        const sid = skill.id || skill.name || '';
        const ver = skill.version ? _t('skills.cardVersion', { version: skill.version }) : '';
        const sc = typeof skill.script_count === 'number' && skill.script_count > 0
            ? _t('skills.cardScripts', { count: skill.script_count })
            : '';
        const fc = typeof skill.file_count === 'number' && skill.file_count > 0
            ? _t('skills.cardFiles', { count: skill.file_count })
            : '';
        const metaItems = [ver, fc, sc].filter(Boolean);
        const tags = Array.isArray(skill.tags) ? skill.tags.filter(Boolean) : [];
        const visibleTags = tags.slice(0, 4);
        const hiddenTagCount = Math.max(0, tags.length - visibleTags.length);
        const tagsHtml = visibleTags.length
            ? `<div class="skill-card-tags">${visibleTags.map(tag => `<span>${escapeHtml(tag)}</span>`).join('')}${hiddenTagCount ? `<span>+${hiddenTagCount}</span>` : ''}</div>`
            : '';
        return `
            <div class="skill-card">
                <div class="skill-card-body">
                    <div class="skill-card-header">
                        <div class="skill-card-title-row">
                            <h3 class="skill-card-title">${escapeHtml(skill.name || sid)}</h3>
                            ${metaItems.length ? `<div class="skill-card-meta">${metaItems.map(item => `<span>${escapeHtml(item)}</span>`).join('')}</div>` : ''}
                        </div>
                        <div class="skill-card-description">${escapeHtml(skillDisplayDescription(skill, _t('skills.noDescription')))}</div>
                        ${tagsHtml}
                    </div>
                    <div class="skill-card-actions">
                        <button type="button" class="btn-secondary btn-small" data-skill-view="${escapeAttr(sid)}">${_t('common.view')}</button>
                        <button type="button" class="btn-secondary btn-small" data-skill-edit="${escapeAttr(sid)}">${_t('common.edit')}</button>
                        <button type="button" class="btn-secondary btn-small btn-danger" data-skill-delete="${escapeAttr(sid)}">${_t('common.delete')}</button>
                    </div>
                </div>
            </div>
        `;
    }).join('');

    skillsListEl.querySelectorAll('[data-skill-view]').forEach(btn => {
        btn.addEventListener('click', () => viewSkill(btn.getAttribute('data-skill-view')));
    });
    skillsListEl.querySelectorAll('[data-skill-edit]').forEach(btn => {
        btn.addEventListener('click', () => editSkill(btn.getAttribute('data-skill-edit')));
    });
    skillsListEl.querySelectorAll('[data-skill-delete]').forEach(btn => {
        btn.addEventListener('click', () => deleteSkill(btn.getAttribute('data-skill-delete')));
    });
    
    // 确保列表容器可以滚动，分页栏可见
    // 使用 setTimeout 确保 DOM 更新完成后再检查
    setTimeout(() => {
        const paginationContainer = document.getElementById('skills-pagination');
        if (paginationContainer && !skillsSearchKeyword) {
            // 确保分页栏可见
            paginationContainer.style.display = 'block';
            paginationContainer.style.visibility = 'visible';
        }
    }, 0);
}

// 渲染分页组件（参考MCP管理页面样式）
function renderSkillsPagination() {
    const paginationContainer = document.getElementById('skills-pagination');
    if (!paginationContainer) return;
    
    const total = skillsPagination.total;
    const pageSize = skillsPagination.pageSize;
    const currentPage = skillsPagination.currentPage;
    const totalPages = Math.ceil(total / pageSize);
    
    // 即使只有一页也显示分页信息（参考MCP样式）
    if (total === 0) {
        paginationContainer.innerHTML = '';
        if (typeof initPaginationSelects === 'function') initPaginationSelects(paginationContainer);
        return;
    }
    
    // 计算显示范围
    const start = total === 0 ? 0 : (currentPage - 1) * pageSize + 1;
    const end = total === 0 ? 0 : Math.min(currentPage * pageSize, total);
    
    let paginationHTML = '<div class="pagination">';
    
    const paginationShowText = _t('skillsPage.paginationShow', { start, end, total });
    const perPageLabelText = _t('skillsPage.perPageLabel');
    const firstPageText = _t('skillsPage.firstPage');
    const prevPageText = _t('skillsPage.prevPage');
    const pageOfText = _t('skillsPage.pageOf', { current: currentPage, total: totalPages || 1 });
    const nextPageText = _t('skillsPage.nextPage');
    const lastPageText = _t('skillsPage.lastPage');
    // 左侧：显示范围信息和每页数量选择器（参考MCP样式）
    paginationHTML += `
        <div class="pagination-info">
            <span>${escapeHtml(paginationShowText)}</span>
            <label class="pagination-page-size">
                ${escapeHtml(perPageLabelText)}
                <select id="skills-page-size-pagination" onchange="changeSkillsPageSize()">
                    <option value="10" ${pageSize === 10 ? 'selected' : ''}>10</option>
                    <option value="20" ${pageSize === 20 ? 'selected' : ''}>20</option>
                    <option value="50" ${pageSize === 50 ? 'selected' : ''}>50</option>
                    <option value="100" ${pageSize === 100 ? 'selected' : ''}>100</option>
                </select>
            </label>
        </div>
    `;
    
    // 右侧：分页按钮（参考MCP样式：首页、上一页、第X/Y页、下一页、末页）
    paginationHTML += `
        <div class="pagination-controls">
            <button class="btn-secondary" onclick="loadSkills(1, ${pageSize})" ${currentPage === 1 || total === 0 ? 'disabled' : ''}>${escapeHtml(firstPageText)}</button>
            <button class="btn-secondary" onclick="loadSkills(${currentPage - 1}, ${pageSize})" ${currentPage === 1 || total === 0 ? 'disabled' : ''}>${escapeHtml(prevPageText)}</button>
            <span class="pagination-page">${escapeHtml(pageOfText)}</span>
            <button class="btn-secondary" onclick="loadSkills(${currentPage + 1}, ${pageSize})" ${currentPage >= totalPages || total === 0 ? 'disabled' : ''}>${escapeHtml(nextPageText)}</button>
            <button class="btn-secondary" onclick="loadSkills(${totalPages || 1}, ${pageSize})" ${currentPage >= totalPages || total === 0 ? 'disabled' : ''}>${escapeHtml(lastPageText)}</button>
        </div>
    `;
    
    paginationHTML += '</div>';
    
    paginationContainer.innerHTML = paginationHTML;
    if (typeof initPaginationSelects === 'function') initPaginationSelects(paginationContainer);
    
    // 确保分页组件与列表内容区域对齐（不包括滚动条）
    function alignPaginationWidth() {
        const skillsList = document.getElementById('skills-list');
        if (skillsList && paginationContainer) {
            // 确保分页容器始终可见
            paginationContainer.style.display = '';
            paginationContainer.style.visibility = 'visible';
            paginationContainer.style.opacity = '1';
            
            // 获取列表的实际内容宽度（不包括滚动条）
            const listClientWidth = skillsList.clientWidth; // 可视区域宽度（不包括滚动条）
            const listScrollHeight = skillsList.scrollHeight; // 内容总高度
            const listClientHeight = skillsList.clientHeight; // 可视区域高度
            const hasScrollbar = listScrollHeight > listClientHeight;
            
            // 如果列表有垂直滚动条，分页组件应该与列表内容区域对齐（clientWidth）
            // 如果没有滚动条，使用100%宽度
            if (hasScrollbar && listClientWidth > 0) {
                // 分页组件应该与列表内容区域对齐，不包括滚动条
                paginationContainer.style.width = `${listClientWidth}px`;
            } else {
                // 如果没有滚动条，使用100%宽度
                paginationContainer.style.width = '100%';
            }
        }
    }
    
    // 立即执行一次
    alignPaginationWidth();
    
    // 监听窗口大小变化和列表内容变化
    const resizeObserver = new ResizeObserver(() => {
        alignPaginationWidth();
    });
    
    const skillsList = document.getElementById('skills-list');
    if (skillsList) {
        resizeObserver.observe(skillsList);
    }
    
    // 确保分页容器始终可见（防止被隐藏）
    paginationContainer.style.display = 'block';
    paginationContainer.style.visibility = 'visible';
}

// 改变每页显示数量
async function changeSkillsPageSize() {
    const pageSizeSelect = document.getElementById('skills-page-size-pagination');
    if (!pageSizeSelect) return;
    
    const newPageSize = parseInt(pageSizeSelect.value);
    if (isNaN(newPageSize) || newPageSize <= 0) return;
    
    // 保存到localStorage
    try {
        localStorage.setItem('skillsPageSize', newPageSize.toString());
    } catch (e) {
        console.warn('无法保存分页设置到localStorage:', e);
    }
    
    // 更新分页状态
    skillsPagination.pageSize = newPageSize;
    
    // 重新计算当前页（确保不超出范围）
    const totalPages = Math.ceil(skillsPagination.total / newPageSize);
    const currentPage = Math.min(skillsPagination.currentPage, totalPages || 1);
    skillsPagination.currentPage = currentPage;
    
    // 重新加载数据
    await loadSkills(currentPage, newPageSize);
}

// 更新skills管理统计信息
function updateSkillsManagementStats() {
    const statsEl = document.getElementById('skills-management-stats');
    if (!statsEl) return;

    const totalEl = statsEl.querySelector('.skill-stat-value');
    if (totalEl) {
        totalEl.textContent = skillsPagination.total;
    }

    const currentCountEl = document.getElementById('skills-current-count');
    if (currentCountEl) {
        currentCountEl.textContent = skillsList.length;
    }

    const fileCountEl = document.getElementById('skills-file-count');
    if (fileCountEl) {
        const fileCount = skillsList.reduce((sum, skill) => {
            return sum + (typeof skill.file_count === 'number' ? skill.file_count : 0);
        }, 0);
        fileCountEl.textContent = fileCount;
    }

    const scriptCountEl = document.getElementById('skills-script-count');
    if (scriptCountEl) {
        const scriptCount = skillsList.reduce((sum, skill) => {
            return sum + (typeof skill.script_count === 'number' ? skill.script_count : 0);
        }, 0);
        scriptCountEl.textContent = scriptCount;
    }
}

// 搜索skills
function handleSkillsSearchInput() {
    clearTimeout(skillsSearchTimeout);
    skillsSearchTimeout = setTimeout(() => {
        searchSkills();
    }, 300);
}

async function searchSkills() {
    const searchInput = document.getElementById('skills-search');
    if (!searchInput) return;
    
    skillsSearchKeyword = searchInput.value.trim();
    const clearBtn = document.getElementById('skills-search-clear');
    if (clearBtn) {
        clearBtn.style.display = skillsSearchKeyword ? 'block' : 'none';
    }
    
    if (skillsSearchKeyword) {
        // 有搜索关键词时，使用后端搜索API（加载所有匹配结果，不分页）
        try {
            const response = await apiFetch(`/api/skills?search=${encodeURIComponent(skillsSearchKeyword)}&limit=10000&offset=0`);
            if (!response.ok) {
                throw new Error(_t('skills.loadListFailed'));
            }
            const data = await response.json();
            skillsList = data.skills || [];
            skillsPagination.total = data.total || 0;
            renderSkillsList();
            // 搜索时隐藏分页
            const paginationContainer = document.getElementById('skills-pagination');
            if (paginationContainer) {
                paginationContainer.innerHTML = '';
            }
            // 更新统计信息（显示搜索结果数量）
            updateSkillsManagementStats();
        } catch (error) {
            console.error('搜索skills失败:', error);
            showNotification(_t('skills.searchFailed') + ': ' + error.message, 'error');
        }
    } else {
        // 没有搜索关键词时，恢复分页加载
        await loadSkills(1, skillsPagination.pageSize);
    }
}

// 清除skills搜索
function clearSkillsSearch() {
    const searchInput = document.getElementById('skills-search');
    if (searchInput) {
        searchInput.value = '';
    }
    skillsSearchKeyword = '';
    const clearBtn = document.getElementById('skills-search-clear');
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    // 恢复分页加载
    loadSkills(1, skillsPagination.pageSize);
}

// 刷新skills
async function refreshSkills() {
    await loadSkills(skillsPagination.currentPage, skillsPagination.pageSize);
    showNotification(_t('skills.refreshed'), 'success');
}

// 显示添加skill模态框
function wireSkillModalOnce() {
    if (skillModalControlsWired) return;
    skillModalControlsWired = true;
    const addTa = document.getElementById('skill-content-add');
    const edTa = document.getElementById('skill-content');
    const descriptionInput = document.getElementById('skill-description');
    if (descriptionInput) descriptionInput.addEventListener('input', () => { skillDescriptionDirty = true; });
    if (addTa) addTa.addEventListener('input', () => { if (skillModalAddMode) skillFileDirty = true; });
    if (edTa) edTa.addEventListener('input', () => { if (!skillModalAddMode) skillFileDirty = true; });
    const nb = document.getElementById('skill-new-file-btn');
    if (nb) {
        nb.addEventListener('click', () => {
            if (!currentEditingSkillName) return;
            const inp = document.getElementById('skill-new-file-path');
            const p = (inp && inp.value || '').trim();
            if (!p) {
                showNotification(_t('skillModal.newFilePathRequired'), 'error');
                return;
            }
            if (p.includes('..') || p.startsWith('/')) {
                showNotification(_t('skillModal.newFilePathInvalid'), 'error');
                return;
            }
            selectSkillPackageFile(currentEditingSkillName, p, { force: true, freshContent: '' });
            if (inp) inp.value = '';
        });
    }
}

function showAddSkillModal() {
    if (typeof requirePermission === 'function' && !requirePermission('skills:write')) return;
    wireSkillModalOnce();
    const modal = document.getElementById('skill-modal');
    if (!modal) return;

    skillModalAddMode = true;
    skillDescriptionDirty = false;
    document.getElementById('skill-description').disabled = false;
    skillFileDirty = false;
    skillActivePath = 'SKILL.md';
    skillPackageFiles = [];
    const pkg = document.getElementById('skill-package-editor');
    const addEd = document.getElementById('skill-add-editor');
    if (pkg) pkg.style.display = 'none';
    if (addEd) addEd.style.display = 'block';

    document.getElementById('skill-modal-title').textContent = _t('skills.addSkill');
    document.getElementById('skill-name').value = '';
    document.getElementById('skill-name').disabled = false;
    document.getElementById('skill-description').value = '';
    const addTa = document.getElementById('skill-content-add');
    if (addTa) addTa.value = '';

    openAppModal('skill-modal');
}

function skillPackagePathDepth(path) {
    if (!path) return 0;
    return (String(path).replace(/\/$/, '').match(/\//g) || []).length;
}

function renderSkillPackageTree() {
    const el = document.getElementById('skill-package-tree');
    if (!el) return;
    const rows = (skillPackageFiles || []).filter(f => f.path && f.path !== '.').sort((a, b) =>
        String(a.path).localeCompare(String(b.path)));
    if (rows.length === 0) {
        el.innerHTML = '<div class="empty-state" style="padding:8px;">' + escapeHtml(_t('skillModal.noPackageFiles')) + '</div>';
        return;
    }
    el.innerHTML = rows.map(f => {
        const path = f.path || '';
        const indent = 8 + skillPackagePathDepth(path) * 14;
        if (f.is_dir) {
            const dirLabel = path.endsWith('/') ? path : path + '/';
            return `<div class="skill-tree-row skill-tree-dir" style="padding-left:${indent}px" title="${escapeHtml(_t('skillModal.folderHint'))}">` +
                `<span class="skill-tree-icon" aria-hidden="true">📁</span>` +
                `<span class="skill-tree-label">${escapeHtml(dirLabel)}</span>` +
                `</div>`;
        }
        const selected = path === skillActivePath ? ' is-selected' : '';
        return `<div class="skill-tree-row skill-tree-file${selected}" style="padding-left:${indent}px" data-skill-tree-path="${escapeAttr(path)}" title="${escapeAttr(_t('skillModal.clickToEdit'))}">` +
            `<span class="skill-tree-icon" aria-hidden="true">📄</span>` +
            `<span class="skill-tree-label">${escapeHtml(path)}</span>` +
            `</div>`;
    }).join('');
    el.querySelectorAll('[data-skill-tree-path]').forEach(node => {
        node.addEventListener('click', () => {
            const p = node.getAttribute('data-skill-tree-path');
            if (p) selectSkillPackageFile(currentEditingSkillName, p, {});
        });
    });
}

async function selectSkillPackageFile(skillId, path, opts) {
    const force = opts && opts.force;
    const freshContent = opts && Object.prototype.hasOwnProperty.call(opts, 'freshContent')
        ? opts.freshContent
        : null;
    if (path !== 'SKILL.md' && skillDescriptionDirty) {
        showNotification(_t('skillModal.saveDescriptionFirst'), 'error');
        return;
    }
    if (!force && skillFileDirty) {
        if (!confirm(_t('skillModal.unsavedSwitch'))) {
            return;
        }
    }
    skillActivePath = path;
    const descriptionInput = document.getElementById('skill-description');
    if (descriptionInput) descriptionInput.disabled = path !== 'SKILL.md';
    const label = document.getElementById('skill-active-path');
    if (label) label.textContent = path;
    const hint = document.getElementById('skill-body-hint-edit');
    if (hint) hint.style.display = path === 'SKILL.md' ? 'block' : 'none';
    const ta = document.getElementById('skill-content');
    if (!ta) return;

    if (freshContent !== null) {
        ta.value = freshContent;
        skillFileDirty = true;
        renderSkillPackageTree();
        return;
    }

    try {
        if (path === 'SKILL.md') {
            const response = await apiFetch(`/api/skills/${encodeURIComponent(skillId)}?depth=full`);
            if (!response.ok) throw new Error(_t('skills.loadDetailFailed'));
            const data = await response.json();
            const skill = data.skill;
            ta.value = skill && skill.content != null ? skill.content : '';
        } else {
            const response = await apiFetch(`/api/skills/${encodeURIComponent(skillId)}/file?path=${encodeURIComponent(path)}`);
            if (!response.ok) throw new Error(_t('skills.loadDetailFailed'));
            const data = await response.json();
            ta.value = data.content != null ? data.content : '';
        }
        skillFileDirty = false;
        renderSkillPackageTree();
    } catch (e) {
        console.error(e);
        showNotification(_t('skills.loadDetailFailed') + ': ' + e.message, 'error');
    }
}

// 编辑skill
async function editSkill(skillId) {
    wireSkillModalOnce();
    const modal = document.getElementById('skill-modal');
    if (!modal) return;
    skillModalAddMode = false;
    skillDescriptionDirty = false;
    document.getElementById('skill-description').disabled = false;
    skillFileDirty = false;
    skillActivePath = 'SKILL.md';
    const pkg = document.getElementById('skill-package-editor');
    const addEd = document.getElementById('skill-add-editor');
    if (pkg) pkg.style.display = 'block';
    if (addEd) addEd.style.display = 'none';
    document.getElementById('skill-modal-title').textContent = _t('skills.editSkill');
    document.getElementById('skill-name').value = '';
    document.getElementById('skill-name').disabled = true;
    document.getElementById('skill-description').value = '';
    const ta = document.getElementById('skill-content');
    if (ta) ta.value = '';
    openAppModal('skill-modal', { focus: false });
    try {
        const [detailRes, filesRes] = await Promise.all([
            apiFetch(`/api/skills/${encodeURIComponent(skillId)}?depth=full`),
            apiFetch(`/api/skills/${encodeURIComponent(skillId)}/files`)
        ]);
        if (!detailRes.ok) {
            throw new Error(_t('skills.loadDetailFailed'));
        }
        const data = await detailRes.json();
        const skill = data.skill;
        let files = [];
        if (filesRes.ok) {
            const fd = await filesRes.json();
            files = fd.files || [];
        }
        currentEditingSkillName = skillId;
        deferModalContent(() => {
            document.getElementById('skill-name').value = skill.id || skillId;
            document.getElementById('skill-description').value = skill.description || '';
            skillPackageFiles = files;
            renderSkillPackageTree();
            if (ta) ta.value = skill.content || '';
            const hint = document.getElementById('skill-body-hint-edit');
            if (hint) hint.style.display = 'block';
            document.getElementById('skill-name')?.focus();
        });
    } catch (error) {
        closeSkillModal();
        console.error('加载skill详情失败:', error);
        showNotification(_t('skills.loadDetailFailed') + ': ' + error.message, 'error');
    }
}

// 查看 skill：先摘要再按需拉全文（与多代理 Eino skill 渐进披露思路一致）
async function viewSkill(skillId) {
    try {
        const sumRes = await apiFetch(`/api/skills/${encodeURIComponent(skillId)}?depth=summary`);
        if (!sumRes.ok) {
            throw new Error(_t('skills.loadDetailFailed'));
        }
        const sumData = await sumRes.json();
        const sumSkill = sumData.skill;

        const modal = document.createElement('div');
        modal.className = 'modal';
        modal.id = 'skill-view-modal';
        const viewTitle = _t('skills.viewSkillTitle', { name: sumSkill.name || skillId });
        const descLabel = _t('skills.descriptionLabel');
        const pathLabel = _t('skills.pathLabel');
        const modTimeLabel = _t('skills.modTimeLabel');
        const contentLabel = _t('skills.contentLabel');
        const closeBtn = _t('common.close');
        const editBtn = _t('common.edit');
        const loadFullLabel = _t('skills.loadFullBody');
        const scriptsLabel = _t('skills.scriptsHeading');

        let scriptsBlock = '';
        if (Array.isArray(sumSkill.scripts) && sumSkill.scripts.length > 0) {
            const lines = sumSkill.scripts.map(s => {
                const rel = escapeHtml(s.rel_path || s.RelPath || '');
                const dn = escapeHtml(s.description || s.Description || '');
                return `<li><code>${rel}</code>${dn ? ' — ' + dn : ''}</li>`;
            }).join('');
            scriptsBlock = `<div style="margin-bottom: 16px;"><strong>${escapeHtml(scriptsLabel)}</strong><ul style="margin:8px 0 0 18px;">${lines}</ul></div>`;
        }

        modal.innerHTML = `
            <div class="modal-content" style="max-width: 900px; max-height: 90vh;">
                <div class="modal-header">
                    <h2>${escapeHtml(viewTitle)}</h2>
                    <span class="modal-close" data-skill-view-close>&times;</span>
                </div>
                <div class="modal-body" style="overflow-y: auto; max-height: calc(90vh - 120px);">
                    ${sumSkill.version ? `<div style="margin-bottom: 8px;"><strong>${escapeHtml(_t('skills.versionLabel'))}</strong> ${escapeHtml(sumSkill.version)}</div>` : ''}
                    ${sumSkill.description ? `<div style="margin-bottom: 16px;"><strong>${escapeHtml(descLabel)}</strong> ${escapeHtml(sumSkill.description)}</div>` : ''}
                    ${scriptsBlock}
                    <div style="margin-bottom: 8px;"><strong>${escapeHtml(pathLabel)}</strong> ${escapeHtml(sumSkill.path || '')}</div>
                    <div style="margin-bottom: 16px;"><strong>${escapeHtml(modTimeLabel)}</strong> ${escapeHtml(sumSkill.mod_time || '')}</div>
                    <div style="margin-bottom: 8px;"><strong>${escapeHtml(contentLabel)}</strong> <span style="opacity:0.8;font-size:12px;">${escapeHtml(_t('skills.summaryHint'))}</span></div>
                    <pre id="skill-view-body" style="padding: 16px; border-radius: 4px; overflow-x: auto; white-space: pre-wrap; word-wrap: break-word; border: 1px solid var(--border-color);">${escapeHtml(sumSkill.content || '')}</pre>
                </div>
                <div class="modal-footer">
                    <button type="button" class="btn-secondary" data-skill-load-full>${escapeHtml(loadFullLabel)}</button>
                    <button type="button" class="btn-secondary" data-skill-view-close>${escapeHtml(closeBtn)}</button>
                    <button type="button" class="btn-primary" data-skill-view-edit>${escapeHtml(editBtn)}</button>
                </div>
            </div>
        `;
        document.body.appendChild(modal);
        openAppModal(modal);

        const close = () => closeSkillViewModal();
        modal.querySelectorAll('[data-skill-view-close]').forEach(el => el.addEventListener('click', close));
        modal.querySelector('[data-skill-view-edit]').addEventListener('click', () => {
            close();
            editSkill(skillId);
        });
        modal.querySelector('[data-skill-load-full]').addEventListener('click', async () => {
            const pre = modal.querySelector('#skill-view-body');
            const btn = modal.querySelector('[data-skill-load-full]');
            if (!pre || !btn) return;
            btn.disabled = true;
            try {
                const fullRes = await apiFetch(`/api/skills/${encodeURIComponent(skillId)}?depth=full`);
                if (!fullRes.ok) throw new Error(_t('skills.loadDetailFailed'));
                const fullData = await fullRes.json();
                pre.textContent = fullData.skill && fullData.skill.content != null ? fullData.skill.content : '';
            } catch (e) {
                showNotification(_t('skills.loadFullFailed') + ': ' + e.message, 'error');
            } finally {
                btn.disabled = false;
            }
        });
    } catch (error) {
        console.error('查看skill失败:', error);
        showNotification(_t('skills.viewFailed') + ': ' + error.message, 'error');
    }
}

// 关闭查看模态框
function closeSkillViewModal() {
    closeAppModal('skill-view-modal');
    const modal = document.getElementById('skill-view-modal');
    if (modal) {
        modal.remove();
        syncAppModalBodyLock();
    }
}

// 关闭skill模态框
function closeSkillModal() {
    closeAppModal('skill-modal');
    currentEditingSkillName = null;
    skillModalAddMode = true;
    skillFileDirty = false;
    skillDescriptionDirty = false;
    document.getElementById('skill-description').disabled = false;
    skillPackageFiles = [];
    skillActivePath = 'SKILL.md';
}

// 保存skill
async function saveSkill() {
    if (typeof requirePermission === 'function' && !requirePermission('skills:write')) return;
    if (isSavingSkill) return;

    const name = document.getElementById('skill-name').value.trim();
    const description = document.getElementById('skill-description').value.trim();

    if (!name) {
        showNotification(_t('skills.nameRequired'), 'error');
        return;
    }

    if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(name)) {
        showNotification(_t('skills.nameInvalid'), 'error');
        return;
    }

    if (skillModalAddMode || !currentEditingSkillName) {
        if (!description) {
            showNotification(_t('skills.descriptionRequired'), 'error');
            return;
        }
        const content = (document.getElementById('skill-content-add') || {}).value;
        const body = (content || '').trim();
        if (!body) {
            showNotification(_t('skills.contentRequired'), 'error');
            return;
        }
    }

    isSavingSkill = true;
    const saveBtn = document.querySelector('#skill-modal .btn-primary');
    if (saveBtn) {
        saveBtn.disabled = true;
        saveBtn.textContent = _t('skills.saving');
    }

    try {
        if (skillModalAddMode || !currentEditingSkillName) {
            const content = (document.getElementById('skill-content-add') || {}).value;
            const body = (content || '').trim();
            const response = await apiFetch('/api/skills', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, description, content: body })
            });
            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || _t('skills.saveFailed'));
            }
            showNotification(_t('skills.createdSuccess'), 'success');
            closeSkillModal();
            await loadSkills(skillsPagination.currentPage, skillsPagination.pageSize);
            return;
        }

        const path = skillActivePath || 'SKILL.md';
        const ta = document.getElementById('skill-content');
        const raw = ta ? ta.value : '';
        if (path === 'SKILL.md') {
            if (!raw.trim()) {
                showNotification(_t('skills.contentRequired'), 'error');
                return;
            }
            const response = await apiFetch(`/api/skills/${encodeURIComponent(currentEditingSkillName)}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    description: description,
                    content: raw.trim()
                })
            });
            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || _t('skills.saveFailed'));
            }
        } else {
            const response = await apiFetch(`/api/skills/${encodeURIComponent(currentEditingSkillName)}/file`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: path, content: raw })
            });
            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || _t('skills.saveFailed'));
            }
        }

        skillFileDirty = false;
        if (path === 'SKILL.md') skillDescriptionDirty = false;
        showNotification(_t('skills.saveSuccess'), 'success');
        const filesRes = await apiFetch(`/api/skills/${encodeURIComponent(currentEditingSkillName)}/files`);
        if (filesRes.ok) {
            const fd = await filesRes.json();
            skillPackageFiles = fd.files || [];
            renderSkillPackageTree();
        }
        await loadSkills(skillsPagination.currentPage, skillsPagination.pageSize);
    } catch (error) {
        console.error('保存skill失败:', error);
        showNotification(_t('skills.saveFailed') + ': ' + error.message, 'error');
    } finally {
        isSavingSkill = false;
        if (saveBtn) {
            saveBtn.disabled = false;
            saveBtn.textContent = _t('common.save');
        }
    }
}

// 删除skill
async function deleteSkill(skillName) {
    // 先检查是否有角色绑定了该skill
    let boundRoles = [];
    try {
        const checkResponse = await apiFetch(`/api/skills/${encodeURIComponent(skillName)}/bound-roles`);
        if (checkResponse.ok) {
            const checkData = await checkResponse.json();
            boundRoles = checkData.bound_roles || [];
        }
    } catch (error) {
        console.warn('检查skill绑定失败:', error);
        // 如果检查失败，继续执行删除流程
    }

    // 构建确认消息
    let confirmMessage = _t('skills.deleteConfirm', { name: skillName });
    if (boundRoles.length > 0) {
        const rolesList = boundRoles.join('、');
        confirmMessage = _t('skills.deleteConfirmWithRoles', { name: skillName, count: boundRoles.length, roles: rolesList });
    }

    if (!confirm(confirmMessage)) {
        return;
    }

    try {
        const response = await apiFetch(`/api/skills/${encodeURIComponent(skillName)}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || _t('skills.deleteFailed'));
        }

        const data = await response.json();
        let successMessage = _t('skills.deleteSuccess');
        if (data.affected_roles && data.affected_roles.length > 0) {
            const rolesList = data.affected_roles.join('、');
            successMessage = _t('skills.deleteSuccessWithRoles', { count: data.affected_roles.length, roles: rolesList });
        }
        showNotification(successMessage, 'success');
        
        // 如果当前页没有数据了，回到上一页
        const currentPage = skillsPagination.currentPage;
        const totalAfterDelete = skillsPagination.total - 1;
        const totalPages = Math.ceil(totalAfterDelete / skillsPagination.pageSize);
        const pageToLoad = currentPage > totalPages && totalPages > 0 ? totalPages : currentPage;
        await loadSkills(pageToLoad, skillsPagination.pageSize);
    } catch (error) {
        console.error('删除skill失败:', error);
        showNotification(_t('skills.deleteFailed') + ': ' + error.message, 'error');
    }
}

// ==================== Skills状态监控相关函数 ====================

// 加载skills监控数据
async function loadSkillsMonitor() {
    try {
        const response = await apiFetch('/api/skills/stats');
        if (!response.ok) {
            throw new Error(_t('skills.loadStatsFailed'));
        }
        const data = await response.json();
        
        skillsStats = {
            total: data.total_skills || 0,
            totalCalls: data.total_calls || 0,
            totalSuccess: data.total_success || 0,
            totalFailed: data.total_failed || 0,
            skillsDir: data.skills_dir || '',
            stats: data.stats || []
        };

        renderSkillsMonitor();
    } catch (error) {
        console.error('加载skills监控数据失败:', error);
        showNotification(_t('skills.loadStatsFailed') + ': ' + error.message, 'error');
        const statsEl = document.getElementById('skills-stats');
        if (statsEl) {
            statsEl.innerHTML = '<div class="monitor-error">' + _t('skills.loadStatsErrorShort') + ': ' + escapeHtml(error.message) + '</div>';
        }
        const monitorListEl = document.getElementById('skills-monitor-list');
        if (monitorListEl) {
            monitorListEl.innerHTML = '<div class="monitor-error">' + _t('skills.loadCallStatsError') + ': ' + escapeHtml(error.message) + '</div>';
        }
    }
}

// 渲染skills监控页面
function renderSkillsMonitor() {
    // 渲染总体统计
    const statsEl = document.getElementById('skills-stats');
    if (statsEl) {
        const successRate = skillsStats.totalCalls > 0 
            ? ((skillsStats.totalSuccess / skillsStats.totalCalls) * 100).toFixed(1) 
            : '0.0';
        
        statsEl.innerHTML = `
            <div class="monitor-stat-card skills-stat-card skills-stat-card--total">
                <div class="monitor-stat-label">${_t('skills.totalSkillsCount')}</div>
                <div class="monitor-stat-value">${skillsStats.total}</div>
            </div>
            <div class="monitor-stat-card skills-stat-card skills-stat-card--calls">
                <div class="monitor-stat-label">${_t('skills.totalCallsCount')}</div>
                <div class="monitor-stat-value">${skillsStats.totalCalls}</div>
            </div>
            <div class="monitor-stat-card skills-stat-card skills-stat-card--success">
                <div class="monitor-stat-label">${_t('skills.successfulCalls')}</div>
                <div class="monitor-stat-value is-success">${skillsStats.totalSuccess}</div>
            </div>
            <div class="monitor-stat-card skills-stat-card skills-stat-card--failed">
                <div class="monitor-stat-label">${_t('skills.failedCalls')}</div>
                <div class="monitor-stat-value is-danger">${skillsStats.totalFailed}</div>
            </div>
            <div class="monitor-stat-card skills-stat-card skills-stat-card--rate">
                <div class="monitor-stat-label">${_t('skills.successRate')}</div>
                <div class="monitor-stat-value">${successRate}%</div>
            </div>
        `;
    }

    // 渲染调用统计表格
    const monitorListEl = document.getElementById('skills-monitor-list');
    if (!monitorListEl) return;

    const stats = skillsStats.stats || [];
    
    // 如果没有统计数据，显示空状态
    if (stats.length === 0) {
        monitorListEl.innerHTML = '<div class="monitor-empty">' + _t('skills.noCallRecords') + '</div>';
        return;
    }

    // 按调用次数排序（降序），如果调用次数相同，按名称排序
    const sortedStats = [...stats].sort((a, b) => {
        const callsA = b.total_calls || 0;
        const callsB = a.total_calls || 0;
        if (callsA !== callsB) {
            return callsA - callsB;
        }
        return (a.skill_name || '').localeCompare(b.skill_name || '');
    });

    monitorListEl.innerHTML = `
        <table class="monitor-table skills-monitor-table">
            <thead>
                <tr>
                    <th>${_t('skills.skillName')}</th>
                    <th>${_t('skills.totalCalls')}</th>
                    <th>${_t('skills.success')}</th>
                    <th>${_t('skills.failure')}</th>
                    <th>${_t('skills.successRate')}</th>
                    <th>${_t('skills.lastCallTime')}</th>
                </tr>
            </thead>
            <tbody>
                ${sortedStats.map(stat => {
                    const totalCalls = stat.total_calls || 0;
                    const successCalls = stat.success_calls || 0;
                    const failedCalls = stat.failed_calls || 0;
                    const successRate = totalCalls > 0 ? ((successCalls / totalCalls) * 100).toFixed(1) : '0.0';
                    const lastCallTime = stat.last_call_time && stat.last_call_time !== '-' ? stat.last_call_time : '-';
                    const rateValue = Math.max(0, Math.min(100, Number(successRate) || 0));
                    
                    return `
                        <tr>
                            <td><strong>${escapeHtml(stat.skill_name || '')}</strong></td>
                            <td><span class="skills-table-pill">${totalCalls}</span></td>
                            <td><span class="skills-table-number is-success">${successCalls}</span></td>
                            <td><span class="skills-table-number is-danger">${failedCalls}</span></td>
                            <td>
                                <div class="skills-rate-cell">
                                    <span>${successRate}%</span>
                                    <span class="skills-rate-track"><span style="width: ${rateValue}%;"></span></span>
                                </div>
                            </td>
                            <td class="skills-last-call">${escapeHtml(lastCallTime)}</td>
                        </tr>
                    `;
                }).join('')}
            </tbody>
        </table>
    `;
}

// 刷新skills监控
async function refreshSkillsMonitor() {
    await loadSkillsMonitor();
    showNotification(_t('skills.refreshed'), 'success');
}

// 清空skills统计数据
async function clearSkillsStats() {
    if (!confirm(_t('skills.clearStatsConfirm'))) {
        return;
    }

    try {
        const response = await apiFetch('/api/skills/stats', {
            method: 'DELETE'
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || _t('skills.clearStatsFailed'));
        }

        showNotification(_t('skills.statsCleared'), 'success');
        // 重新加载统计数据
        await loadSkillsMonitor();
    } catch (error) {
        console.error('清空统计数据失败:', error);
        showNotification(_t('skills.clearStatsFailed') + ': ' + error.message, 'error');
    }
}

// HTML转义函数
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function escapeAttr(text) {
    return escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

// 语言切换时重新渲染当前页（技能列表与分页使用 _t，需随语言更新）
document.addEventListener('languagechange', function () {
    const page = document.getElementById('page-skills-management');
    if (page && page.classList.contains('active')) {
        renderSkillsList();
        if (!skillsSearchKeyword) {
            renderSkillsPagination();
        }
    }
    const pkg = document.getElementById('skill-package-editor');
    if (pkg && pkg.style.display !== 'none' && currentEditingSkillName) {
        renderSkillPackageTree();
    }
});

document.addEventListener('DOMContentLoaded', function () {
    startSkillsAutoRefresh();
});
