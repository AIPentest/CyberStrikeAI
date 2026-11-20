/**
 * 导航配置：id 即路由段（#/<id>），与后端页面和权限键保持一致。
 */
import {
    IconArticle, IconBolt, IconBook, IconBranch, IconBriefcase, IconCheckList,
    IconComment, IconFlag, IconFolder, IconHome, IconLock, IconPuzzle,
    IconServer, IconSetting, IconShield, IconTerminal, IconUser, IconUserGroup
} from '@douyinfe/semi-icons';

export const NAV_TREE = [
    { id: 'dashboard', i18n: 'nav.dashboard', fallback: '仪表盘', icon: IconHome },
    { id: 'chat', i18n: 'nav.chat', fallback: '对话', icon: IconComment },
    {
        id: 'security', i18n: 'nav.security', fallback: '安全防护', icon: IconShield,
        anyPermission: ['hitl:read', 'config:read'],
        children: [
            { id: 'hitl', i18n: 'nav.hitl', fallback: '人机协同', permission: 'hitl:read' },
            { id: 'tool-guard', i18n: 'nav.toolGuard', fallback: '调用拦截', permission: 'config:read' }
        ]
    },
    { id: 'projects', i18n: 'nav.projects', fallback: '项目管理', icon: IconBriefcase },
    {
        id: 'assets', i18n: 'nav.assets', fallback: '资产管理', icon: IconFlag,
        anyPermission: ['asset:read', 'fofa:execute'],
        children: [
            { id: 'asset-overview', i18n: 'nav.assetOverview', fallback: '资产概览' },
            { id: 'asset-library', i18n: 'nav.assetLibrary', fallback: '资产库' },
            { id: 'info-collect', i18n: 'nav.infoCollect', fallback: '信息收集' }
        ]
    },
    { id: 'vulnerabilities', i18n: 'nav.vulnerabilities', fallback: '漏洞管理', icon: IconShield },
    { id: 'tasks', i18n: 'nav.tasks', fallback: '任务管理', icon: IconCheckList },
    { id: 'workflows', i18n: 'nav.workflows', fallback: '工作流', icon: IconBranch },
    { id: 'webshell', i18n: 'nav.webshell', fallback: 'WebShell管理', icon: IconTerminal },
    {
        id: 'c2', i18n: 'nav.c2', fallback: 'C2', icon: IconBolt,
        children: [
            { id: 'c2-listeners', i18n: 'nav.c2Listeners', fallback: '监听器' },
            { id: 'c2-sessions', i18n: 'nav.c2Sessions', fallback: '会话' },
            { id: 'c2-tasks', i18n: 'nav.c2Tasks', fallback: '任务' },
            { id: 'c2-payloads', i18n: 'nav.c2Payloads', fallback: '载荷' },
            { id: 'c2-events', i18n: 'nav.c2Events', fallback: '事件' },
            { id: 'c2-profiles', i18n: 'nav.c2Profiles', fallback: '流量伪装' }
        ]
    },
    { id: 'chat-files', i18n: 'nav.chatFiles', fallback: '文件管理', icon: IconFolder },
    {
        id: 'mcp', i18n: 'nav.mcp', fallback: 'MCP', icon: IconServer,
        children: [
            { id: 'mcp-monitor', i18n: 'nav.mcpMonitor', fallback: 'MCP状态监控' },
            { id: 'mcp-management', i18n: 'nav.mcpManagement', fallback: 'MCP管理' }
        ]
    },
    {
        id: 'knowledge', i18n: 'nav.knowledge', fallback: '知识', icon: IconBook,
        children: [
            { id: 'knowledge-retrieval-logs', i18n: 'nav.knowledgeRetrievalLogs', fallback: '检索历史' },
            { id: 'knowledge-management', i18n: 'nav.knowledgeManagement', fallback: '知识管理' }
        ]
    },
    {
        id: 'skills', i18n: 'nav.skills', fallback: '技能', icon: IconPuzzle,
        children: [
            { id: 'skills-monitor', i18n: 'nav.skillsMonitor', fallback: '技能状态' },
            { id: 'skills-management', i18n: 'nav.skillsManagement', fallback: '技能管理' }
        ]
    },
    {
        id: 'agents', i18n: 'nav.agents', fallback: '智能体', icon: IconUserGroup,
        children: [
            { id: 'agents-management', i18n: 'nav.agentsManagement', fallback: '智能体管理' }
        ]
    },
    {
        id: 'roles', i18n: 'nav.roles', fallback: '角色', icon: IconUser,
        children: [
            { id: 'roles-management', i18n: 'nav.rolesManagement', fallback: '角色管理' }
        ]
    },
    { id: 'platform-rbac', i18n: 'nav.platformRbac', fallback: '平台权限', icon: IconLock },
    { id: 'settings', i18n: 'nav.settings', fallback: '系统设置', icon: IconSetting },
    { id: 'api-docs', i18n: 'apiDocs.title', fallback: 'API 文档', icon: IconArticle }
];

export const NAV_SECTIONS = [
    { id: 'workspace', i18n: 'nav.groupWorkspace', fallback: '工作台', pages: ['dashboard', 'chat', 'security'] },
    { id: 'operations', i18n: 'nav.groupOperations', fallback: '安全作业', pages: ['projects', 'assets', 'vulnerabilities', 'tasks', 'workflows', 'webshell', 'c2', 'chat-files'] },
    { id: 'capabilities', i18n: 'nav.groupCapabilities', fallback: '能力中心', pages: ['mcp', 'knowledge', 'skills', 'agents', 'roles'] },
    { id: 'platform', i18n: 'nav.groupPlatform', fallback: '平台管理', pages: ['platform-rbac', 'settings', 'api-docs'] }
];

/** 扁平页签列表，用于标题解析和导航高亮。 */
export const NAV_PAGES = NAV_TREE.flatMap(item => (item.children ? item.children.map(child => ({ ...child, parent: item.id })) : [item]));

export function findPage(id) {
    return NAV_PAGES.find(item => item.id === id) || NAV_TREE.find(item => item.id === id) || null;
}

export const DEFAULT_PAGE = 'dashboard';
