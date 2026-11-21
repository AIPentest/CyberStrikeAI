import { useEffect, useId, useMemo, useState } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import {
    Avatar, Badge, Breadcrumb, Button, Dropdown, Form, Nav, Popover, SideSheet, Space, Typography
} from '@douyinfe/semi-ui';
import {
    IconBell, IconChevronLeft, IconChevronRight, IconExit, IconKey, IconLanguage, IconMenu, IconMoon, IconSun
} from '@douyinfe/semi-icons';
import { useAuth } from '../lib/auth';
import { useI18n, LANGUAGES, useT } from '../lib/i18n';
import { useTheme } from '../lib/theme';
import { NAV_SECTIONS, NAV_TREE, findPage } from '../lib/nav';
import { api } from '../lib/http';
import { Drawer, toastError, toastSuccess } from '../components/ui';
import { formatDateTime } from '../lib/format';
import { useInterval } from '../components/hooks';

const { Text } = Typography;
const sidebarSubmenuProps = {
    position: 'right',
    contentClassName: 'cs-nav-flyout'
};

function useIsMobile() {
    const [mobile, setMobile] = useState(() => window.matchMedia('(max-width: 900px)').matches);
    useEffect(() => {
        const media = window.matchMedia('(max-width: 900px)');
        const onChange = event => setMobile(event.matches);
        media.addEventListener('change', onChange);
        return () => media.removeEventListener('change', onChange);
    }, []);
    return mobile;
}

function buildNavItems(tree, t, hasPermission, hasAnyPermission) {
    return tree
        .filter(item => (!item.anyPermission || hasAnyPermission(item.anyPermission)) && (!item.permission || hasPermission(item.permission)))
        .map(item => {
            const children = item.children
                ? item.children.filter(child => !child.permission || hasPermission(child.permission))
                : null;
            if (children && children.length === 0) return null;
            return {
                itemKey: children ? item.id : item.id,
                text: t(item.i18n, null, item.fallback),
                icon: item.icon ? <item.icon aria-hidden="true" /> : undefined,
                items: children
                    ? children.map(child => ({ itemKey: child.id, text: t(child.i18n, null, child.fallback) }))
                    : undefined
            };
        })
        .filter(Boolean);
}

function ThemeMenu() {
    const { preference, setPreference } = useTheme();
    const t = useT();
    const options = [
        { key: 'system', label: t('theme.system', null, '跟随系统') },
        { key: 'light', label: t('theme.light', null, '浅色') },
        { key: 'dark', label: t('theme.dark', null, '暗色') }
    ];
    const current = options.find(option => option.key === preference) || options[0];
    return (
        <Dropdown
            trigger="click"
            position="bottomRight"
            menu={options.map(option => ({
                node: 'item',
                name: option.label,
                active: option.key === preference,
                onClick: () => setPreference(option.key)
            }))}
        >
            <Button theme="borderless" type="tertiary" icon={preference === 'dark' ? <IconMoon /> : <IconSun />} aria-label={current.label} />
        </Dropdown>
    );
}

function LanguageMenu() {
    const { lang, setLanguage } = useI18n();
    return (
        <Dropdown
            trigger="click"
            position="bottomRight"
            menu={LANGUAGES.map(item => ({
                node: 'item',
                name: item.label,
                active: item.id === lang,
                onClick: () => setLanguage(item.id)
            }))}
        >
            <Button theme="borderless" type="tertiary" icon={<IconLanguage />} aria-label={lang} />
        </Dropdown>
    );
}

function NotificationsBell({ navigate }) {
    const t = useT();
    const [items, setItems] = useState([]);
    const [pending, setPending] = useState(0);
    const [visible, setVisible] = useState(false);
    const [marking, setMarking] = useState(false);

    const load = async () => {
        try {
            const summary = await api.get('/notifications/summary', { limit: 80, lang: document.documentElement.lang || 'zh-CN' });
            const list = Array.isArray(summary?.items) ? summary.items : [];
            setItems(list);
            setPending(summary?.unreadCount ?? list.filter(item => !item.read).length);
        } catch (error) {
            /* 通知属于辅助信息，失败不打扰 */
        }
    };

    useEffect(() => { load(); }, []);
    useInterval(load, 60000);

    const markSeen = async () => {
        const ids = items.filter(item => !item.read).map(item => item.id).filter(Boolean);
        if (!ids.length || marking) return;
        setMarking(true);
        try {
            await api.post('/notifications/read', { eventIds: ids });
            await load();
        } catch (error) {
            toastError(error);
        } finally {
            setMarking(false);
        }
    };

    const openItem = item => {
        setVisible(false);
        if (item.type === 'vulnerability_created' && item.vulnerabilityId) navigate(`/vulnerabilities?id=${encodeURIComponent(item.vulnerabilityId)}`);
        else if ((item.type === 'task_completed' || item.type === 'long_running_tasks') && item.conversationId) navigate(`/chat?conversation=${encodeURIComponent(item.conversationId)}`);
        else if (item.type === 'task_failed') navigate('/mcp-monitor');
        else if (item.type === 'hitl_pending') navigate('/hitl');
        else if (item.type === 'c2_session_online') navigate('/c2-sessions');
    };

    const content = (
        <div className="cs-notification-panel">
            <div className="cs-notification-head">
                <Text strong>{t('notifications.title', null, '通知')}</Text>
                <Button size="small" theme="borderless" onClick={markSeen} loading={marking}>{t('notifications.markAllRead', null, '全部标记已读')}</Button>
            </div>
            <div className="cs-notification-list">
                {items.length === 0 && <Text type="tertiary">{t('notifications.empty', null, '暂无新通知')}</Text>}
                {items.map(item => (
                    <button type="button" key={item.id || `${item.type}-${item.ts}`} className="cs-notification-item" onClick={() => openItem(item)}>
                        <span className={`cs-notification-dot level-${item.level || 'p2'}`} />
                        <span className="cs-notification-body">
                            <span className="cs-notification-title">{item.title || item.message || item.type}</span>
                            <span className="cs-notification-meta">
                                {item.ts ? formatDateTime(item.ts) : ''}
                                {item.count > 1 ? ` · ×${item.count}` : ''}
                            </span>
                        </span>
                    </button>
                ))}
            </div>
        </div>
    );

    return (
        <Popover content={content} trigger="click" position="bottomRight" visible={visible} onVisibleChange={setVisible}>
            <Badge count={pending || null} overflowCount={99} type="danger">
                <Button theme="borderless" type="tertiary" icon={<IconBell />} aria-label={t('notifications.title', null, '通知')} />
            </Badge>
        </Popover>
    );
}

function UserMenu({ navigate }) {
    const { user, logout, refresh } = useAuth();
    const t = useT();
    const [passwordOpen, setPasswordOpen] = useState(false);
    const passwordFormId = useId();
    const [formApi, setFormApi] = useState(null);
    const [saving, setSaving] = useState(false);

    const submitPassword = async values => {
        setSaving(true);
        try {
            await api.post('/auth/change-password', { oldPassword: values.oldPassword, newPassword: values.newPassword });
            toastSuccess(t('auth.passwordChanged', null, '密码已更新'));
            setPasswordOpen(false);
            formApi?.reset();
        } catch (error) {
            toastError(error, t('auth.changeFailed', null, '修改密码失败'));
        } finally {
            setSaving(false);
        }
    };

    return (
        <>
            <Dropdown
                trigger="click"
                position="bottomRight"
                menu={[
                        { node: 'title', name: user?.display_name || user?.username || 'admin' },
                        {
                            node: 'item',
                            name: t('auth.changePassword', null, '修改密码'),
                            icon: <IconKey />,
                            onClick: () => setPasswordOpen(true)
                        },
                        {
                            node: 'item',
                            name: t('auth.refreshSession', null, '刷新会话权限'),
                            onClick: () => refresh().then(() => toastSuccess(t('auth.sessionRefreshed', null, '会话已刷新'))).catch(error => toastError(error))
                        },
                        { node: 'divider' },
                        {
                            node: 'item',
                            name: t('auth.logout', null, '退出登录'),
                            icon: <IconExit />,
                            onClick: async () => { await logout(); navigate('/dashboard'); }
                        }
                ]}
            >
                <Button theme="borderless" type="tertiary" className="cs-user-trigger" aria-label={t('auth.userMenu', null, '用户菜单')}>
                    <Avatar size="extra-small" color="blue">{(user?.display_name || user?.username || 'A').slice(0, 1).toUpperCase()}</Avatar>
                    <span className="cs-user-name">{user?.display_name || user?.username || 'admin'}</span>
                </Button>
            </Dropdown>
            <Drawer
                title={t('auth.changePassword', null, '修改密码')}
                visible={passwordOpen}
                onCancel={() => setPasswordOpen(false)}
                formId={passwordFormId}
                okText={t('common.save', null, '保存')}
                confirmLoading={saving}
                width={420}
            >
                <Form id={passwordFormId} onSubmit={submitPassword} getFormApi={setFormApi}>
                    <Form.Input field="oldPassword" label={t('auth.currentPassword', null, '当前密码')} type="password" rules={[{ required: true, message: t('auth.required', null, '必填') }]} />
                    <Form.Input field="newPassword" label={t('auth.newPassword', null, '新密码')} type="password" rules={[{ required: true, min: 8, message: t('auth.passwordMin', null, '至少 8 位') }]} />
                    <Form.Input field="confirmPassword" label={t('auth.confirmPassword', null, '确认新密码')} type="password" rules={[
                        { required: true, message: t('auth.required', null, '必填') },
                        { validator: (rule, value) => value === formApi?.getValue('newPassword'), message: t('auth.passwordMismatch', null, '两次输入不一致') }
                    ]} />
                </Form>
            </Drawer>
        </>
    );
}

export default function AppLayout() {
    const { session, hasPermission, hasAnyPermission } = useAuth();
    const versionId = useId();
    const t = useT();
    const location = useLocation();
    const navigate = useNavigate();
    const mobile = useIsMobile();
    const [collapsed, setCollapsed] = useState(() => localStorage.getItem('csai-nav-collapsed') === '1');
    const [drawerOpen, setDrawerOpen] = useState(false);
    const [openKeys, setOpenKeys] = useState([]);

    const segments = location.pathname.replace(/^\//, '').split('/');
    const pageId = segments[0] || 'dashboard';
    const page = findPage(pageId);
    const selectedKeys = useMemo(() => [pageId], [pageId]);
    const navItems = useMemo(() => buildNavItems(NAV_TREE, t, hasPermission, hasAnyPermission), [t, hasPermission, hasAnyPermission]);

    useEffect(() => {
        setDrawerOpen(false);
        if (page?.parent) setOpenKeys(keys => keys.includes(page.parent) ? keys : [...keys, page.parent]);
    }, [location.pathname, page?.parent]);

    useEffect(() => {
        document.title = `${page ? t(page.i18n, null, page.fallback) : '工作台'} · CyberStrikeAI`;
    }, [page, t]);

    const onSelect = ({ itemKey }) => {
        const search = location.search;
        const carry = ['chat', 'vulnerabilities', 'settings'].includes(itemKey) && pageId === itemKey ? search : '';
        navigate(`/${itemKey}${carry}`);
    };

    const toggleCollapsed = () => {
        setCollapsed(current => {
            localStorage.setItem('csai-nav-collapsed', current ? '0' : '1');
            return !current;
        });
    };

    const compact = collapsed && !mobile;
    const version = !compact && session?.appVersion;
    const navNode = (
        <div className={`cs-sidebar-inner ${compact ? 'is-compact' : ''}`}>
            <Button className="cs-brand" theme="borderless" type="tertiary" onClick={() => navigate('/dashboard')}
                aria-label="CyberStrikeAI" aria-describedby={version ? versionId : undefined}>
                <img src="/static/logo.png" alt="" className="cs-brand-logo" />
                {!compact && (
                    <span className="cs-brand-copy">
                        <span className="cs-brand-name">CyberStrike<span>AI</span></span>
                        {version && <span id={versionId} className="cs-brand-version" dir="ltr"
                            title={`${t('header.version', null, '当前版本')} ${version}`}>{version}</span>}
                    </span>
                )}
            </Button>
            <div className="cs-nav-scroll">
                {NAV_SECTIONS.map(section => {
                    const items = navItems.filter(item => section.pages.includes(item.itemKey));
                    if (!items.length) return null;
                    return (
                        <section className="cs-nav-section" key={section.id} aria-label={t(section.i18n, null, section.fallback)}>
                            {!compact && <div className="cs-nav-section-label">{t(section.i18n, null, section.fallback)}</div>}
                            <Nav
                                className="cs-nav"
                                style={{ width: '100%' }}
                                items={items}
                                selectedKeys={selectedKeys}
                                onSelect={onSelect}
                                isCollapsed={compact}
                                subDropdownProps={sidebarSubmenuProps}
                                openKeys={openKeys}
                                onOpenChange={({ openKeys: keys }) => setOpenKeys(keys)}
                            />
                        </section>
                    );
                })}
            </div>
            {!mobile && (
                <div className="cs-nav-footer">
                    <Button theme="borderless" type="tertiary" block icon={compact ? <IconChevronRight /> : <IconChevronLeft />}
                        aria-label={t(compact ? 'nav.expand' : 'nav.collapse', null, compact ? '展开菜单' : '收起菜单')}
                        onClick={toggleCollapsed}>
                        {!compact && t('nav.collapse', null, '收起菜单')}
                    </Button>
                </div>
            )}
        </div>
    );

    const title = page ? t(page.i18n, null, page.fallback) : 'CyberStrikeAI';
    const parentPage = page?.parent ? findPage(page.parent) : null;
    const parentTitle = parentPage ? t(parentPage.i18n, null, parentPage.fallback) : '';

    return (
        <div className={`cs-shell ${collapsed && !mobile ? 'is-collapsed' : ''}`}>
            {!mobile && <aside className="cs-sider">{navNode}</aside>}
            {mobile && (
                <SideSheet
                    visible={drawerOpen}
                    onCancel={() => setDrawerOpen(false)}
                    placement="left"
                    width={264}
                    title={t('nav.navigation', null, '导航')}
                    bodyStyle={{ padding: 0 }}
                    className="cs-mobile-nav"
                >
                    {navNode}
                </SideSheet>
            )}
            <div className="cs-main">
                <header className="cs-header">
                    <div className="cs-header-left">
                        {mobile && (
                            <Button theme="borderless" icon={<IconMenu />} onClick={() => setDrawerOpen(true)} aria-label={t('common.expandPanel', null, '展开面板')} />
                        )}
                        <Breadcrumb className="cs-header-crumb" compact={false}>
                            <Breadcrumb.Item onClick={() => navigate('/dashboard')}>{t('nav.groupWorkspace', null, '工作台')}</Breadcrumb.Item>
                            {parentTitle && <Breadcrumb.Item>{parentTitle}</Breadcrumb.Item>}
                            <Breadcrumb.Item>{title}</Breadcrumb.Item>
                        </Breadcrumb>
                    </div>
                    <Space className="cs-header-actions" spacing={4}>
                        <NotificationsBell navigate={navigate} />
                        <LanguageMenu />
                        <ThemeMenu />
                        <UserMenu navigate={navigate} />
                    </Space>
                </header>
                <main className={`cs-content ${pageId === 'chat' ? 'cs-content-workspace' : ''}`} id="main-content">
                    <Outlet context={{ navigate, pageId, mobile }} />
                </main>
            </div>
        </div>
    );
}
