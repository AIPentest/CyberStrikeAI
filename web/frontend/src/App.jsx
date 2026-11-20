import { Suspense, useEffect, useMemo } from 'react';
import { Navigate, Route, Routes, useLocation, useParams } from 'react-router-dom';
import { Spin, Typography } from '@douyinfe/semi-ui';
import AppLayout from './layout/AppLayout';
import LoginPage from './layout/LoginPage';
import { useAuth } from './lib/auth';
import { useT } from './lib/i18n';
import { findPage } from './lib/nav';
import { PAGES } from './pages/registry';

const { Title, Text } = Typography;
const LEGACY_PAGES = new Set(['chat', 'tasks', 'system-update', 'info-collect', 'hitl', 'c2-sessions']);

function Loading() {
    return (
        <div className="cs-page-loading">
            <Spin size="large" />
        </div>
    );
}

function StatusPage({ code, title, description }) {
    return (
        <div className="cs-page-loading" style={{ flexDirection: 'column', gap: 8 }}>
            <Title heading={2} style={{ margin: 0 }}>{code}</Title>
            <Text strong>{title}</Text>
            {description && <Text type="tertiary">{description}</Text>}
        </div>
    );
}

function LegacyPage({ pageId }) {
    const { search } = useLocation();
    const href = `/#${pageId}${search}`;
    useEffect(() => { window.location.replace(href); }, [href]);
    return <Loading />;
}

function PageHost() {
    const { pageId } = useParams();
    const { search } = useLocation();
    const { hasPermission, hasAnyPermission } = useAuth();
    const t = useT();
    const page = findPage(pageId);
    const Component = pageId ? PAGES[pageId] : null;

    const allowed = useMemo(() => {
        if (!page) return true;
        if (page.anyPermission) return hasAnyPermission(page.anyPermission);
        if (page.permission) return hasPermission(page.permission);
        const parent = page.parent ? findPage(page.parent) : null;
        if (parent?.anyPermission) return hasAnyPermission(parent.anyPermission);
        return true;
    }, [page, hasPermission, hasAnyPermission]);

    const section = new URLSearchParams(search).get('section');
    if (LEGACY_PAGES.has(pageId) || (pageId === 'settings' && section && section !== 'models')) {
        return <LegacyPage pageId={pageId} />;
    }
    if (!page) {
        return <StatusPage code="404" title={t('common.pageNotFound', null, '页面不存在')} />;
    }
    if (!Component) {
        return <StatusPage code="404" title={page.fallback} description={t('common.pagePending', null, '页面尚未注册')} />;
    }
    if (!allowed) {
        return <StatusPage code="403" title={t('common.forbidden', null, '当前账号没有访问该页面的权限')} />;
    }
    return (
        <Suspense fallback={<Loading />}>
            <Component />
        </Suspense>
    );
}

export default function App() {
    const { authenticated, checking } = useAuth();
    if (checking) return <Loading />;
    if (!authenticated) return <LoginPage />;
    return (
        <Routes>
            <Route element={<AppLayout />}>
                <Route path="/" element={<Navigate to="/dashboard" replace />} />
                <Route path="/:pageId" element={<PageHost />} />
                <Route path="*" element={<Navigate to="/dashboard" replace />} />
            </Route>
        </Routes>
    );
}
