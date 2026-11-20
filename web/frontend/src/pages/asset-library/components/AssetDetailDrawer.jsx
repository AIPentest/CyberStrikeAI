/** 资产详情抽屉：全字段 + 关联漏洞/项目跳转。 */
import { useNavigate } from 'react-router-dom';
import { Button, Divider, SideSheet, Space, Typography } from '@douyinfe/semi-ui';
import { CopyButton, KeyValueList } from '../../../components/ui';
import { formatDateTime } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { AssetStatusTag, ProtocolTag, RiskTag, TagList } from './AssetBits';
import { criticalityLabel, environmentLabel, riskLabel } from './assetConstants';
import { assetServiceLabel, assetTargetLabel } from './assetTarget';

const { Text } = Typography;

export default function AssetDetailDrawer({ asset, visible, onClose, onEdit, onDelete, canWrite, canDelete }) {
    const t = useT();
    const navigate = useNavigate();
    if (!asset) return null;

    const openVulnerabilities = () => {
        if (asset.last_scan_task_id) {
            navigate(`/vulnerabilities?task_id=${encodeURIComponent(asset.last_scan_task_id)}`);
            return;
        }
        if (asset.last_scan_conversation_id) {
            navigate(`/vulnerabilities?conversation_id=${encodeURIComponent(asset.last_scan_conversation_id)}`);
            return;
        }
        const host = asset.host || asset.domain || asset.ip || '';
        navigate(host ? `/vulnerabilities?host_q=${encodeURIComponent(host)}` : '/vulnerabilities');
    };

    const items = [
        { label: 'Host', value: asset.host || '—' },
        { label: 'IP', value: asset.ip || '—' },
        { label: t('assets.domain', null, '域名'), value: asset.domain || '—' },
        { label: t('assets.service', null, '服务'), value: <ProtocolTag protocol={asset.protocol} port={asset.port} /> },
        { label: t('assets.project', null, '所属项目'), value: asset.project_name || asset.project_id || '—' },
        { label: t('assets.status', null, '状态'), value: <AssetStatusTag status={asset.status} /> },
        { label: t('assets.riskLevel', null, '风险等级'), value: <RiskTag asset={asset} /> },
        { label: t('assets.environment', null, '环境'), value: environmentLabel(asset.environment) || '—' },
        { label: t('assets.criticality', null, '重要性'), value: criticalityLabel(asset.criticality) || '—' },
        {
            label: t('assets.location', null, '地区'),
            value: [asset.country, asset.province, asset.city].filter(Boolean).join(' / ') || '—'
        },
        { label: t('assets.responsiblePerson', null, '负责人'), value: asset.responsible_person || '—' },
        { label: t('assets.department', null, '部门'), value: asset.department || '—' },
        { label: t('assets.businessSystem', null, '业务系统'), value: asset.business_system || '—' },
        { label: t('assets.title', null, '标题/指纹'), value: asset.title || '—' },
        { label: t('assets.server', null, '服务指纹'), value: asset.server || '—' },
        { label: t('assets.source', null, '来源'), value: asset.source || '—' },
        {
            label: t('assets.sourceQuery', null, '来源查询'),
            value: asset.source_query ? <Text code size="small">{asset.source_query}</Text> : '—'
        },
        { label: t('assets.tagsLabel', null, '标签'), value: <TagList tags={asset.tags} max={6} /> },
        { label: t('assets.firstSeen', null, '首次发现'), value: formatDateTime(asset.first_seen_at) },
        { label: t('assets.lastSeen', null, '最近发现'), value: formatDateTime(asset.last_seen_at) },
        { label: t('assets.lastScan', null, '上次扫描'), value: formatDateTime(asset.last_scan_at) },
        { label: '创建时间', value: formatDateTime(asset.created_at) },
        { label: '更新时间', value: formatDateTime(asset.updated_at) },
        { label: '资产 ID', value: <Text copyable={{ content: asset.id }} size="small">{asset.id}</Text> }
    ];

    return (
        <SideSheet
            visible={visible}
            onCancel={onClose}
            title={t('assets.detailTitle', null, '资产详情')}
            width={620}
            placement="right"
            footer={(
                <Space>
                    {canWrite && <Button onClick={() => onEdit(asset)}>{t('common.edit', null, '编辑')}</Button>}
                    {canDelete && (
                        <Button type="danger" onClick={() => onDelete(asset)}>
                            {t('common.delete', null, '删除')}
                        </Button>
                    )}
                    <Button theme="borderless" onClick={onClose}>{t('common.close', null, '关闭')}</Button>
                </Space>
            )}
        >
            <div className="asset-library-detail-target">
                <div className="asset-library-detail-target-main">
                    <Text strong size="large">{assetTargetLabel(asset)}</Text>
                    <CopyButton text={assetTargetLabel(asset)} />
                </div>
                <Text type="tertiary">
                    {[asset.ip, asset.domain, assetServiceLabel(asset)].filter(Boolean).join(' · ') || '—'}
                </Text>
                <Space spacing={6} wrap className="asset-library-detail-badges">
                    <AssetStatusTag status={asset.status} />
                    <RiskTag asset={asset} />
                    <Text type="tertiary" size="small">
                        {t('assets.relatedVulnerabilities', null, '相关漏洞')}：
                        {Number(asset.vulnerability_count || 0)}
                        {asset.risk_level ? `（${riskLabel(asset.risk_level)}）` : ''}
                    </Text>
                </Space>
            </div>
            <Divider margin="12px" />
            <KeyValueList items={items} columns={2} />
            <Divider margin="12px" />
            <Space>
                <Button theme="light" onClick={openVulnerabilities}>
                    {t('assets.viewRelatedVulnerabilities', null, '查看关联漏洞')}
                </Button>
                {asset.project_id && (
                    <Button
                        theme="borderless"
                        onClick={() => navigate(`/projects?id=${encodeURIComponent(asset.project_id)}`)}
                    >
                        {t('assets.viewProject', null, '查看项目')}
                    </Button>
                )}
            </Space>
        </SideSheet>
    );
}
