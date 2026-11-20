/** 工作流包导入：选择/粘贴包 → 预检 → 冲突处理 → 导入结果。 */
import { useEffect, useMemo, useState } from 'react';
import {
    Banner, Button, Checkbox, Input, Radio, RadioGroup, Space, Tag, TextArea, Typography
} from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { Drawer, KeyValueList, toastSuccess } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import {
    PACKAGE_ACTIONS, allowedActions, createIdempotencyKey, errText, packageErrorText, shortHash
} from './helpers';

const { Text, Paragraph } = Typography;

const MAX_BYTES = 10 * 1024 * 1024;

function base64ToFile(text) {
    const clean = String(text || '').replace(/^data:[^;]+;base64,/, '').replace(/\s+/g, '');
    if (!clean) throw new Error('粘贴内容为空');
    const binary = atob(clean);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
    return new File([bytes], 'pasted-workflow.csapkg.zip', { type: 'application/zip' });
}

const RESULT_MESSAGE = {
    created: 'workflows.package.result.created',
    overwritten: 'workflows.package.result.overwritten',
    renamed: 'workflows.package.result.renamed',
    kept_existing: 'workflows.package.result.keptExisting',
    skipped_identical: 'workflows.package.result.skippedIdentical'
};

export default function PackageImportModal({ visible, onCancel, onImported }) {
    const t = useT();
    const [file, setFile] = useState(null);
    const [pasteText, setPasteText] = useState('');
    const [pasteError, setPasteError] = useState('');
    const [inspection, setInspection] = useState(null);
    const [action, setAction] = useState('');
    const [newId, setNewId] = useState('');
    const [overwriteConfirmed, setOverwriteConfirmed] = useState(false);
    const [inspecting, setInspecting] = useState(false);
    const [importing, setImporting] = useState(false);
    const [error, setError] = useState('');
    const [result, setResult] = useState(null);

    useEffect(() => {
        if (visible) return;
        setFile(null);
        setPasteText('');
        setPasteError('');
        setInspection(null);
        setAction('');
        setNewId('');
        setOverwriteConfirmed(false);
        setError('');
        setResult(null);
        setInspecting(false);
        setImporting(false);
    }, [visible]);

    const conflictState = inspection?.conflict?.state || '';
    const actions = useMemo(() => allowedActions(conflictState), [conflictState]);

    const usePaste = () => {
        setPasteError('');
        try {
            const next = base64ToFile(pasteText);
            if (next.size > MAX_BYTES) {
                setPasteError(t('workflows.package.errors.fileTooLarge', null, '文件超过 10 MiB 限制，请选择更小的本地包。'));
                return;
            }
            setFile(next);
            setInspection(null);
            setResult(null);
        } catch (decodeError) {
            setPasteError(`Base64 解码失败：${errText(decodeError, '')}`);
        }
    };

    const pickFile = (event) => {
        const picked = event.target.files && event.target.files[0];
        setPasteError('');
        setError('');
        setResult(null);
        setInspection(null);
        if (!picked) {
            setFile(null);
            return;
        }
        if (picked.size > MAX_BYTES) {
            setPasteError(t('workflows.package.errors.fileTooLarge', null, '文件超过 10 MiB 限制，请选择更小的本地包。'));
            setFile(null);
            return;
        }
        setFile(picked);
    };

    const inspect = async () => {
        if (!file) {
            setPasteError(t('workflows.package.errors.fileRequired', null, '请选择本地工作流包后再预检。'));
            return;
        }
        setInspecting(true);
        setError('');
        setResult(null);
        try {
            const form = new FormData();
            form.append('file', file, file.name || 'workflow.csapkg.zip');
            const payload = await api.post('/workflow-package-inspections', form);
            const summary = payload?.inspection || null;
            setInspection(summary);
            const nextActions = allowedActions(summary?.conflict?.state || '');
            setAction(nextActions[0] || '');
            setNewId('');
            setOverwriteConfirmed(false);
        } catch (inspectError) {
            setError(packageErrorText(inspectError, t));
        } finally {
            setInspecting(false);
        }
    };

    const applyImport = async () => {
        if (!inspection?.id) {
            setError(t('workflows.package.resolution.needInspection', null, '请先选择本地包并完成预检。'));
            return;
        }
        if (!action) {
            setError(t('workflows.package.errors.invalidAction', null, '导入处理方式无效，请重新选择。'));
            return;
        }
        if (action === 'rename' && !newId.trim()) {
            setError(t('workflows.package.errors.invalidRenameId', null, '请输入可用的新工作流 ID。'));
            return;
        }
        if (action === 'overwrite' && !overwriteConfirmed) {
            setError(t('workflows.package.errors.overwriteConfirmationRequired', null, '请勾选确认后再覆盖本地工作流。'));
            return;
        }
        const key = createIdempotencyKey();
        if (!key) {
            setError(t('workflows.package.errors.idempotencyKeyRequired', null, '浏览器无法生成导入请求标识，请刷新页面后重试。'));
            return;
        }
        setImporting(true);
        setError('');
        try {
            const payload = await api.post('/workflow-package-imports', {
                inspection_id: inspection.id,
                resolution: {
                    action,
                    new_workflow_id: action === 'rename' ? newId.trim() : ''
                },
                confirm_overwrite: action === 'overwrite'
            }, { headers: { 'Idempotency-Key': key } });
            setResult(payload?.import || null);
            toastSuccess(t('workflows.package.result.complete', null, '导入已完成。'));
            onImported?.(payload?.import || null);
        } catch (importError) {
            setError(packageErrorText(importError, t));
        } finally {
            setImporting(false);
        }
    };

    const conflictHint = conflictState === 'identical'
        ? t('workflows.package.conflict.identical', null, '检测到同 ID 且内容相同的本地工作流；本次将跳过导入。')
        : conflictState === 'id_conflict'
            ? t('workflows.package.conflict.idConflict', null, '检测到同 ID 但内容不同的本地工作流。默认保留本地版本。')
            : conflictState === 'none'
                ? t('workflows.package.conflict.none', null, '未发现同 ID 的本地工作流，可创建导入。')
                : '';

    const workflow = inspection?.workflow;

    return (
        <Drawer
            visible={visible}
            title={t('workflows.package.title', null, '导入本地包')}
            onCancel={onCancel}
            width={760}
            maskClosable={!inspecting && !importing}
            footer={(
                <Space>
                    <Button onClick={onCancel}>{t('common.close', null, '关闭')}</Button>
                    <Button
                        type="primary"
                        loading={inspecting}
                        disabled={Boolean(result)}
                        onClick={inspect}
                    >
                        {inspecting
                            ? t('workflows.package.inspecting', null, '正在安全预检…')
                            : t('workflows.package.inspect', null, '上传并预检')}
                    </Button>
                    <Button
                        type="primary"
                        loading={importing}
                        disabled={!inspection || Boolean(result) || !action}
                        onClick={applyImport}
                    >
                        {t('workflows.package.confirmImport', null, '确认导入')}
                    </Button>
                </Space>
            )}
        >
            <Paragraph type="tertiary" size="small">
                {t('workflows.package.preflightHint', null, '先检查包内容与冲突，再由你确认是否导入')}
                {' · '}
                {t('workflows.package.fileHint', null, '仅支持单个 .csapkg.zip，文件不得超过 10 MiB。')}
            </Paragraph>

            <div className="workflow-package-block">
                <Text strong size="small">{t('workflows.package.stepInspection', null, '上传与预检')}</Text>
                <div className="workflow-package-file-row">
                    <input
                        type="file"
                        accept=".zip,application/zip"
                        className="workflow-file-input"
                        onChange={pickFile}
                        aria-label={t('workflows.package.selectFile', null, '选择本地包')}
                    />
                    <Text type="tertiary" size="small">
                        {file ? `${file.name} · ${(file.size / 1024).toFixed(1)} KiB` : t('workflows.package.noFile', null, '未选择文件')}
                    </Text>
                    {file ? (
                        <Button size="small" theme="borderless" onClick={() => { setFile(null); setInspection(null); setResult(null); }}>
                            {t('workflows.package.removeFile', null, '移除文件')}
                        </Button>
                    ) : null}
                </div>
                <TextArea
                    value={pasteText}
                    rows={3}
                    placeholder="或粘贴工作流包 base64（.csapkg.zip）"
                    onChange={value => setPasteText(value)}
                />
                <Space>
                    <Button size="small" onClick={usePaste} disabled={!pasteText.trim()}>
                        使用粘贴内容
                    </Button>
                    {pasteError ? <Text type="danger" size="small">{pasteError}</Text> : null}
                </Space>
            </div>

            {error ? (
                <Banner type="danger" bordered closeIcon={null} description={error} className="workflow-package-banner" />
            ) : null}

            {inspection ? (
                <div className="workflow-package-block">
                    <div className="workflow-package-summary-head">
                        <Text strong size="small">{t('workflows.package.summary.title', null, '预检结果')}</Text>
                        <Tag size="small" color="green">{t('workflows.package.summary.passed', null, '校验通过')}</Tag>
                    </div>
                    <Banner
                        type={conflictState === 'id_conflict' ? 'warning' : 'success'}
                        bordered
                        closeIcon={null}
                        description={conflictHint}
                        className="workflow-package-banner"
                    />
                    <KeyValueList
                        columns={2}
                        items={[
                            { label: t('workflows.package.summary.workflowName', null, '工作流名称'), value: workflow?.name || '—' },
                            { label: t('workflows.package.summary.sourceId', null, '源工作流 ID'), value: <code>{workflow?.source_id || '—'}</code> },
                            { label: t('workflows.package.summary.sourceRevision', null, '源版本'), value: workflow?.source_revision ?? '—' },
                            {
                                label: t('workflows.package.summary.graphSize', null, '图规模'),
                                value: `${workflow?.node_count || 0} 节点 · ${workflow?.edge_count || 0} 连线`
                            },
                            { label: t('workflows.package.summary.contentHash', null, '内容摘要'), value: shortHash(workflow?.content_hash) },
                            { label: t('workflows.package.summary.expiresAt', null, '预检有效期'), value: formatDateTime(inspection.expires_at) }
                        ]}
                    />

                    <Text strong size="small">{t('workflows.package.resolutionTitle', null, '处理方式')}</Text>
                    <RadioGroup
                        value={action}
                        onChange={event => { setAction(event.target.value); setOverwriteConfirmed(false); }}
                        direction="vertical"
                    >
                        {actions.map(item => (
                            <Radio key={item} value={item}>
                                <span className="workflow-package-action">
                                    <Text strong size="small">{PACKAGE_ACTIONS[item]?.label || item}</Text>
                                    <Text type="tertiary" size="small">{PACKAGE_ACTIONS[item]?.hint || ''}</Text>
                                </span>
                            </Radio>
                        ))}
                    </RadioGroup>

                    {action === 'rename' ? (
                        <Input
                            value={newId}
                            placeholder={t('workflows.package.resolution.newWorkflowIdPlaceholder', null, '例如：web-scan-basic-copy')}
                            onChange={value => setNewId(value)}
                        />
                    ) : null}

                    {action === 'overwrite' ? (
                        <div className="workflow-package-overwrite">
                            <Banner
                                type="warning"
                                bordered
                                closeIcon={null}
                                description={t('workflows.package.overwriteWarning', null, '覆盖将替换名称、描述、图定义和启用状态。本期不提供回滚。')}
                            />
                            <Checkbox
                                checked={overwriteConfirmed}
                                onChange={event => setOverwriteConfirmed(event.target.checked)}
                            >
                                {t('workflows.package.overwriteCheck', null, '我已确认覆盖当前本地工作流')}
                            </Checkbox>
                        </div>
                    ) : null}
                </div>
            ) : null}

            {result ? (
                <Banner
                    type="success"
                    bordered
                    closeIcon={null}
                    className="workflow-package-banner"
                    description={t(RESULT_MESSAGE[result.result] || 'workflows.package.result.complete', null, '导入已完成。')}
                />
            ) : null}
        </Drawer>
    );
}
