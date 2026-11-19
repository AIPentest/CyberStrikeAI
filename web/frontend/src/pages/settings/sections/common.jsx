/** 系统设置各分区的共用小件：权限提示、加载骨架、只读键值行。 */
import { Banner, Skeleton, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../../lib/i18n';

const { Text } = Typography;

export function PermissionBanner({ permission, label }) {
    const t = useT();
    return (
        <Banner
            type="warning"
            bordered
            closeIcon={null}
            description={t(
                'settings.permissionMissing',
                { permission },
                `当前账号缺少 ${permission} 权限，无法${label || '访问该分区'}。`
            )}
        />
    );
}

export function LoadingBlock({ rows = 4 }) {
    return (
        <div className="settings-loading-block">
            <Skeleton placeholder={<Skeleton.Paragraph rows={rows} />} loading>
                <div style={{ height: rows * 18 }} />
            </Skeleton>
        </div>
    );
}

export function Hint({ children }) {
    return <Text type="tertiary" size="small" className="settings-hint">{children}</Text>;
}

export function Mono({ children }) {
    return <span className="settings-mono">{children}</span>;
}
