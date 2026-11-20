import { useRef, useState } from 'react';
import { Button, Form, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconPlus } from '@douyinfe/semi-icons';
import { Link } from 'react-router-dom';
import { useResource } from '../../components/hooks';
import { DataTable, Drawer, ErrorBanner, SectionCard, toastSuccess } from '../../components/ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';

const { Text } = Typography;
const PAGE_SIZE = 20;
const KINDS = {
    cloud_access_key: 'AK/SK',
    credential: '账号凭据',
    api_token: 'API Key / Token',
    other: '其他线索'
};

function SurfaceItemEditor({ endpoint, item, onClose, onSaved }) {
    const t = useT();
    const form = useRef(null);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState(null);
    const title = item.id
        ? t('deepAssessments.surface.edit', null, '编辑线索')
        : t('deepAssessments.surface.add', null, '新增线索');
    const options = Object.entries(KINDS).map(([value, label]) => ({
        value, label: t(`deepAssessments.surface.kinds.${value}`, null, label)
    }));

    const submit = async ({ kind, summary = '', location = '', fact_id = '' }) => {
        setSaving(true);
        setError(null);
        const body = { kind, summary: summary.trim(), location: location.trim(), fact_id: fact_id.trim() };
        try {
            if (item.id) {
                await api.put(`${endpoint}/${encodeURIComponent(item.id)}`, { ...body, revision: item.revision });
            } else {
                await api.post(endpoint, body);
            }
            toastSuccess(t('deepAssessments.surface.saved', null, '已保存攻击面线索'));
            onSaved();
        } catch (error) {
            setError(error);
        } finally {
            setSaving(false);
        }
    };

    return (
        <Drawer
            visible
            title={title}
            aria-label={title}
            onCancel={onClose}
            width={520}
            closable={!saving}
            closeOnEsc={!saving}
            maskClosable={!saving}
            footer={(
                <div className="assessment-form-actions">
                    <Button onClick={onClose} disabled={saving}>{t('common.cancel', null, '取消')}</Button>
                    <Button theme="solid" onClick={() => form.current?.submitForm()} loading={saving} disabled={saving}>
                        {t('common.save', null, '保存')}
                    </Button>
                </div>
            )}
        >
            <Text type="tertiary">{t('deepAssessments.surface.redactionHint', null, '请填写脱敏信息。密钥、密码和令牌原文应留在原证据位置。')}</Text>
            <Form
                getFormApi={instance => { form.current = instance; }}
                initValues={item}
                labelPosition="top"
                onSubmit={submit}
                disabled={saving}
            >
                <Form.RadioGroup
                    field="kind"
                    label={t('deepAssessments.surface.kind', null, '类型')}
                    options={options}
                    direction="vertical"
                    rules={[{ required: true, message: t('deepAssessments.surface.kindRequired', null, '请选择线索类型') }]}
                />
                <Form.Input
                    field="summary"
                    label={t('deepAssessments.surface.summary', null, '脱敏摘要')}
                    maxLength={120}
                    rules={[{ required: true, whitespace: true, message: t('deepAssessments.surface.summaryRequired', null, '请填写脱敏摘要') }]}
                />
                <Form.Input field="location" label={t('deepAssessments.surface.location', null, '发现位置')} maxLength={240} />
                <Form.Input
                    field="fact_id"
                    label={t('deepAssessments.surface.factId', null, '关联事实 ID（选填）')}
                    placeholder={t('deepAssessments.surface.factHint', null, '可从本次测评的事实详情复制')}
                    maxLength={128}
                />
                <ErrorBanner error={error} />
            </Form>
        </Drawer>
    );
}

export default function SurfaceItems({ id, canWrite, reloadToken }) {
    const t = useT();
    const [offset, setOffset] = useState(0);
    const [edit, setEdit] = useState(null);
    const endpoint = `/deep-assessments/${id}/surface-items`;
    const resource = useResource(() => api.get(endpoint, { offset }), [endpoint, offset, reloadToken]);
    const items = resource.data?.items || [];

    const columns = [
        {
            title: t('deepAssessments.surface.kind', null, '类型'), dataIndex: 'kind', width: 135,
            render: value => <Tag size="small">{t(`deepAssessments.surface.kinds.${value}`, null, KINDS[value] || value)}</Tag>
        },
        { title: t('deepAssessments.surface.summary', null, '脱敏摘要'), dataIndex: 'summary' },
        { title: t('deepAssessments.surface.location', null, '发现位置'), dataIndex: 'location', render: value => value || '—' },
        {
            title: t('deepAssessments.surface.evidence', null, '证据'), dataIndex: 'fact_id', width: 145,
            render: value => value ? (
                <Link to={`?${new URLSearchParams({ id, tab: 'evidence-path', fact_id: value })}`}>
                    {t('deepAssessments.surface.viewEvidence', null, '查看证据路径')}
                </Link>
            ) : '—'
        },
        ...(canWrite ? [{
            title: t('deepAssessments.actions', null, '操作'), key: 'actions', width: 95,
            render: (_, item) => (
                <Button size="small" theme="borderless" onClick={() => setEdit(item)}>
                    {t('deepAssessments.surface.edit', null, '编辑线索')}
                </Button>
            )
        }] : [])
    ];

    return (
        <SectionCard
            title={t('deepAssessments.surface.title', null, '攻击面线索')}
            extra={canWrite && (
                <Button icon={<IconPlus />} size="small" onClick={() => setEdit({ kind: 'cloud_access_key' })}>
                    {t('deepAssessments.surface.add', null, '新增线索')}
                </Button>
            )}
        >
            <ErrorBanner error={resource.error} onRetry={resource.reload} />
            <DataTable
                columns={columns}
                dataSource={items}
                rowKey="id"
                loading={resource.loading}
                pagination={false}
                emptyText={t('deepAssessments.surface.empty', null, '暂无攻击面线索。')}
            />
            {(offset > 0 || resource.data?.has_more) && (
                <div className="assessment-pager">
                    <Space>
                        <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => value - PAGE_SIZE)}>
                            {t('deepAssessments.previous', null, '上一页')}
                        </Button>
                        <Button size="small" disabled={!resource.data?.has_more} onClick={() => setOffset(value => value + PAGE_SIZE)}>
                            {t('deepAssessments.next', null, '下一页')}
                        </Button>
                    </Space>
                </div>
            )}
            {edit && canWrite && (
                <SurfaceItemEditor
                    endpoint={endpoint}
                    item={edit}
                    onClose={() => setEdit(null)}
                    onSaved={() => {
                        setEdit(null);
                        if (offset === 0) resource.reload();
                        else setOffset(0);
                    }}
                />
            )}
        </SectionCard>
    );
}
