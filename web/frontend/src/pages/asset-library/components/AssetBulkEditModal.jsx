/** 批量编辑：PUT /assets/bulk，仅提交填写的字段，空白保持原值。 */
import { useEffect, useMemo, useState } from 'react';
import { Banner, Input, Select, Typography } from '@douyinfe/semi-ui';
import { api, errorMessage } from '../../../lib/http';
import { useT } from '../../../lib/i18n';
import { Drawer, toastSuccess } from '../../../components/ui';
import { CRITICALITY_OPTIONS, ENVIRONMENT_OPTIONS, STATUS_OPTIONS } from './assetConstants';
import { splitTags } from './assetTarget';

const { Text } = Typography;

const EMPTY = {
    status: '', environment: '', criticality: '',
    responsible_person: '', department: '', business_system: '',
    add_tags: '', remove_tags: ''
};

export default function AssetBulkEditModal({ visible, assetIds, onClose, onDone }) {
    const t = useT();
    const [values, setValues] = useState(EMPTY);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);

    const ids = useMemo(() => (Array.isArray(assetIds) ? assetIds : []), [assetIds]);

    useEffect(() => {
        if (visible) {
            setValues(EMPTY);
            setError('');
            setBusy(false);
        }
    }, [visible]);

    const patch = next => setValues(prev => ({ ...prev, ...next }));

    const submit = async () => {
        if (busy || !ids.length) return;
        const body = { asset_ids: ids };
        ['status', 'environment', 'criticality', 'responsible_person', 'department', 'business_system']
            .forEach(key => {
                const value = String(values[key] || '').trim();
                if (value) body[key] = value;
            });
        const addTags = splitTags(values.add_tags);
        const removeTags = splitTags(values.remove_tags);
        if (addTags.length) body.add_tags = addTags;
        if (removeTags.length) body.remove_tags = removeTags;
        if (Object.keys(body).length === 1) {
            setError('请至少填写一项需要修改的内容');
            return;
        }
        setBusy(true);
        setError('');
        try {
            const result = await api.put('/assets/bulk', body);
            toastSuccess(`已更新 ${Number(result?.updated || ids.length)} 个资产`);
            onDone?.();
            onClose?.();
        } catch (submitError) {
            setError(errorMessage(submitError, '批量编辑失败'));
        } finally {
            setBusy(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            title={t('assets.bulkEdit', null, '批量编辑')}
            width={620}
            onCancel={onClose}
            onOk={submit}
            confirmLoading={busy}
        >
            <Text type="tertiary">将修改 {ids.length} 个资产；空白字段保持原值。</Text>
            <div className="asset-library-form-grid asset-library-form-grid--bulk">
                <Select
                    value={values.status || undefined}
                    placeholder={t('assets.status', null, '状态')}
                    optionList={STATUS_OPTIONS}
                    showClear
                    onChange={value => patch({ status: value || '' })}
                />
                <Select
                    value={values.environment || undefined}
                    placeholder={t('assets.environment', null, '环境')}
                    optionList={ENVIRONMENT_OPTIONS}
                    showClear
                    onChange={value => patch({ environment: value || '' })}
                />
                <Select
                    value={values.criticality || undefined}
                    placeholder={t('assets.criticality', null, '重要性')}
                    optionList={CRITICALITY_OPTIONS}
                    showClear
                    onChange={value => patch({ criticality: value || '' })}
                />
                <Input
                    value={values.responsible_person}
                    placeholder={t('assets.responsiblePerson', null, '负责人')}
                    onChange={value => patch({ responsible_person: value })}
                />
                <Input
                    value={values.department}
                    placeholder={t('assets.department', null, '部门')}
                    onChange={value => patch({ department: value })}
                />
                <Input
                    value={values.business_system}
                    placeholder={t('assets.businessSystem', null, '业务系统')}
                    onChange={value => patch({ business_system: value })}
                />
                <Input
                    value={values.add_tags}
                    placeholder="追加标签（逗号分隔）"
                    onChange={value => patch({ add_tags: value })}
                />
                <Input
                    value={values.remove_tags}
                    placeholder="移除标签（逗号分隔）"
                    onChange={value => patch({ remove_tags: value })}
                />
            </div>
            {error && <Banner type="danger" bordered closeIcon={null} description={error} className="asset-library-form-error" />}
        </Drawer>
    );
}
