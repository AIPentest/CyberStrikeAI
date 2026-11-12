// 页面路由管理
let currentPage = null;

/** 对话、漏洞管理和设置页切换时保留查询串（会话 ID 或设置分区）。 */
function buildHashForPage(pageId) {
    if (pageId !== 'chat' && pageId !== 'vulnerabilities' && pageId !== 'settings') {
        return pageId;
    }
    const full = window.location.hash.slice(1);
    const parts = full.split('?');
    const curPage = parts[0];
    const q = parts.length > 1 ? parts.slice(1).join('?') : '';
    if (curPage === pageId && q) {
        return pageId + '?' + q;
    }
    return pageId;
}

let chatConversationFromHashSeq = 0;

function cancelScheduledChatConversationFromHash() {
    chatConversationFromHashSeq++;
    setChatConversationRestorePending('', false);
}
window.cancelScheduledChatConversationFromHash = cancelScheduledChatConversationFromHash;

function setChatConversationRestorePending(conversationId, pending) {
    const container = document.querySelector('.chat-container');
    if (!container) return;
    const id = String(conversationId || '').trim();
    if (pending && id) {
        container.classList.add('is-conversation-restoring');
        container.dataset.restoringConversationId = id;
        container.setAttribute('aria-busy', 'true');
        return;
    }
    container.classList.remove('is-conversation-restoring');
    delete container.dataset.restoringConversationId;
    container.removeAttribute('aria-busy');
}

function finishChatConversationRestore(conversationId) {
    setChatConversationRestorePending(conversationId, false);
}
window.finishChatConversationRestore = finishChatConversationRestore;

function scheduleChatConversationFromHash(delayMs) {
    const hash = window.location.hash.slice(1);
    const hashParts = hash.split('?');
    if (hashParts[0] !== 'chat' || hashParts.length < 2) {
        return;
    }
    const params = new URLSearchParams(hashParts.slice(1).join('?'));
    const conversationId = params.get('conversation');
    const projectId = params.get('project');
    if (projectId && typeof setActiveProjectId === 'function') {
        setActiveProjectId(projectId);
        if (typeof refreshChatProjectSelector === 'function') {
            refreshChatProjectSelector();
        }
    }
    if (!conversationId) {
        return;
    }
    // 同一事件循环内先遮住默认新对话状态，避免网络请求返回前闪出“无项目”。
    setChatConversationRestorePending(conversationId, true);
    const token = ++chatConversationFromHashSeq;
    setTimeout(() => {
        if (token !== chatConversationFromHashSeq) {
            return;
        }
        if (typeof loadConversation === 'function') {
            loadConversation(conversationId);
        } else if (typeof window.loadConversation === 'function') {
            window.loadConversation(conversationId);
        } else {
            console.warn('loadConversation function not found');
        }
    }, delayMs);
}

/** 跳转到指定对话：单次切页 + 单次加载，避免 hashchange 与手动 load 重复触发导致闪烁 */
function navigateToConversation(conversationId) {
    const cid = String(conversationId || '').trim();
    if (!cid) return;
    const targetHash = 'chat?conversation=' + encodeURIComponent(cid);
    const alreadyOnChat = currentPage === 'chat';

    if (window.location.hash.slice(1) !== targetHash) {
        history.replaceState(null, '', '#' + targetHash);
    }

    if (!alreadyOnChat) {
        switchPage('chat');
    }

    if (typeof loadConversation === 'function') {
        return loadConversation(cid);
    } else if (typeof window.loadConversation === 'function') {
        return window.loadConversation(cid);
    }
}
window.navigateToConversation = navigateToConversation;

// 初始化路由
function initRouter() {
    // 从URL hash读取页面（如果有）
    const hash = window.location.hash.slice(1);
    if (hash) {
        const hashParts = hash.split('?');
        let pageId = hashParts[0].replace(/^#?\//, '');
        if (pageId === 'c2') pageId = 'c2-listeners';
        // 一键更新已并进系统设置页的分区菜单：#system-update 这条老深链落到设置页的那一区。
        const settingsSection = pageId === 'system-update' ? 'update' : pageId === 'settings' && new URLSearchParams(hashParts.slice(1).join('?')).get('section') === 'token-usage' ? 'token-usage' : '';
        if (settingsSection) pageId = 'settings';
        if (pageId && ['dashboard', 'chat', 'hitl', 'tool-guard', 'asset-overview', 'asset-library', 'info-collect', 'projects', 'vulnerabilities', 'webshell', 'chat-files', 'mcp-monitor', 'mcp-management', 'knowledge-management', 'knowledge-retrieval-logs', 'roles-management', 'platform-rbac', 'workflows', 'skills-monitor', 'skills-management', 'agents-management', 'settings', 'tasks', 'c2-listeners', 'c2-sessions', 'c2-tasks', 'c2-payloads', 'c2-events', 'c2-profiles'].includes(pageId)) {
            switchPage(pageId);
            if (settingsSection && typeof switchSettingsSection === 'function') {
                switchSettingsSection(settingsSection);
            }
            if (pageId === 'chat') {
                scheduleChatConversationFromHash(0);
            }
            return;
        }
    }
    
    // 默认显示仪表盘
    switchPage('dashboard');
}

// 切换页面
function switchPage(pageId) {
    const targetPage = document.getElementById(`page-${pageId}`);
    if (!targetPage) return;
    if (pageId !== 'chat') {
        setChatConversationRestorePending('', false);
        if (currentPage === 'chat' && typeof window.abandonChatConversationForPageNavigation === 'function') {
            window.abandonChatConversationForPageNavigation();
        }
    }

    // 导航点击会修改 hash，随后浏览器还会触发 hashchange。
    // 同一页面已经激活时不再重复初始化，避免接口重复请求和页面二次重绘。
    if (currentPage === pageId && targetPage.classList.contains('active')) {
        const currentHash = buildHashForPage(pageId);
        if (window.location.hash.slice(1) !== currentHash) {
            window.location.hash = currentHash;
        }
        return;
    }

    if (typeof window.syncC2NavOnceFromServer === 'function') {
        void window.syncC2NavOnceFromServer();
    }
    // 隐藏所有页面
    document.querySelectorAll('.page').forEach(page => {
        page.classList.remove('active');
    });
    
    // 显示目标页面
    targetPage.classList.add('active');
    currentPage = pageId;
        
    const newHash = buildHashForPage(pageId);
    if (window.location.hash.slice(1) !== newHash) {
        window.location.hash = newHash;
    }
        
    // 更新导航状态
    updateNavState(pageId);
        
    // 页面特定的初始化
    initPage(pageId);

    if (typeof applyRBACToUI === 'function') {
        applyRBACToUI(targetPage);
    }
}
window.switchPage = switchPage;

// 更新导航状态
function updateNavState(pageId) {
    // 移除所有活动状态
    document.querySelectorAll('.nav-item').forEach(item => {
        item.classList.remove('active');
        item.classList.remove('expanded');
    });
    
    document.querySelectorAll('.nav-submenu-item').forEach(item => {
        item.classList.remove('active');
    });
    
    // 设置活动状态
    if (pageId === 'hitl' || pageId === 'tool-guard') {
        const securityItem = document.querySelector('.nav-item[data-page="security"]');
        if (securityItem) {
            securityItem.classList.add('active');
            securityItem.classList.add('expanded');
        }
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) submenuItem.classList.add('active');
    } else if (pageId === 'asset-overview' || pageId === 'asset-library' || pageId === 'info-collect') {
        const assetItem = document.querySelector('.nav-item[data-page="assets"]');
        if (assetItem) {
            assetItem.classList.add('active');
            assetItem.classList.add('expanded');
        }
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) submenuItem.classList.add('active');
    } else if (pageId === 'mcp-monitor' || pageId === 'mcp-management') {
        // MCP子菜单项
        const mcpItem = document.querySelector('.nav-item[data-page="mcp"]');
        if (mcpItem) {
            mcpItem.classList.add('active');
            // 展开MCP子菜单
            mcpItem.classList.add('expanded');
        }
        
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId === 'knowledge-management' || pageId === 'knowledge-retrieval-logs') {
        // 知识子菜单项
        const knowledgeItem = document.querySelector('.nav-item[data-page="knowledge"]');
        if (knowledgeItem) {
            knowledgeItem.classList.add('active');
            // 展开知识子菜单
            knowledgeItem.classList.add('expanded');
        }
        
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId === 'skills-monitor' || pageId === 'skills-management') {
        // Skills子菜单项
        const skillsItem = document.querySelector('.nav-item[data-page="skills"]');
        if (skillsItem) {
            skillsItem.classList.add('active');
            // 展开Skills子菜单
            skillsItem.classList.add('expanded');
        }
        
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId === 'agents-management') {
        const agentsItem = document.querySelector('.nav-item[data-page="agents"]');
        if (agentsItem) {
            agentsItem.classList.add('active');
            agentsItem.classList.add('expanded');
        }
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId.startsWith('c2') || pageId === 'c2-listeners' || pageId === 'c2-sessions' || pageId === 'c2-tasks' || pageId === 'c2-payloads' || pageId === 'c2-events' || pageId === 'c2-profiles') {
        // C2 子菜单项
        const c2Item = document.querySelector('.nav-item[data-page="c2"]');
        if (c2Item) {
            c2Item.classList.add('active');
            c2Item.classList.add('expanded');
        }
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId === 'roles-management') {
        // 角色子菜单项
        const rolesItem = document.querySelector('.nav-item[data-page="roles"]');
        if (rolesItem) {
            rolesItem.classList.add('active');
            // 展开角色子菜单
            rolesItem.classList.add('expanded');
        }
        
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else {
        // 主菜单项
        const navItem = document.querySelector(`.nav-item[data-page="${pageId}"]`);
        if (navItem) {
            navItem.classList.add('active');
        }
    }
}

/** 读取侧栏子菜单项（仅 .nav-submenu 内，避免误匹配） */
function getNavSubmenuItems(navItem) {
    if (!navItem) return [];
    const submenu = navItem.querySelector('.nav-submenu');
    if (!submenu) return [];
    return Array.from(submenu.querySelectorAll('.nav-submenu-item'));
}

// 切换子菜单
function toggleSubmenu(menuId) {
    const sidebar = document.getElementById('main-sidebar');
    const navItem = document.querySelector(`.nav-item[data-page="${menuId}"]`);
    
    if (!navItem) return;
    
    const collapsed = sidebar && sidebar.classList.contains('collapsed');

    // 检查侧边栏是否折叠
    if (collapsed) {
        // 折叠状态下显示弹出菜单
        showSubmenuPopup(navItem, menuId);
        return;
    }

    // 展开状态下切换子菜单，并滚入视口以便看到子项
    const willExpand = !navItem.classList.contains('expanded');
    navItem.classList.toggle('expanded');
    if (willExpand) {
        requestAnimationFrame(() => {
            navItem.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
            const items = getNavSubmenuItems(navItem);
            const last = items[items.length - 1];
            if (last) {
                last.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
            }
        });
    }
}
window.toggleSubmenu = toggleSubmenu;

// 显示子菜单弹出框
function showSubmenuPopup(navItem, menuId) {
    const existingPopup = document.querySelector('.submenu-popup');
    if (existingPopup) {
        const sameMenu = existingPopup.dataset.menuId === menuId;
        existingPopup.remove();
        // 再次点击同一项：仅关闭；点击另一项：继续打开新菜单
        if (sameMenu) {
            return;
        }
    }

    const navItemContent = navItem.querySelector('.nav-item-content');
    const submenu = navItem.querySelector('.nav-submenu');
    
    if (!submenu) return;
    
    // 获取菜单位置
    const rect = navItemContent.getBoundingClientRect();
    
    // 创建弹出菜单
    const popup = document.createElement('div');
    popup.className = 'submenu-popup';
    popup.dataset.menuId = menuId;
    popup.style.position = 'fixed';
    popup.style.left = (rect.right + 8) + 'px';
    popup.style.top = rect.top + 'px';
    popup.style.zIndex = '1000';
    
    // 复制子菜单项到弹出菜单
    const submenuItems = submenu.querySelectorAll('.nav-submenu-item');
    submenuItems.forEach(item => {
        if (item.hidden || (typeof permissionAllowedForElement === 'function' && !permissionAllowedForElement(item))) return;
        const popupItem = document.createElement('div');
        popupItem.className = 'submenu-popup-item';
        popupItem.textContent = item.textContent.trim();
        
        // 检查是否是当前激活的页面
        const pageId = item.getAttribute('data-page');
        if (pageId && document.querySelector(`.nav-submenu-item[data-page="${pageId}"].active`)) {
            popupItem.classList.add('active');
        }
        
        popupItem.onclick = function(e) {
            e.stopPropagation();
            e.preventDefault();
            
            // 获取页面ID并切换
            const pageId = item.getAttribute('data-page');
            if (pageId) {
                switchPage(pageId);
            }
            
            // 关闭弹出菜单
            popup.remove();
            document.removeEventListener('click', closePopup);
        };
        popup.appendChild(popupItem);
    });
    
    document.body.appendChild(popup);
    
    // 点击外部关闭弹出菜单
    const closePopup = function(e) {
        if (!popup.contains(e.target) && !navItem.contains(e.target)) {
            popup.remove();
            document.removeEventListener('click', closePopup);
        }
    };
    
    // 延迟添加事件监听，避免立即触发
    setTimeout(() => {
        document.addEventListener('click', closePopup);
    }, 0);
}

// 初始化页面
async function initPage(pageId) {
    // 等待 i18n 就绪，避免快速刷新时翻译函数未初始化导致页面显示原始占位符 key
    if (window.i18nReady) await window.i18nReady;
    if (typeof stopExternalMcpPoll === 'function') {
        stopExternalMcpPoll();
    }
    switch(pageId) {
        case 'dashboard':
            if (typeof refreshDashboard === 'function') {
                refreshDashboard();
            }
            break;
        case 'chat':
            // 恢复对话列表折叠状态（从其他页返回时保持用户选择）
            initConversationSidebarState();
            if (typeof prefetchProjectsForChat === 'function') {
                prefetchProjectsForChat();
            }
            if (typeof refreshChatProjectSelector === 'function') {
                refreshChatProjectSelector();
            }
            break;
        case 'tool-guard':
            if (typeof loadToolGuardConfig === 'function') loadToolGuardConfig();
            break;
        case 'hitl':
            if (typeof refreshHitlActivePanel === 'function') {
                refreshHitlActivePanel();
            } else if (typeof refreshHitlPending === 'function') {
                refreshHitlPending();
            }
            break;
        case 'info-collect':
            // 信息收集页面
            if (typeof initInfoCollectPage === 'function') {
                initInfoCollectPage();
            }
            break;
        case 'asset-overview':
            if (typeof loadAssetOverview === 'function') loadAssetOverview();
            break;
        case 'asset-library':
            if (typeof loadAssets === 'function') loadAssets();
            break;
        case 'tasks':
            // 初始化任务管理页面
            if (typeof initTasksPage === 'function') {
                initTasksPage();
            }
            break;
        case 'mcp-monitor':
            // 初始化监控面板
            if (typeof refreshMonitorPanel === 'function') {
                refreshMonitorPanel();
            }
            if (typeof startMonitorPoll === 'function') {
                startMonitorPoll();
            }
            break;
        case 'mcp-management':
            // 初始化MCP管理
            const startLoadMcpTools = () => {
                // 加载工具列表（MCP工具配置已移到MCP管理页面）
                // 使用异步加载，避免阻塞页面渲染
                if (typeof loadToolsList === 'function') {
                    // 确保工具分页设置已初始化
                    if (typeof getToolsPageSize === 'function' && typeof toolsPagination !== 'undefined') {
                        toolsPagination.pageSize = getToolsPageSize();
                    }
                    // 延迟加载，让页面先渲染
                    setTimeout(() => {
                        loadToolsList(1, '').catch(err => {
                            console.error('加载工具列表失败:', err);
                        });
                    }, 100);
                }
            };
            const afterMcpConfigReady = () => {
                startLoadMcpTools();
                if (typeof loadExternalMCPs === 'function') {
                    loadExternalMCPs().catch(err => {
                        console.warn('加载外部MCP列表失败:', err);
                    });
                }
                if (typeof startExternalMcpPoll === 'function') {
                    startExternalMcpPoll();
                }
            };
            // 先拉取配置（含 tool_search 常驻列表），再加载工具与外部 MCP。
            // 仅有 mcp:read 的用户无需拉全量 /api/config（需 config:read）。
            const canLoadFullConfig = typeof hasPermission !== 'function' || hasPermission('config:read');
            if (typeof loadConfig === 'function' && canLoadFullConfig) {
                loadConfig(false, { silent: true })
                    .catch(err => {
                        console.warn('加载配置失败（将继续加载 MCP 列表）:', err);
                    })
                    .finally(afterMcpConfigReady);
            } else {
                afterMcpConfigReady();
            }
            break;
        case 'projects':
            if (typeof initProjectsPage === 'function') {
                initProjectsPage();
            }
            break;
        case 'vulnerabilities':
            // 初始化漏洞管理页面
            if (typeof initVulnerabilityPage === 'function') {
                initVulnerabilityPage();
            }
            break;
        case 'webshell':
            // 初始化 WebShell 管理页面
            if (typeof initWebshellPage === 'function') {
                initWebshellPage();
            }
            break;
        case 'chat-files':
            if (typeof initChatFilesPage === 'function') {
                initChatFilesPage();
            }
            break;
        case 'settings':
            // 初始化设置页面（不需要加载工具列表）
            if (typeof loadConfig === 'function') {
                loadConfig(false);
            }
            // 回到设置页时若停在「一键更新」分区，重新读一遍安装状态并把进度轮询接上——
            // 离开设置页时轮询已被停掉，不接的话画面上会留一份不再动的进度。
            const updateSection = document.getElementById('settings-section-update');
            if (updateSection && updateSection.classList.contains('active') && typeof loadUpdateConsole === 'function') {
                loadUpdateConsole();
            }
            // Returning through the regular settings nav reloads a retained usage panel.
            // Deep links are loaded by switchSettingsSection after switchPage returns.
            const tokenSection = document.getElementById('settings-section-token-usage');
            if (tokenSection && tokenSection.classList.contains('active') && window.location.hash !== '#settings?section=token-usage' && typeof refreshTokenUsage === 'function') {
                refreshTokenUsage();
            }
            break;
        case 'roles-management':
            // 初始化角色管理页面
            // 重置搜索UI（变量会在下次搜索时自动更新）
            const rolesSearchInput = document.getElementById('roles-search');
            if (rolesSearchInput) {
                rolesSearchInput.value = '';
            }
            const rolesSearchClear = document.getElementById('roles-search-clear');
            if (rolesSearchClear) {
                rolesSearchClear.style.display = 'none';
            }
            if (typeof loadRoles === 'function') {
                loadRoles().then(() => {
                    if (typeof renderRolesList === 'function') {
                        renderRolesList();
                    }
                });
            }
            break;
        case 'platform-rbac':
            if (typeof initPlatformRbacPage === 'function') {
                initPlatformRbacPage();
            }
            break;
        case 'workflows':
            if (typeof refreshWorkflows === 'function') {
                refreshWorkflows();
            }
            break;
        case 'skills-monitor':
            // 初始化Skills状态监控页面
            if (typeof loadSkillsMonitor === 'function') {
                loadSkillsMonitor();
            }
            break;
        case 'skills-management':
            // 初始化Skills管理页面
            // 重置搜索UI（变量会在下次搜索时自动更新）
            const skillsSearchInput = document.getElementById('skills-search');
            if (skillsSearchInput) {
                skillsSearchInput.value = '';
            }
            const skillsSearchClear = document.getElementById('skills-search-clear');
            if (skillsSearchClear) {
                skillsSearchClear.style.display = 'none';
            }
            if (typeof initSkillsPagination === 'function') {
                initSkillsPagination();
            }
            if (typeof loadSkills === 'function') {
                loadSkills();
            }
            break;
        case 'agents-management':
            if (typeof loadMarkdownAgents === 'function') {
                loadMarkdownAgents();
            }
            break;
        case 'c2-listeners':
        case 'c2-sessions':
        case 'c2-tasks':
        case 'c2-payloads':
        case 'c2-events':
        case 'c2-profiles':
            window.currentPageId = pageId;
            if (window.C2 && typeof window.C2.init === 'function') {
                window.C2.init();
            }
            break;
    }
    
    // 清理其他页面的定时器
    if (pageId !== 'tasks' && typeof cleanupTasksPage === 'function') {
        cleanupTasksPage();
    }
    // 一键更新的进度轮询同理：它现在住在系统设置页里，离开设置页就没必要每 1.5 秒读一次任务了
    // （同一页内切到别的分区由 settings.js 的 switchSettingsSection 负责停）。
    if (pageId !== 'settings' && typeof stopUpdatePolling === 'function') {
        stopUpdatePolling();
    }
    if (pageId !== 'settings' && typeof stopTokenUsageRequests === 'function') {
        stopTokenUsageRequests();
    }
}

// 页面加载完成后初始化路由
document.addEventListener('DOMContentLoaded', function() {
    initRouter();
    document.documentElement.classList.remove('initial-route-pending');
    initSidebarState();
    
    // 监听hash变化
    window.addEventListener('hashchange', function() {
        const hash = window.location.hash.slice(1);
        // 处理带参数的hash（如 chat?conversation=xxx）
        const hashParts = hash.split('?');
        let pageId = hashParts[0].replace(/^#?\//, '');
        
        if (pageId === 'c2') pageId = 'c2-listeners';
        // 与首屏解析同一套别名：#system-update 落到系统设置页的「一键更新」分区。
        const settingsSection = pageId === 'system-update' ? 'update' : pageId === 'settings' && new URLSearchParams(hashParts.slice(1).join('?')).get('section') === 'token-usage' ? 'token-usage' : '';
        if (settingsSection) pageId = 'settings';
        if (pageId && ['dashboard', 'chat', 'hitl', 'tool-guard', 'asset-overview', 'asset-library', 'info-collect', 'projects', 'tasks', 'workflows', 'vulnerabilities', 'webshell', 'chat-files', 'mcp-monitor', 'mcp-management', 'knowledge-management', 'knowledge-retrieval-logs', 'roles-management', 'platform-rbac', 'skills-monitor', 'skills-management', 'agents-management', 'settings', 'c2-listeners', 'c2-sessions', 'c2-tasks', 'c2-payloads', 'c2-events', 'c2-profiles'].includes(pageId)) {
            switchPage(pageId);
            if (settingsSection && typeof switchSettingsSection === 'function') {
                switchSettingsSection(settingsSection);
            }
            if (pageId === 'chat') {
                scheduleChatConversationFromHash(0);
            }
        }
    });
});

// 切换侧边栏折叠/展开
function toggleSidebar() {
    const sidebar = document.getElementById('main-sidebar');
    if (sidebar) {
        sidebar.classList.toggle('collapsed');
        // 保存折叠状态到localStorage
        const isCollapsed = sidebar.classList.contains('collapsed');
        localStorage.setItem('sidebarCollapsed', isCollapsed ? 'true' : 'false');
    }
}
window.toggleSidebar = toggleSidebar;

// 初始化侧边栏状态
function initSidebarState() {
    const sidebar = document.getElementById('main-sidebar');
    if (sidebar) {
        const savedState = localStorage.getItem('sidebarCollapsed');
        if (savedState === 'true') {
            sidebar.classList.add('collapsed');
        }
    }
    initConversationSidebarState();
}

// 切换对话页左侧列表折叠/展开
function toggleConversationSidebar() {
    const sidebar = document.getElementById('conversation-sidebar');
    if (sidebar) {
        sidebar.classList.toggle('collapsed');
        const isCollapsed = sidebar.classList.contains('collapsed');
        localStorage.setItem('conversationSidebarCollapsed', isCollapsed ? 'true' : 'false');
    }
}
window.toggleConversationSidebar = toggleConversationSidebar;

// 恢复对话列表折叠状态（进入对话页时生效）
function initConversationSidebarState() {
    const sidebar = document.getElementById('conversation-sidebar');
    if (sidebar) {
        const savedState = localStorage.getItem('conversationSidebarCollapsed');
        if (savedState === 'true') {
            sidebar.classList.add('collapsed');
        } else {
            sidebar.classList.remove('collapsed');
        }
    }
}

// 导出函数供其他脚本使用（与上方尽早绑定保持一致，便于外部脚本探测）
window.currentPage = function() { return currentPage; };
