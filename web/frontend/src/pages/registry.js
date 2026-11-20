/** 页面注册表：路由 id → 页面组件（按需加载）。 */
import { lazy } from 'react';

export const PAGES = {
    dashboard: lazy(() => import('./dashboard')),
    projects: lazy(() => import('./projects')),
    'asset-overview': lazy(() => import('./asset-overview')),
    'asset-library': lazy(() => import('./asset-library')),
    vulnerabilities: lazy(() => import('./vulnerabilities')),
    'chat-files': lazy(() => import('./chat-files')),
    'mcp-monitor': lazy(() => import('./mcp-monitor')),
    'knowledge-retrieval-logs': lazy(() => import('./knowledge-retrieval-logs')),
    'knowledge-management': lazy(() => import('./knowledge-management')),
    'skills-monitor': lazy(() => import('./skills-monitor')),
    'platform-rbac': lazy(() => import('./platform-rbac')),
    settings: lazy(() => import('./settings')),
};
