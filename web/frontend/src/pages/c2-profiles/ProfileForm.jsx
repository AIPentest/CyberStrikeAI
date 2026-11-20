/**
 * C2 流量伪装 Profile 表单：UA、URI、请求/响应头与 Jitter 范围。
 */
import { useState } from 'react';
import { Button, Form, Space, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../lib/i18n';
import { Drawer, toastWarning } from '../../components/ui';

const { Text } = Typography;

function toValues(record) {
    return {
        name: record?.name || '',
        user_agent: record?.userAgent || '',
        uris: Array.isArray(record?.uris) ? record.uris.join('\n') : '',
        jitter_min: String(record?.jitterMinMs ?? 0),
        jitter_max: String(record?.jitterMaxMs ?? 0),
        request_headers: record?.requestHeaders ? JSON.stringify(record.requestHeaders, null, 2) : '',
        response_headers: record?.responseHeaders ? JSON.stringify(record.responseHeaders, null, 2) : '',
        body_template: record?.bodyTemplate || ''
    };
}

function parseHeaders(text, t) {
    const raw = String(text || '').trim();
    if (!raw) return {};
    try {
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
            throw new Error('not an object');
        }
        return parsed;
    } catch (error) {
        toastWarning(t('c2.profiles.toastInvalidHeadersJson', null, '响应头 JSON 无效'));
        return null;
    }
}

export default function ProfileForm({ visible, record, submitting, onCancel, onSubmit }) {
    const t = useT();
    const [formApi, setFormApi] = useState(null);

    const submit = values => {
        const min = Number(values.jitter_min);
        const max = Number(values.jitter_max);
        if (!Number.isInteger(min) || !Number.isInteger(max) || min < 0 || max < min) {
            toastWarning(t('c2.profiles.toastInvalidJitter', null, 'Jitter 必须为整数，且满足 0 <= 最小值 <= 最大值'));
            return;
        }
        const requestHeaders = parseHeaders(values.request_headers, t);
        if (requestHeaders === null) return;
        const responseHeaders = parseHeaders(values.response_headers, t);
        if (responseHeaders === null) return;
        onSubmit({
            name: String(values.name || '').trim(),
            userAgent: String(values.user_agent || '').trim(),
            uris: String(values.uris || '').split(/\r?\n/).map(item => item.trim()).filter(Boolean),
            jitterMinMs: min,
            jitterMaxMs: max,
            requestHeaders,
            responseHeaders,
            bodyTemplate: String(values.body_template || '')
        });
    };

    return (
        <Drawer
            visible={visible}
            title={record ? t('c2.profiles.editTitle', null, '编辑 Profile') : t('c2.profiles.modalCreateTitle', null, '创建 Malleable Profile')}
            footer={(
                <Space>
                    <Button onClick={onCancel}>{t('c2.common.cancel', null, '取消')}</Button>
                    <Button theme="solid" type="primary" loading={submitting} onClick={() => formApi?.submitForm()}>
                        {record ? t('c2.profiles.save', null, '保存') : t('c2.profiles.submitCreate', null, '创建')}
                    </Button>
                </Space>
            )}
            width={640}
            onCancel={onCancel}
        >
            <Form
                key={record?.id || 'new'}
                initValues={toValues(record)}
                onSubmit={submit}
                getFormApi={setFormApi}
                labelPosition="top"
            >
                <Form.Input
                    field="name"
                    label={t('c2.profiles.profileNameLabel', null, 'Profile 名称')}
                    placeholder={t('c2.profiles.placeholderProfileName', null, '例如 cdn-fronting')}
                    rules={[{ required: true, message: t('c2.profiles.toastNameRequired', null, '请填写 Profile 名称') }]}
                />
                <Form.Input field="user_agent" label={t('c2.profiles.userAgent', null, 'User-Agent')} placeholder="Mozilla/5.0 ..." />
                <Form.TextArea
                    field="uris"
                    label={t('c2.profiles.labelBeaconUris', null, 'Beacon URI（每行一个）')}
                    placeholder="/api/v1/ping"
                    autosize={{ minRows: 2, maxRows: 5 }}
                />
                <div className="c2-form-row">
                    <Form.Input field="jitter_min" label={t('c2.profiles.labelJitterMin', null, 'Jitter 最小值 (ms)')} placeholder="0" />
                    <Form.Input field="jitter_max" label={t('c2.profiles.labelJitterMax', null, 'Jitter 最大值 (ms)')} placeholder="0" />
                </div>
                <Form.TextArea
                    field="request_headers"
                    label={t('c2.profiles.requestHeadersJson', null, '自定义请求头 (JSON)')}
                    placeholder='{"X-Forwarded-For":"127.0.0.1"}'
                    autosize={{ minRows: 2, maxRows: 6 }}
                />
                <Form.TextArea
                    field="response_headers"
                    label={t('c2.profiles.labelRespHeaders', null, '自定义响应头 (JSON)')}
                    placeholder='{"Server":"nginx"}'
                    autosize={{ minRows: 2, maxRows: 6 }}
                />
                <Form.TextArea
                    field="body_template"
                    label={t('c2.profiles.bodyTemplate', null, '响应体模板')}
                    placeholder='{"status":"ok"}'
                    autosize={{ minRows: 2, maxRows: 6 }}
                />
                <Text type="tertiary" size="small">{t('c2.profiles.hintHeaders', null, '用于伪装为合法服务的 HTTP 响应头')}</Text>
            </Form>
        </Drawer>
    );
}
