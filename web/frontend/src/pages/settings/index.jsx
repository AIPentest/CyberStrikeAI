/**
 * 系统设置：Tabs 分区，每个分区自己取数、自己处理错误，互不影响。
 * 深链接 #/settings?section=token-usage（与旧页面一致）。
 */
import { useMemo } from 'react';
import { useOutletContext, useSearchParams } from 'react-router-dom';
import { Tabs } from '@douyinfe/semi-ui';
import { PageHeader } from '../../components/ui';
import { useT } from '../../lib/i18n';
import BasicSection from './sections/BasicSection';
import ModelsSection from './sections/ModelsSection';
import TokenUsageSection from './sections/TokenUsageSection';
import StorageSection from './sections/StorageSection';
import UpdateSection from './sections/UpdateSection';
import AuditSection from './sections/AuditSection';
import AppearanceSection from './sections/AppearanceSection';
import './page.css';

const SECTION_KEYS = ['basic', 'models', 'token-usage', 'storage', 'update', 'audit', 'appearance'];

export default function SettingsPage() {
    const t = useT();
    const { mobile } = useOutletContext();
    const [params, setParams] = useSearchParams();

    const requested = params.get('section');
    const activeKey = useMemo(
        () => (SECTION_KEYS.includes(requested) ? requested : 'basic'),
        [requested]
    );

    const change = key => {
        const next = new URLSearchParams(params);
        next.set('section', key);
        setParams(next, { replace: true });
    };

    return (
        <div className="settings-page">
            <PageHeader
                title={t('settings.title', null, '系统设置')}
                description={t('settings.pageDesc', null, '配置、模型探测、Token 用量、存储清理、审计日志与界面偏好；每个分区独立加载，单区失败不影响其他分区。')}
            />
            <Tabs
                type="line"
                tabPosition={mobile ? 'top' : 'left'}
                collapsible={mobile}
                lazyRender
                activeKey={activeKey}
                onChange={change}
                className="settings-tabs"
            >
                <Tabs.TabPane itemKey="basic" tab={t('settings.nav.basic', null, '基础配置')}>
                    <BasicSection />
                </Tabs.TabPane>
                <Tabs.TabPane itemKey="models" tab={t('settings.nav.models', null, '模型与工具')}>
                    <ModelsSection />
                </Tabs.TabPane>
                <Tabs.TabPane itemKey="token-usage" tab={t('settings.nav.tokenUsage', null, 'Token 用量')}>
                    <TokenUsageSection />
                </Tabs.TabPane>
                <Tabs.TabPane itemKey="storage" tab={t('settings.nav.storage', null, '存储清理')}>
                    <StorageSection />
                </Tabs.TabPane>
                <Tabs.TabPane itemKey="update" tab={t('settings.nav.update', null, '更新状态')}>
                    <UpdateSection />
                </Tabs.TabPane>
                <Tabs.TabPane itemKey="audit" tab={t('settings.nav.audit', null, '审计日志')}>
                    <AuditSection />
                </Tabs.TabPane>
                <Tabs.TabPane itemKey="appearance" tab={t('settings.nav.appearance', null, '外观')}>
                    <AppearanceSection />
                </Tabs.TabPane>
            </Tabs>
        </div>
    );
}
