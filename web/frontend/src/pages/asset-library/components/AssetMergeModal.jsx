/** 合并重复资产：选择主资产，POST /assets/merge（后端要求两条资产共享域名/IP/Host）。 */
import { useEffect, useMemo, useState } from 'react';
import { Banner, Radio, RadioGroup, Space, Typography } from '@douyinfe/semi-ui';
import { api, errorMessage } from '../../../lib/http';
import { useT } from '../../../lib/i18n';
import { Drawer, toastSuccess } from '../../../components/ui';
import { assetTargetLabel } from './assetTarget';

const { Text } = Typography;

export default function AssetMergeModal({ visible, assets, onClose, onDone }) {
    const t = useT();
    const list = useMemo(() => (Array.isArray(assets) ? assets : []), [assets]);
    const [primaryId, setPrimaryId] = useState('');
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);

    useEffect(() => {
        if (visible) {
            setPrimaryId(list[0]?.id || '');
            setError('');
            setBusy(false);
        }
    }, [visible, list]);

    const submit = async () => {
        if (busy || list.length < 2) return;
        setBusy(true);
        setError('');
        try {
            const result = await api.post('/assets/merge', {
                asset_ids: list.map(item => item.id),
                primary_id: primaryId || list[0].id
            });
            toastSuccess(`已合并 ${Number(result?.merged || list.length)} 个资产`);
            onDone?.();
            onClose?.();
        } catch (submitError) {
            setError(errorMessage(submitError, '合并资产失败'));
        } finally {
            setBusy(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            title={t('assets.mergeDuplicates', null, '合并重复资产')}
            width={560}
            onCancel={onClose}
            onOk={submit}
            okText={t('assets.merge', null, '合并')}
            okButtonProps={{ disabled: list.length < 2 }}
            confirmLoading={busy}
        >
            {list.length < 2 ? (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    description={t('assets.mergeRequiresMultiple', null, '请至少选择两个重复资产')}
                />
            ) : (
                <Space vertical align="start" spacing={8} style={{ width: '100%' }}>
                    <Text type="tertiary">
                        {t('assets.mergeHint', null, '将保留一个主资产，其余资产的空字段与标签会合并进来，然后删除被合并记录。所选资产必须共享域名、IP 或 Host。')}
                    </Text>
                    <RadioGroup value={primaryId} onChange={event => setPrimaryId(event.target.value)}>
                        <Space vertical align="start" spacing={4}>
                            {list.map(item => (
                                <Radio key={item.id} value={item.id}>
                                    {assetTargetLabel(item)}
                                    {item.project_name ? ` · ${item.project_name}` : ''}
                                </Radio>
                            ))}
                        </Space>
                    </RadioGroup>
                </Space>
            )}
            {error && <Banner type="danger" bordered closeIcon={null} description={error} className="asset-library-form-error" />}
        </Drawer>
    );
}
