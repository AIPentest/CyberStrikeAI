/**
 * C2 载荷生成：单行反弹命令（oneliner）与跨平台 Beacon 编译下载。
 */
import { useMemo, useState } from 'react';
import { Button, Input, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import {
    PageHeader, SectionCard, DataTable, ErrorBanner, CopyButton,
    toastError, toastSuccess, toastWarning
} from '../../components/ui';
import { formatBytes } from '../../lib/format';
import * as c2 from '../c2-sessions/components/c2-api';
import { LISTENER_TYPES, ONELINER_KINDS } from '../c2-sessions/components/c2-shared';
import './page.css';

const { Text, Title } = Typography;

const OS_OPTIONS = [
    { value: 'linux', label: 'Linux' },
    { value: 'windows', label: 'Windows' },
    { value: 'darwin', label: 'macOS' }
];

const ARCH_OPTIONS = [
    { value: 'amd64', label: 'AMD64' },
    { value: 'arm64', label: 'ARM64' },
    { value: '386', label: '386' }
];

const typeLabel = value => LISTENER_TYPES.find(item => item.value === value)?.label || value || '—';

export default function C2PayloadsPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('c2:write');

    const { data, loading, error, reload } = useResource(() => c2.listListeners(), []);
    const listeners = Array.isArray(data?.listeners) ? data.listeners : [];

    const [onelinerForm, setOnelinerForm] = useState({ listenerId: '', kind: '', host: '' });
    const [oneliner, setOneliner] = useState(null);
    const [onelinerBusy, setOnelinerBusy] = useState(false);

    const [buildForm, setBuildForm] = useState({ listenerId: '', host: '', os: 'linux', arch: 'amd64' });
    const [buildResult, setBuildResult] = useState(null);
    const [buildBusy, setBuildBusy] = useState(false);
    const [artifacts, setArtifacts] = useState([]);

    const activeListener = useMemo(
        () => listeners.find(item => item.id === (onelinerForm.listenerId || listeners[0]?.id)) || null,
        [listeners, onelinerForm.listenerId]
    );
    const kindOptions = ONELINER_KINDS[activeListener?.type] || [];

    const listenerOptions = listeners.map(item => ({
        value: item.id,
        label: `${item.name || item.id} · ${typeLabel(item.type)}`
    }));

    const generateOneliner = async () => {
        const listenerId = onelinerForm.listenerId || listeners[0]?.id;
        if (!listenerId) {
            toastWarning(t('c2.payloads.toastPickListener', null, '请先选择一个监听器'));
            return;
        }
        const kind = onelinerForm.kind || kindOptions[0]?.value;
        if (!kind) {
            toastWarning(t('c2.payloads.noKindOption', null, '无可用类型'));
            return;
        }
        setOnelinerBusy(true);
        try {
            const payload = await c2.buildOneliner({ listener_id: listenerId, kind, host: onelinerForm.host.trim() });
            setOneliner(payload);
        } catch (err) {
            toastError(err, t('c2.payloads.toastOnelinerFail', null, '生成 Oneliner 失败'));
        } finally {
            setOnelinerBusy(false);
        }
    };

    const buildBeacon = async () => {
        const listenerId = buildForm.listenerId || listeners[0]?.id;
        if (!listenerId) {
            toastWarning(t('c2.payloads.toastPickListener', null, '请先选择一个监听器'));
            return;
        }
        setBuildBusy(true);
        try {
            const payload = await c2.buildPayload({
                listener_id: listenerId,
                host: buildForm.host.trim(),
                os: buildForm.os,
                arch: buildForm.arch,
                sleep_seconds: 5,
                jitter_percent: 0
            });
            const result = payload?.payload || null;
            setBuildResult(result);
            if (result) {
                const fileName = String(result.output_path || '').split(/[\\/]/).pop();
                setArtifacts(current => [
                    {
                        id: result.payload_id || fileName,
                        fileName,
                        listenerId,
                        os: result.os,
                        arch: result.arch,
                        size: result.size_bytes
                    },
                    ...current
                ]);
                toastSuccess(t('c2.payloads.toastBuildSuccess', null, '构建成功').replace('{{bytes}}', String(result.size_bytes || 0)));
            }
        } catch (err) {
            toastError(err, t('c2.payloads.toastBuildFail', null, '构建 Beacon 失败'));
        } finally {
            setBuildBusy(false);
        }
    };

    const download = async artifact => {
        if (!artifact.fileName?.startsWith('beacon_')) {
            toastWarning(t('c2.payloads.invalidArtifact', null, '该产物不可下载'));
            return;
        }
        try {
            await c2.downloadPayload(artifact.fileName, artifact.fileName);
            toastSuccess(t('c2.payloads.toastDownloadQueued', null, '下载任务已排队'));
        } catch (err) {
            toastError(err, t('c2.payloads.download', null, '下载失败'));
        }
    };

    const artifactColumns = [
        { title: t('c2.payloads.fileName', null, '文件'), dataIndex: 'fileName', render: value => <span className="c2-mono">{value || '—'}</span> },
        { title: t('c2.payloads.os', null, '系统'), dataIndex: 'os', width: 100 },
        { title: t('c2.payloads.arch', null, '架构'), dataIndex: 'arch', width: 100 },
        { title: t('c2.payloads.size', null, '大小'), dataIndex: 'size', width: 110, render: value => formatBytes(value) },
        {
            title: t('c2.tasks.colActions', null, '操作'),
            dataIndex: 'actions',
            width: 120,
            render: (_, artifact) => (
                <Button size="small" theme="borderless" onClick={() => download(artifact)}>{t('c2.payloads.download', null, '下载')}</Button>
            )
        }
    ];

    return (
        <div className="c2-page" id="page-c2-payloads">
            <PageHeader
                title={t('c2.payloads.title', null, 'Payload 生成')}
                description={t('c2.payloads.pageDesc', null, '生成单行反弹命令，或交叉编译完整 Beacon 二进制')}
                extra={[
                    <Button key="refresh" onClick={reload} loading={loading}>{t('common.refresh', null, '刷新')}</Button>
                ]}
            />

            <ErrorBanner error={error} onRetry={reload} />

            <div className="c2-payload-grid">
                <SectionCard title={t('c2.payloads.oneliner', null, '单行 Payload')} description={t('c2.payloads.onelinerDesc', null, '生成可在目标机直接执行的反弹命令')}>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.payloads.listener', null, '监听器')}</span>
                        <Select
                            value={onelinerForm.listenerId || listeners[0]?.id || ''}
                            onChange={value => setOnelinerForm(form => ({ ...form, listenerId: value, kind: '' }))}
                            optionList={listenerOptions}
                            placeholder={t('c2.payloads.noListenersOption', null, '暂无可用监听器')}
                            style={{ width: '100%' }}
                        />
                    </div>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.payloads.kind', null, '类型')}</span>
                        <Select
                            value={onelinerForm.kind || kindOptions[0]?.value || ''}
                            onChange={value => setOnelinerForm(form => ({ ...form, kind: value }))}
                            optionList={kindOptions}
                            placeholder={t('c2.payloads.noKindOption', null, '无可用类型')}
                            style={{ width: '100%' }}
                        />
                    </div>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.payloads.hostOptional', null, '回连地址（可选）')}</span>
                        <Input
                            value={onelinerForm.host}
                            onChange={value => setOnelinerForm(form => ({ ...form, host: value }))}
                            placeholder={t('c2.payloads.placeholderListenerHost', null, '留空则使用监听器回连地址')}
                        />
                    </div>
                    <Button theme="solid" type="primary" block disabled={!canWrite} loading={onelinerBusy} onClick={generateOneliner}>
                        {t('c2.payloads.generateOnelinerBtn', null, '生成 Oneliner')}
                    </Button>
                    {oneliner?.oneliner && (
                        <div className="c2-oneliner-result">
                            <div className="c2-oneliner-head">
                                <Space spacing={6}>
                                    <Tag size="small">{oneliner.kind}</Tag>
                                    <Text type="tertiary" size="small">{oneliner.host}:{oneliner.port}</Text>
                                </Space>
                                <CopyButton text={oneliner.oneliner} label={t('c2.payloads.copy', null, '复制')} size="small" />
                            </div>
                            <pre className="c2-pre">{oneliner.oneliner}</pre>
                        </div>
                    )}
                </SectionCard>

                <SectionCard title={t('c2.payloads.build', null, '编译 Beacon')} description={t('c2.payloads.buildDesc', null, '交叉编译多平台完整 Beacon 可执行文件')}>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.payloads.listener', null, '监听器')}</span>
                        <Select
                            value={buildForm.listenerId || listeners[0]?.id || ''}
                            onChange={value => setBuildForm(form => ({ ...form, listenerId: value }))}
                            optionList={listenerOptions}
                            placeholder={t('c2.payloads.noListenersOption', null, '暂无可用监听器')}
                            style={{ width: '100%' }}
                        />
                    </div>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.payloads.hostOptional', null, '回连地址（可选）')}</span>
                        <Input
                            value={buildForm.host}
                            onChange={value => setBuildForm(form => ({ ...form, host: value }))}
                            placeholder={t('c2.payloads.placeholderListenerHost', null, '留空则使用监听器回连地址')}
                        />
                    </div>
                    <div className="c2-form-row">
                        <div className="c2-field">
                            <span className="c2-field-label">{t('c2.payloads.os', null, '目标系统')}</span>
                            <Select value={buildForm.os} onChange={value => setBuildForm(form => ({ ...form, os: value }))} optionList={OS_OPTIONS} style={{ width: '100%' }} />
                        </div>
                        <div className="c2-field">
                            <span className="c2-field-label">{t('c2.payloads.arch', null, '目标架构')}</span>
                            <Select value={buildForm.arch} onChange={value => setBuildForm(form => ({ ...form, arch: value }))} optionList={ARCH_OPTIONS} style={{ width: '100%' }} />
                        </div>
                    </div>
                    <Button theme="solid" type="primary" block disabled={!canWrite} loading={buildBusy} onClick={buildBeacon}>
                        {t('c2.payloads.buildBeaconBtn', null, '构建 Beacon')}
                    </Button>
                    {buildResult && (
                        <div className="c2-build-result">
                            <Title heading={6}>{t('c2.payloads.buildSuccessTitle', null, '构建成功')}</Title>
                            <Text type="tertiary" size="small">
                                {t('c2.payloads.buildMetaOsArch', null, '系统 / 架构').replace('{{os}}', buildResult.os || '—').replace('{{arch}}', buildResult.arch || '—')}
                            </Text>
                            <Text type="tertiary" size="small">{t('c2.payloads.buildSize', null, '大小').replace('{{bytes}}', String(buildResult.size_bytes || 0))}</Text>
                            <pre className="c2-pre">{buildResult.output_path}</pre>
                            <Button size="small" onClick={() => download({ fileName: String(buildResult.output_path || '').split(/[\\/]/).pop(), ...buildResult })}>
                                {t('c2.payloads.download', null, '下载')}
                            </Button>
                        </div>
                    )}
                </SectionCard>
            </div>

            <SectionCard title={t('c2.payloads.artifacts', null, '本次会话构建产物')}>
                <DataTable
                    columns={artifactColumns}
                    dataSource={artifacts}
                    rowKey="id"
                    pagination={{ pageSize: 10 }}
                    emptyText={t('c2.payloads.noArtifacts', null, '暂无构建产物')}
                />
            </SectionCard>
        </div>
    );
}
