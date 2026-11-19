/**
 * WebShell 连接表单：新建 / 编辑，支持一次探活测试。
 */
import { useState } from 'react';
import { Form, Button, Space, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../lib/i18n';
import { Drawer, toastError, toastSuccess, toastWarning } from '../../components/ui';
import { probeConnection, MASKED_SECRET } from './api';

const { Text } = Typography;

const TYPE_OPTIONS = [
    { value: 'php', label: 'PHP' },
    { value: 'asp', label: 'ASP' },
    { value: 'aspx', label: 'ASPX' },
    { value: 'jsp', label: 'JSP' },
    { value: 'custom', label: '自定义' }
];

const METHOD_OPTIONS = [
    { value: 'post', label: 'POST' },
    { value: 'get', label: 'GET' }
];

const ENCODING_OPTIONS = [
    { value: 'auto', label: '自动检测' },
    { value: 'utf-8', label: 'UTF-8' },
    { value: 'gbk', label: 'GBK（中文 Windows）' },
    { value: 'gb18030', label: 'GB18030' }
];

const OS_OPTIONS = [
    { value: 'auto', label: '自动（按 Shell 类型推断）' },
    { value: 'linux', label: 'Linux / Unix' },
    { value: 'windows', label: 'Windows' }
];

function toFormValues(record) {
    if (!record) {
        return {
            url: '', password: '', type: 'php', method: 'post', cmd_param: '',
            encoding: 'auto', os: 'auto', remark: '', project_id: ''
        };
    }
    return {
        url: record.url || '',
        password: record.password || '',
        type: record.type || 'php',
        method: (record.method || 'post').toLowerCase(),
        cmd_param: record.cmdParam || '',
        encoding: record.encoding || 'auto',
        os: record.os || 'auto',
        remark: record.remark || '',
        project_id: record.project_id || ''
    };
}

export default function ConnectionForm({ visible, record, projects, submitting, onCancel, onSubmit }) {
    const t = useT();
    const [formApi, setFormApi] = useState(null);
    const [testing, setTesting] = useState(false);

    const handleTest = async () => {
        const values = formApi?.getValues?.() || {};
        const url = String(values.url || '').trim();
        if (!url) {
            toastWarning(t('webshell.url', null, '请填写 Shell 地址'));
            return;
        }
        if (values.password === MASKED_SECRET) {
            toastWarning(t('webshell.testNoExpectedOutput', null, '已保存口令不可用于新建测试，请填写口令'));
            return;
        }
        setTesting(true);
        try {
            const result = await probeConnection({
                id: record?.id || '',
                url,
                password: values.password || '',
                type: values.type || 'php',
                method: values.method || 'post',
                cmdParam: values.cmd_param || '',
                encoding: values.encoding || 'auto',
                os: values.os || 'auto'
            });
            if (result.ok) toastSuccess(t('webshell.testSuccess', null, '连通性正常，Shell 可访问'));
            else toastError(result.error || t('webshell.testFailed', null, '连通性测试失败'));
        } catch (error) {
            toastError(error, t('webshell.testFailed', null, '连通性测试失败'));
        } finally {
            setTesting(false);
        }
    };

    const submit = values => {
        onSubmit({
            url: String(values.url || '').trim(),
            password: values.password || '',
            type: values.type || 'php',
            method: values.method || 'post',
            cmd_param: String(values.cmd_param || '').trim(),
            encoding: values.encoding || 'auto',
            os: values.os || 'auto',
            remark: String(values.remark || '').trim(),
            project_id: values.project_id || ''
        });
    };

    return (
        <Drawer
            visible={visible}
            title={record ? t('webshell.editConnectionTitle', null, '编辑连接') : t('webshell.addConnection', null, '添加连接')}
            footer={(
                <Space>
                    <Button onClick={handleTest} loading={testing}>{t('webshell.testConnectivity', null, '测试连通性')}</Button>
                    <Button onClick={onCancel}>{t('webshell.cancelEdit', null, '取消')}</Button>
                    <Button theme="solid" type="primary" loading={submitting} onClick={() => formApi?.submitForm()}>
                        {record ? t('webshell.saveFile', null, '保存') : t('webshell.addConnection', null, '添加连接')}
                    </Button>
                </Space>
            )}
            width={600}
            onCancel={onCancel}
        >
            <Form
                key={record?.id || 'new'}
                initValues={toFormValues(record)}
                onSubmit={submit}
                getFormApi={setFormApi}
                labelPosition="top"
            >
                <Form.Input
                    field="url"
                    label={t('webshell.url', null, 'Shell 地址')}
                    placeholder="http(s)://target.com/shell.php"
                    rules={[{ required: true, message: t('webshell.url', null, '请填写 Shell 地址') }]}
                />
                <Form.Input
                    field="password"
                    label={t('webshell.password', null, '连接密码/密钥')}
                    placeholder={t('webshell.passwordPlaceholder', null, '如冰蝎/蚁剑的连接密码')}
                />
                <div className="webshell-form-row">
                    <Form.Select field="type" label={t('webshell.type', null, 'Shell 类型')} optionList={TYPE_OPTIONS} style={{ width: '100%' }} />
                    <Form.Select field="method" label={t('webshell.method', null, '请求方式')} optionList={METHOD_OPTIONS} style={{ width: '100%' }} />
                </div>
                <div className="webshell-form-row">
                    <Form.Input field="cmd_param" label={t('webshell.cmdParam', null, '命令参数名')} placeholder="cmd" />
                    <Form.Select field="encoding" label={t('webshell.encoding', null, '响应编码')} optionList={ENCODING_OPTIONS} style={{ width: '100%' }} />
                </div>
                <div className="webshell-form-row">
                    <Form.Select field="os" label={t('webshell.os', null, '目标系统')} optionList={OS_OPTIONS} style={{ width: '100%' }} />
                    <Form.Select
                        field="project_id"
                        label={t('webshell.project', null, '所属项目')}
                        optionList={projects.map(item => ({ value: item.id, label: item.name || item.id }))}
                        showClear
                        style={{ width: '100%' }}
                        placeholder="可选"
                    />
                </div>
                <Form.Input field="remark" label={t('webshell.remark', null, '备注')} placeholder={t('webshell.remarkPlaceholder', null, '便于识别的备注名')} />
                <Text type="tertiary" size="small">{t('webshell.encodingHint', null, '中文 Windows 目标若出现乱码，请切换为 GBK 或 GB18030')}</Text>
            </Form>
        </Drawer>
    );
}
