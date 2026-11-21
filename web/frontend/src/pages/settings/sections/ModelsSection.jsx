/** 模型与工具：通道配置、模型连接探测和工具开关。 */
import { useCallback } from 'react';
import { api } from '../../../lib/http';
import { useAuth } from '../../../lib/auth';
import { useT } from '../../../lib/i18n';
import { useResource } from '../../../components/hooks';
import { ErrorBanner } from '../../../components/ui';
import { LoadingBlock, PermissionBanner } from './common';
import ProbePanel from './ProbePanel';
import ToolsPanel from './ToolsPanel';

export default function ModelsSection() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('config:read');
    const loader = useCallback(() => api.get('/config'), []);
    const config = useResource(loader, [canRead], { skip: !canRead });

    if (!canRead) {
        return <PermissionBanner permission="config:read" label={t('settings.nav.models', null, '读取模型与工具配置')} />;
    }

    return (
        <>
            {config.loading && !config.data ? <LoadingBlock rows={6} /> : (
                <>
                    <ErrorBanner error={config.error} onRetry={config.reload} />
                    {config.data && (
                        <ProbePanel
                            config={config.data}
                            canWrite={hasPermission('config:write')}
                            reloadConfig={config.reload}
                        />
                    )}
                </>
            )}
            <ToolsPanel />
        </>
    );
}
