/**
 * C2 任务下发：POST /c2/tasks，按任务类型组织 payload。
 */
import { useState } from 'react';
import { Button, Form, Space, Typography } from '@douyinfe/semi-ui';
import { Drawer } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { TASK_TYPES } from '../c2-sessions/components/c2-shared';

const { Text } = Typography;

const COMMAND_TYPES = new Set(['shell', 'exec']);
const PATH_TYPES = new Set(['ls', 'download', 'cd']);

export default function CreateTaskModal({ visible, defaultSessionId, onCancel, onSubmit, submitting }) {
    const t = useT();
    const [formApi, setFormApi] = useState(null);
    const [taskType, setTaskType] = useState('shell');

    const submit = values => {
        const payload = {};
        const command = String(values.command || '').trim();
        if (COMMAND_TYPES.has(values.task_type)) {
            payload.command = command;
            const timeout = Number(values.timeout_seconds);
            if (Number.isFinite(timeout) && timeout > 0) payload.timeout_seconds = timeout;
        } else if (PATH_TYPES.has(values.task_type)) {
            payload.path = command;
        } else if (command) {
            payload.path = command;
        }
        const extra = String(values.extra || '').trim();
        if (extra) {
            try {
                Object.assign(payload, JSON.parse(extra));
            } catch (error) {
                payload.raw = extra;
            }
        }
        onSubmit({
            session_id: String(values.session_id || '').trim(),
            task_type: values.task_type || 'shell',
            payload,
            source: 'manual'
        });
    };

    return (
        <Drawer
            visible={visible}
            title={t('c2.tasks.createTitle', null, '下发任务')}
            footer={(
                <Space>
                    <Button onClick={onCancel}>{t('c2.tasks.cancelFilters', null, '取消')}</Button>
                    <Button theme="solid" type="primary" loading={submitting} onClick={() => formApi?.submitForm()}>
                        {t('c2.tasks.createSubmit', null, '下发')}
                    </Button>
                </Space>
            )}
            width={560}
            onCancel={onCancel}
        >
            <Form
                key={visible ? 'open' : 'closed'}
                initValues={{ session_id: defaultSessionId || '', task_type: 'shell', command: '', timeout_seconds: '60', extra: '' }}
                onSubmit={submit}
                getFormApi={setFormApi}
                labelPosition="top"
                onValueChange={changed => { if (changed.task_type) setTaskType(changed.task_type); }}
            >
                <Form.Input
                    field="session_id"
                    label={t('c2.tasks.labelSession', null, '会话')}
                    placeholder="s_xxxxxxxx"
                    rules={[{ required: true, message: t('c2.tasks.labelSession', null, '请填写会话 ID') }]}
                />
                <Form.Select field="task_type" label={t('c2.tasks.labelType', null, '类型')} optionList={TASK_TYPES} style={{ width: '100%' }} />
                <Form.Input
                    field="command"
                    label={COMMAND_TYPES.has(taskType) ? t('c2.tasks.labelCommand', null, '命令') : t('c2.tasks.labelPath', null, '路径')}
                    placeholder={COMMAND_TYPES.has(taskType) ? 'whoami' : '/etc/passwd'}
                />
                {COMMAND_TYPES.has(taskType) && (
                    <Form.Input field="timeout_seconds" label={t('c2.tasks.labelTimeout', null, '超时（秒）')} placeholder="60" />
                )}
                <Form.Input field="extra" label={t('c2.tasks.labelExtraPayload', null, '附加参数（JSON，可选）')} placeholder='{"key":"value"}' />
                <Text type="tertiary" size="small">
                    {t('c2.tasks.createHint', null, '任务入队后由植入体在下次心跳拉取；危险类型可能需要人机协同审批。')}
                </Text>
            </Form>
        </Drawer>
    );
}
