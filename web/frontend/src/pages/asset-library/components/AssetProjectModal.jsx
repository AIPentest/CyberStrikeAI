/** 批量绑定项目：PUT /assets/project-binding，支持解绑（project_id 置空）。 */
import { useEffect, useMemo, useState } from 'react';
import { Banner, Select, Space, Switch, Typography } from '@douyinfe/semi-ui';
import { api, errorMessage } from '../../../lib/http';
import { useT } from '../../../lib/i18n';
import { Drawer, toastSuccess } from '../../../components/ui';

const { Text } = Typography;

export default function AssetProjectModal({ visible, projects, assetIds, onClose, onDone }) {
    const t = useT();
    const [projectId, setProjectId] = useState('');
    const [unbind, setUnbind] = useState(false);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);

    const ids = useMemo(() => (Array.isArray(assetIds) ? assetIds : []), [assetIds]);
    const projectOptions = useMemo(
        () => (projects || []).map(project => ({ value: project.id, label: project.name || project.id })),
        [projects]
    );

    useEffect(() => {
        if (visible) {
            setProjectId('');
            setUnbind(false);
            setError('');
            setBusy(false);
        }
    }, [visible]);

    const submit = async () => {
        if (busy || !ids.length) return;
        if (!unbind && !projectId) {
            setError(t('assets.selectProjectRequired', null, '请选择要绑定的项目'));
            return;
        }
        setBusy(true);
        setError('');
        try {
            await api.put('/assets/project-binding', {
                asset_ids: ids,
                project_id: unbind ? '' : projectId
            });
            toastSuccess(t('assets.bindProjectDone', { count: ids.length }, `已更新 ${ids.length} 个资产`));
            onDone?.();
            onClose?.();
        } catch (submitError) {
            setError(errorMessage(submitError, t('assets.bindProjectFailed', null, '绑定项目失败')));
        } finally {
            setBusy(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            title={t('assets.bindProjectTitle', null, '绑定项目')}
            width={520}
            onCancel={onClose}
            onOk={submit}
            okText={unbind ? t('assets.unbindProject', null, '解除绑定') : t('assets.confirmBinding', null, '确认绑定')}
            confirmLoading={busy}
        >
            <Space vertical align="start" spacing={10} style={{ width: '100%' }}>
                <Text type="tertiary">
                    {t('assets.bindProjectCount', { count: ids.length }, `将更新 ${ids.length} 个资产`)}
                </Text>
                <Select
                    value={projectId || undefined}
                    optionList={projectOptions}
                    filter
                    showClear
                    disabled={unbind}
                    placeholder={t('assets.chooseProject', null, '请选择项目')}
                    onChange={value => setProjectId(value || '')}
                    style={{ width: '100%' }}
                />
                <Space>
                    <Switch checked={unbind} onChange={setUnbind} size="small" />
                    <Text size="small">{t('assets.unbindProject', null, '解除项目绑定')}</Text>
                </Space>
                <Text type="tertiary" size="small">{t('assets.bindProjectHint', null, '所选资产将统一更新为该项目归属。')}</Text>
            </Space>
            {error && <Banner type="danger" bordered closeIcon={null} description={error} className="asset-library-form-error" />}
        </Drawer>
    );
}
