/** 人机协同：待审批 / 审批日志 / 白名单 / 审核策略。 */
import { useCallback, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Badge, Banner, Button, Space, Tabs, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { PageHeader } from '../../components/ui';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import PendingPanel from './pending-panel';
import LogsPanel from './logs-panel';
import WhitelistPanel from './whitelist-panel';
import StrategyPanel from './strategy-panel';
import './page.css';

const { Text } = Typography;
const TAB_KEYS = ['pending', 'logs', 'whitelist', 'strategy'];

export default function HitlPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('hitl:read');
    const canWrite = hasPermission('hitl:write');
    const [params, setParams] = useSearchParams();
    const [pendingCount, setPendingCount] = useState(0);
    const [refreshKey, setRefreshKey] = useState(0);

    const tabParam = params.get('tab');
    const logId = params.get('id') || '';
    const convId = params.get('conv') || '';
    const tab = TAB_KEYS.includes(tabParam)
        ? tabParam
        : (logId ? 'logs' : (convId ? 'strategy' : 'pending'));

    const updateParams = useCallback(patch => {
        const next = new URLSearchParams(params);
        Object.entries(patch).forEach(([key, value]) => {
            if (value === null || value === undefined || value === '') next.delete(key);
            else next.set(key, value);
        });
        setParams(next, { replace: true });
    }, [params, setParams]);

    const switchTab = useCallback(nextTab => {
        updateParams({ tab: nextTab, id: null });
    }, [updateParams]);

    const openLog = useCallback(id => updateParams({ tab: 'logs', id }), [updateParams]);
    const closeLog = useCallback(() => updateParams({ id: null }), [updateParams]);

    const tabLabel = (key, fallback) => (key === 'pending' && pendingCount > 0
        ? (
            <span className="hitl-tab-label">
                {fallback}
                <Badge count={pendingCount} type="danger" />
            </span>
        )
        : fallback);

    return (
        <div className="hitl-page">
            <PageHeader
                title={t('hitl.pageTitle', null, '人机协同审批')}
                description="敏感工具调用先进入审批队列；审计 Agent 可按策略自动裁决，白名单内工具直接放行。"
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={() => setRefreshKey(key => key + 1)}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                    </Space>
                )}
            >
                {!canWrite && <Text type="tertiary" size="small">当前账号只读，审批与保存操作已禁用。</Text>}
            </PageHeader>

            {!canRead && (
                <Banner
                    type="warning"
                    bordered
                    description="当前账号缺少 hitl:read 权限，无法读取待审批与审计日志。"
                />
            )}

            {canRead && (
                <Tabs
                    type="line"
                    activeKey={tab}
                    onChange={switchTab}
                    className="hitl-tabs"
                >
                    <Tabs.TabPane itemKey="pending" tab={tabLabel('pending', t('hitl.tabPending', null, '待审批'))}>
                        {tab === 'pending' && (
                            <PendingPanel
                                canWrite={canWrite}
                                refreshKey={refreshKey}
                                onCountChange={setPendingCount}
                            />
                        )}
                    </Tabs.TabPane>
                    <Tabs.TabPane itemKey="logs" tab={t('hitl.tabLogs', null, '审批日志')}>
                        {tab === 'logs' && (
                            <LogsPanel
                                canWrite={canWrite}
                                refreshKey={refreshKey}
                                detailId={logId}
                                onOpenDetail={openLog}
                                onCloseDetail={closeLog}
                            />
                        )}
                    </Tabs.TabPane>
                    <Tabs.TabPane itemKey="whitelist" tab={t('hitl.tabWhitelist', null, '白名单')}>
                        {tab === 'whitelist' && (
                            <WhitelistPanel canWrite={canWrite} refreshKey={refreshKey} />
                        )}
                    </Tabs.TabPane>
                    <Tabs.TabPane itemKey="strategy" tab={t('hitl.tabStrategy', null, '审核策略')}>
                        {tab === 'strategy' && (
                            <StrategyPanel
                                canWrite={canWrite}
                                refreshKey={refreshKey}
                                initialConversationId={convId}
                            />
                        )}
                    </Tabs.TabPane>
                </Tabs>
            )}
        </div>
    );
}
