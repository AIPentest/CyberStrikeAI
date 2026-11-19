/** MCP 管理：外部 MCP 配置 + 工具启用状态。 */
import { useCallback, useState } from 'react';
import { Button, Space } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { ErrorBanner, PageHeader, StatGrid } from '../../components/ui';
import { useResource } from '../../components/hooks';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import ExternalMcpPanel from './external-mcp';
import ToolsPanel from './tools-panel';
import './page.css';

export default function McpManagementPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('mcp:write');
    const [refreshKey, setRefreshKey] = useState(0);
    const stats = useResource(() => api.get('/external-mcp/stats'), [refreshKey]);
    const data = stats.data || {};

    const reloadStats = useCallback(() => {
        setRefreshKey(key => key + 1);
        stats.reload({ silent: true });
    }, [stats.reload]); // eslint-disable-line react-hooks/exhaustive-deps

    return (
        <div className="mcp-management-page">
            <PageHeader
                title={t('mcp.managementTitle', null, 'MCP 管理')}
                description="维护外部 MCP 连接与工具启用状态；改动写入 config.yaml 后立即生效。"
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={reloadStats} loading={stats.loading}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                    </Space>
                )}
            />

            <StatGrid
                columns={4}
                metrics={[
                    {
                        id: 'total',
                        label: t('mcp.totalCount', null, '总数'),
                        value: stats.error ? '—' : (data.total ?? 0),
                        detail: t('mcp.externalConfig', null, '外部 MCP 配置'),
                        loading: stats.loading && !stats.data
                    },
                    {
                        id: 'enabled',
                        label: t('mcp.enabledCount', null, '已启用'),
                        value: stats.error ? '—' : (data.enabled ?? 0),
                        tone: 'success',
                        loading: stats.loading && !stats.data
                    },
                    {
                        id: 'connected',
                        label: t('mcp.connectedCount', null, '已连接'),
                        value: stats.error ? '—' : (data.connected ?? 0),
                        tone: data.connected > 0 ? 'success' : undefined,
                        loading: stats.loading && !stats.data
                    },
                    {
                        id: 'disabled',
                        label: t('mcp.disabledCount', null, '已停用'),
                        value: stats.error ? '—' : (data.disabled ?? 0),
                        loading: stats.loading && !stats.data
                    }
                ]}
            />

            <ErrorBanner error={stats.error} onRetry={reloadStats} />

            <ExternalMcpPanel canWrite={canWrite} onChanged={reloadStats} refreshKey={refreshKey} />
            <ToolsPanel canWrite={canWrite} refreshKey={refreshKey} />
        </div>
    );
}
