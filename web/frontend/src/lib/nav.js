/**
 * 导航配置：id 即路由段（#/<id>），与后端页面和权限键保持一致。
 */
import {
    IconBook, IconBriefcase, IconFlag, IconFolder, IconHome, IconLock, IconPuzzle,
    IconServer, IconSetting, IconShield
} from '@douyinfe/semi-icons';

export const NAV_TREE = [
    { id: 'dashboard', i18n: 'nav.dashboard', fallback: '仪表盘', icon: IconHome },
    { id: 'projects', i18n: 'nav.projects', fallback: '项目管理', icon: IconBriefcase },
    {
        id: 'assets', i18n: 'nav.assets', fallback: '资产管理', icon: IconFlag,
        anyPermission: ['asset:read', 'fofa:execute'],
        children: [
            { id: 'asset-overview', i18n: 'nav.assetOverview', fallback: '资产概览' },
            { id: 'asset-library', i18n: 'nav.assetLibrary', fallback: '资产库' },
        ]
    },
    { id: 'vulnerabilities', i18n: 'nav.vulnerabilities', fallback: '漏洞管理', icon: IconShield },
    { id: 'chat-files', i18n: 'nav.chatFiles', fallback: '文件管理', icon: IconFolder },
    {
        id: 'mcp', i18n: 'nav.mcp', fallback: 'MCP', icon: IconServer,
        children: [
            { id: 'mcp-monitor', i18n: 'nav.mcpMonitor', fallback: 'MCP状态监控' },
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
        ]
    },
    { id: 'platform-rbac', i18n: 'nav.platformRbac', fallback: '平台权限', icon: IconLock },
    { id: 'settings', i18n: 'nav.settings', fallback: '系统设置', icon: IconSetting, permission: 'config:read' },
];

export const NAV_SECTIONS = [
    { id: 'workspace', i18n: 'nav.groupWorkspace', fallback: '工作台', pages: ['dashboard'] },
    { id: 'operations', i18n: 'nav.groupOperations', fallback: '安全作业', pages: ['projects', 'assets', 'vulnerabilities', 'chat-files'] },
    { id: 'capabilities', i18n: 'nav.groupCapabilities', fallback: '能力中心', pages: ['mcp', 'knowledge', 'skills'] },
    { id: 'platform', i18n: 'nav.groupPlatform', fallback: '平台管理', pages: ['platform-rbac', 'settings'] }
];

/** 扁平页签列表，用于标题解析和导航高亮。 */
export const NAV_PAGES = NAV_TREE.flatMap(item => (item.children ? item.children.map(child => ({ ...child, parent: item.id })) : [item]));

export function findPage(id) {
    return NAV_PAGES.find(item => item.id === id) || NAV_TREE.find(item => item.id === id) || null;
}

export const DEFAULT_PAGE = 'dashboard';
