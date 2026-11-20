import { useMemo, useState } from 'react';
import {
    Button, Checkbox, Form, Input, Space, Tabs, Tag, Typography
} from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import {
    Drawer, DataTable, ErrorBanner, PageHeader, SectionCard, confirmAction, toastError, toastSuccess
} from '../../components/ui';

import ResourceGrant from './ResourceGrant';
import { formatDateTime } from '../../lib/format';

const { Text } = Typography;

function PermissionPicker({ value = [], options, onChange }) {
    const groups = useMemo(() => {
        const map = new Map();
        options.forEach(key => {
            const [resource] = key.split(':');
            if (!map.has(resource)) map.set(resource, []);
            map.get(resource).push(key);
        });
        return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0]));
    }, [options]);
    return (
        <div style={{ maxHeight: 320, overflow: 'auto', border: '1px solid var(--semi-color-border)', borderRadius: 8, padding: 12 }}>
            {groups.map(([resource, keys]) => (
                <div key={resource} style={{ marginBottom: 10 }}>
                    <Text type="tertiary" style={{ fontSize: 12, textTransform: 'uppercase' }}>{resource}</Text>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 4 }}>
                        {keys.map(key => (
                            <Checkbox
                                key={key}
                                checked={value.includes(key)}
                                onChange={event => {
                                    const next = new Set(value);
                                    if (event.target.checked) next.add(key);
                                    else next.delete(key);
                                    onChange([...next]);
                                }}
                            >
                                {key.split(':')[1]}
                            </Checkbox>
                        ))}
                    </div>
                </div>
            ))}
        </div>
    );
}

export default function PlatformRbacPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('rbac:write');
    const [tab, setTab] = useState('roles');
    const [roleEditor, setRoleEditor] = useState(null);
    const [userEditor, setUserEditor] = useState(null);
    const [grantOpen, setGrantOpen] = useState(false);
    const [saving, setSaving] = useState(false);

    const metadata = useResource(() => api.get('/rbac/metadata'), []);
    const roles = useResource(() => api.get('/rbac/roles'), []);
    const users = useResource(() => api.get('/rbac/users'), []);
    const assignments = useResource(() => api.get('/rbac/resource-assignments'), []);

    const permissionKeys = metadata.data?.permissions ? Object.keys(metadata.data.permissions) : [];
    const roleRows = Array.isArray(roles.data?.roles) ? roles.data.roles : [];
    const userRows = Array.isArray(users.data?.users) ? users.data.users : [];
    const assignmentRows = Array.isArray(assignments.data?.assignments) ? assignments.data.assignments : [];
    const roleOptions = roleRows.map(role => ({ value: role.id, label: role.name }));

    const roleNames = new Map(roleRows.map(role => [role.id, role.name]));
    const userNames = new Map(userRows.map(user => [user.id, user.display_name || user.username]));

    const saveRole = async () => {
        if (saving || !roleEditor?.form) return;
        const values = await roleEditor.form.validate().catch(() => null);
        if (!values) return;
        setSaving(true);
        try {
            const payload = {
                name: values.name.trim(),
                description: values.description || '',
                scope: values.scope || '',
                permissions: roleEditor.permissions || []
            };
            if (roleEditor.role?.id) await api.put(`/rbac/roles/${encodeURIComponent(roleEditor.role.id)}`, payload);
            else await api.post('/rbac/roles', payload);
            toastSuccess('角色已保存');
            setRoleEditor(null);
            roles.reload();
            metadata.reload();
        } catch (error) {
            toastError(error, '保存角色失败');
        } finally {
            setSaving(false);
        }
    };

    const saveUser = async () => {
        if (saving || !userEditor?.form) return;
        const values = await userEditor.form.validate().catch(() => null);
        if (!values) return;
        setSaving(true);
        try {
            const payload = {
                display_name: values.display_name || '',
                enabled: values.enabled !== false,
                roles: values.roles || []
            };
            if (userEditor.user?.id) {
                if (values.password) payload.password = values.password;
                await api.put(`/rbac/users/${encodeURIComponent(userEditor.user.id)}`, payload);
            } else {
                await api.post('/rbac/users', { ...payload, username: values.username.trim(), password: values.password });
            }
            toastSuccess('用户已保存');
            setUserEditor(null);
            users.reload();
        } catch (error) {
            toastError(error, '保存用户失败');
        } finally {
            setSaving(false);
        }
    };

    const removeRole = role => confirmAction({
        title: `删除角色「${role.name}」？`,
        onOk: async () => {
            await api.del(`/rbac/roles/${encodeURIComponent(role.id)}`);
            toastSuccess('已删除');
            roles.reload();
        }
    });

    const removeUser = user => confirmAction({
        title: `删除用户「${user.username}」？`,
        onOk: async () => {
            await api.del(`/rbac/users/${encodeURIComponent(user.id)}`);
            toastSuccess('已删除');
            users.reload();
        }
    });

    const removeAssignment = row => confirmAction({
        title: '删除该资源授权？',
        content: `${row.resourceType || ''} ${row.resourceId || ''}`,
        onOk: async () => {
            await api.del(`/rbac/resource-assignments/${encodeURIComponent(row.id)}`);
            toastSuccess('已删除');
            assignments.reload();
        }
    });

    return (
        <div>
            <PageHeader
                title={t('auth.rbacTitle', null, '平台权限')}
                description="平台账号、角色与资源授权；角色决定接口权限键，资源授权决定可见数据范围。"
                extra={<Button icon={<IconRefresh />} onClick={() => { roles.reload(); users.reload(); assignments.reload(); metadata.reload(); }}>刷新</Button>}
            />
            <Tabs type="line" activeKey={tab} onChange={setTab}>
                <Tabs.TabPane itemKey="roles" tab={`角色权限（${roleRows.length}）`}>
                    <SectionCard
                        extra={canWrite && <Button theme="solid" type="primary" icon={<IconPlus />} onClick={() => setRoleEditor({ role: null, permissions: [], form: {} })}>新建角色</Button>}
                    >
                        <ErrorBanner error={roles.error} onRetry={() => roles.reload()} />
                        <DataTable
                            columns={[
                                { title: '角色', dataIndex: 'name', render: (value, record) => <Space vertical spacing={0} align="start"><Text strong>{value}</Text><Text type="tertiary">{record.description || '—'}</Text></Space> },
                                { title: '数据范围', dataIndex: 'scope', width: 130, render: value => (value ? <Tag size="small">{value}</Tag> : '—') },
                                { title: '权限数', dataIndex: 'permissions', width: 100, render: value => (Array.isArray(value) ? value.length : 0) },
                                { title: '系统内置', dataIndex: 'is_system', width: 100, render: value => (value ? <Tag size="small" color="blue">是</Tag> : '否') },
                                {
                                    title: '操作', width: 160, render: (_, record) => (
                                        <Space>
                                            {canWrite && !record.is_system && <Button size="small" theme="borderless" icon={<IconEdit />} onClick={() => setRoleEditor({ role: record, permissions: record.permissions || [], form: {} })}>编辑</Button>}
                                            {canWrite && !record.is_system && <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => removeRole(record)} />}
                                        </Space>
                                    )
                                }
                            ]}
                            dataSource={roleRows}
                            rowKey="id"
                            loading={roles.loading}
                            pagination={{ pageSize: 20 }}
                        />
                    </SectionCard>
                </Tabs.TabPane>
                <Tabs.TabPane itemKey="users" tab={`平台用户（${userRows.length}）`}>
                    <SectionCard extra={canWrite && <Button theme="solid" type="primary" icon={<IconPlus />} onClick={() => setUserEditor({ user: null, form: {} })}>新建用户</Button>}>
                        <ErrorBanner error={users.error} onRetry={() => users.reload()} />
                        <DataTable
                            columns={[
                                { title: '用户名', dataIndex: 'username', render: (value, record) => <Space vertical spacing={0} align="start"><Text strong>{value}</Text><Text type="tertiary">{record.display_name || '—'}</Text></Space> },
                                { title: '角色', dataIndex: 'roles', width: 260, render: value => (Array.isArray(value) && value.length ? <Space wrap spacing={4}>{value.map(role => <Tag size="small" key={role}>{roleNames.get(role) || role}</Tag>)}</Space> : '—') },
                                { title: '状态', dataIndex: 'enabled', width: 100, render: value => (value === false ? <Tag size="small" color="grey">停用</Tag> : <Tag size="small" color="green">启用</Tag>) },
                                {
                                    title: '操作', width: 160, render: (_, record) => (
                                        <Space>
                                            {canWrite && <Button size="small" theme="borderless" icon={<IconEdit />} onClick={() => setUserEditor({ user: record, form: {} })}>编辑</Button>}
                                            {canWrite && <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => removeUser(record)} />}
                                        </Space>
                                    )
                                }
                            ]}
                            dataSource={userRows}
                            rowKey="id"
                            loading={users.loading}
                            pagination={{ pageSize: 20 }}
                        />
                    </SectionCard>
                </Tabs.TabPane>
                <Tabs.TabPane itemKey="assignments" tab={`资源授权（${assignmentRows.length}）`}>
                    <SectionCard extra={canWrite && <Button theme="solid" icon={<IconPlus />} onClick={() => setGrantOpen(true)}>新增资源授权</Button>}>
                        <ErrorBanner error={assignments.error} onRetry={() => assignments.reload()} />
                        <DataTable
                            columns={[
                                { title: '资源类型', dataIndex: 'resourceType', width: 140, render: value => value || '—' },
                                { title: '资源', dataIndex: 'resourceId', render: (value, record) => record.resourceLabel || value || '—' },
                                { title: '用户', dataIndex: 'userId', render: value => userNames.get(value) || value || '—' },
                                { title: '创建时间', dataIndex: 'createdAt', width: 180, render: value => formatDateTime(value) },
                                {
                                    title: '操作', width: 100, render: (_, record) => (
                                        canWrite && <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => removeAssignment(record)} />
                                    )
                                }
                            ]}
                            dataSource={assignmentRows}
                            rowKey="id"
                            loading={assignments.loading}
                            pagination={{ pageSize: 20 }}
                        />
                    </SectionCard>
                </Tabs.TabPane>
            </Tabs>

            {canWrite && grantOpen && (
                <ResourceGrant users={userRows} onCancel={() => setGrantOpen(false)} onSaved={() => {
                    setGrantOpen(false);
                    assignments.reload();
                }} />
            )}

            <Drawer
                title={roleEditor?.role ? '编辑角色' : '新建角色'}
                visible={Boolean(roleEditor)}
                key={roleEditor?.role?.id || 'new-rbac-role'}
                onCancel={() => setRoleEditor(null)}
                onOk={saveRole}
                confirmLoading={saving}
                width={820}
            >
                {roleEditor && (
                    <Form
                        getFormApi={form => setRoleEditor(current => (current ? { ...current, form } : current))}
                        initValues={roleEditor.role ? {
                            name: roleEditor.role.name,
                            description: roleEditor.role.description,
                            scope: roleEditor.role.scope
                        } : {}}
                    >
                        <Form.Input field="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]} />
                        <Form.Input field="description" label="描述" />
                        <Form.Select
                            field="scope"
                            label="数据范围"
                            optionList={(metadata.data?.scopes || ['all', 'assigned', 'own']).map(item => ({ value: item, label: item }))}
                        />
                        <Form.Slot label="权限">
                            <PermissionPicker
                                value={roleEditor.permissions}
                                options={permissionKeys}
                                onChange={next => setRoleEditor(current => ({ ...current, permissions: next }))}
                            />
                        </Form.Slot>
                    </Form>
                )}
            </Drawer>

            <Drawer
                title={userEditor?.user ? '编辑用户' : '新建用户'}
                visible={Boolean(userEditor)}
                key={userEditor?.user?.id || 'new-rbac-user'}
                onCancel={() => setUserEditor(null)}
                onOk={saveUser}
                confirmLoading={saving}
                width={620}
            >
                {userEditor && (
                    <Form
                        getFormApi={form => setUserEditor(current => (current ? { ...current, form } : current))}
                        initValues={userEditor.user ? {
                            username: userEditor.user.username,
                            display_name: userEditor.user.display_name,
                            enabled: userEditor.user.enabled !== false,
                            roles: userEditor.user.roles || []
                        } : { enabled: true, roles: [] }}
                    >
                        <Form.Input field="username" label="用户名" disabled={Boolean(userEditor.user)} rules={[{ required: true, message: '请输入用户名' }]} />
                        <Form.Input field="display_name" label="显示名称" />
                        <Form.Input field="password" label={userEditor.user ? '重置密码（留空不修改）' : '密码'} mode="password" rules={userEditor.user ? [] : [{ required: true, min: 8, message: '至少 8 位' }]} />
                        <Form.Select field="roles" label="角色" multiple optionList={roleOptions} />
                        <Form.Slot label="启用"><Form.Switch field="enabled" /></Form.Slot>
                    </Form>
                )}
            </Drawer>
        </div>
    );
}
