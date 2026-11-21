import { useId, useState } from 'react';
import { Button, Form, Input, Select, Space, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useDebounced, useResource } from '../../components/hooks';
import { Drawer, ErrorBanner, toastSuccess } from '../../components/ui';

const types = [
    ['project', '项目'], ['conversation', '会话'], ['asset', '资产'],
    ['vulnerability', '漏洞'], ['batch_task', '批量任务'],
];

export default function ResourceGrant({ users, onCancel, onSaved }) {
    const formId = useId();
    const [type, setType] = useState(types[0][0]);
    const [query, setQuery] = useState('');
    const [offset, setOffset] = useState(0);
    const [picked, setPicked] = useState([]);
    const [error, setError] = useState(null);
    const [saving, setSaving] = useState(false);
    const q = useDebounced(query, 250);
    const result = useResource(async () => ({
        type, q, offset, payload: await api.get('/rbac/resources', { type, q, limit: 20, offset })
    }), [type, q, offset]);
    const match = result.data?.type === type && result.data.q === q && result.data.offset === offset;
    const data = match ? result.data.payload : null;
    const resources = Array.isArray(data?.resources) ? data.resources : [];
    const optionMap = new Map([...picked, ...resources.map(item => [item.id, item.label || item.id])]);

    const submit = async values => {
        if (saving) return;
        setError(null);
        const manual = String(values.ids || '').split(/[\s,，;；]+/).filter(Boolean);
        const ids = [...new Set([...picked.map(([id]) => id), ...manual])];
        if (!ids.length || ids.length > 100) {
            setError(new Error('请选择或输入 1 至 100 个资源 ID'));
            return;
        }
        setSaving(true);
        try {
            await api.post('/rbac/resource-assignments', {
                user_id: values.user_id, resource_type: type, resource_ids: ids
            });
            toastSuccess('资源授权已保存');
            onSaved();
        } catch (err) {
            setError(err);
        } finally {
            setSaving(false);
        }
    };

    return (
        <Drawer
            visible
            title="新增资源授权"
            width={560}
            closable={!saving}
            maskClosable={!saving}
            closeOnEsc={!saving}
            onCancel={onCancel}
            formId={formId}
            okText="保存授权"
            confirmLoading={saving}
            cancelButtonProps={{ disabled: saving }}
        >
            <Form id={formId} onSubmit={submit} initValues={{ resource_type: type }}>
                <Form.Select field="user_id" label="用户" filter
                    optionList={users.map(user => ({ value: user.id, label: user.display_name || user.username }))}
                    rules={[{ required: true, message: '请选择用户' }]} />
                <Form.Select field="resource_type" label="资源类型"
                    optionList={types.map(([value, label]) => ({ value, label }))}
                    onChange={value => { setType(value); setPicked([]); setQuery(''); setOffset(0); }} />
                <Form.Slot label="按名称或 ID 搜索">
                    <Input aria-label="搜索资源" value={query} onChange={value => { setQuery(value); setOffset(0); }} />
                </Form.Slot>
                <Form.Slot label="选择资源">
                    <Space wrap>
                        <Button size="small" disabled={offset === 0 || result.loading} onClick={() => setOffset(value => Math.max(0, value - 20))}>上一页</Button>
                        <Typography.Text type="tertiary">{offset + resources.length} / {data?.total ?? '—'}</Typography.Text>
                        <Button size="small" disabled={!data?.has_more || result.loading} onClick={() => setOffset(value => value + 20)}>下一页</Button>
                    </Space>
                    <Select multiple style={{ width: '100%', marginTop: 8 }} loading={result.loading}
                        value={picked.map(([id]) => id)}
                        aria-label="选择资源"
                        optionList={[...optionMap].map(([value, label]) => ({ value, label }))}
                        onChange={values => setPicked((values || []).map(id => [id, optionMap.get(id) || id]))} />
                </Form.Slot>
                <Form.TextArea field="ids" label="手动输入资源 ID" placeholder="多个 ID 用空格或换行分隔" rows={2} />
                <ErrorBanner error={error || result.error} />
            </Form>
        </Drawer>
    );
}
