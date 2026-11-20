/** 漏洞详情抽屉：概览 / 复现与证据 / 关联 / 下载报告（GET /:id/context 提供上下文）。 */
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Collapse, Radio, RadioGroup, SideSheet, Space, Spin, Tabs, Timeline, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconRefresh } from '@douyinfe/semi-icons';
import { api, downloadFile } from '../../lib/http';
import { formatDateTime } from '../../lib/format';
import { useResource } from '../../components/hooks';
import {
    CopyButton, ErrorBanner, KeyValueList, MarkdownView, SeverityTag, StatusTag, toastError, toastSuccess
} from '../../components/ui';
import { historyText, hostPortText, hostText, shortId, statusText } from './shared';

const { Text, Paragraph } = Typography;

function Section({ title, extra, children }) {
    return (
        <section className="vulnerability-section">
            <div className="vulnerability-section-head">
                <span className="vulnerability-section-title">{title}</span>
                {extra}
            </div>
            <div className="vulnerability-section-body">{children}</div>
        </section>
    );
}

function Narrative({ title, content, code }) {
    const text = String(content ?? '').trim();
    if (!text) return null;
    if (code) {
        return (
            <Section title={title} extra={<CopyButton text={text} size="small" />}>
                <pre className="vulnerability-code">{text}</pre>
            </Section>
        );
    }
    return (
        <Section title={title}>
            <MarkdownView content={text} />
        </Section>
    );
}

export default function DetailDrawer({
    visible, vulnId, refreshKey, onClose, onEdit, onDelete, onOpenVuln,
    canWrite, canDelete, t, projectNames
}) {
    const navigate = useNavigate();
    const [tab, setTab] = useState('overview');
    const [format, setFormat] = useState('docx');
    const [downloading, setDownloading] = useState(false);

    const context = useResource(
        () => api.get(`/vulnerabilities/${encodeURIComponent(vulnId)}/context`),
        [vulnId, refreshKey, visible],
        { skip: !visible || !vulnId }
    );

    useEffect(() => {
        setTab('overview');
        setFormat('docx');
    }, [vulnId]);

    // 只接受与当前 id 匹配的载荷，避免切换记录时闪现上一条详情。
    const payload = context.data?.vulnerability?.id === vulnId ? context.data : null;
    const vuln = payload?.vulnerability || null;
    const history = Array.isArray(payload?.history) ? payload.history : [];
    const members = Array.isArray(payload?.members) ? payload.members : [];
    const assets = Array.isArray(payload?.assets) ? payload.assets : [];
    const projectName = vuln?.project_id ? (projectNames?.[vuln.project_id] || vuln.project_id) : '';

    const openConversation = conversationId => {
        if (!conversationId) return;
        navigate(`/chat?conversation=${encodeURIComponent(conversationId)}`);
    };

    const handleDownload = async () => {
        if (!vuln || downloading) return;
        setDownloading(true);
        try {
            const name = await downloadFile(`/vulnerabilities/${encodeURIComponent(vuln.id)}/download`, { query: { format } });
            toastSuccess(`${t('vulnerabilityPage.downloadOkTitle', null, '下载成功')}${name ? ` · ${name}` : ''}`);
        } catch (error) {
            toastError(error, t('vulnerabilityPage.downloadFailed', null, '下载失败'));
        } finally {
            setDownloading(false);
        }
    };

    const overviewItems = vuln ? [
        {
            label: t('vulnerabilityPage.detailVulnId', null, '漏洞ID'),
            value: <span className="vulnerability-inline-code">{vuln.id}</span>
        },
        {
            label: t('vulnerabilityPage.tableSeverity', null, '严重度'),
            value: <SeverityTag severity={vuln.severity} />
        },
        {
            label: t('vulnerabilityPage.status', null, '状态'),
            value: <StatusTag status={vuln.status} label={statusText(t, vuln.status)} />
        },
        vuln.type ? { label: t('vulnerabilityPage.detailType', null, '类型'), value: vuln.type } : null,
        vuln.target ? { label: t('vulnerabilityPage.detailTarget', null, '目标'), value: <span className="vulnerability-inline-code">{vuln.target}</span> } : null,
        hostText(vuln) && hostText(vuln) !== vuln.target
            ? { label: t('vulnerabilityPage.detailHost', null, '宿主'), value: <span className="vulnerability-inline-code">{hostText(vuln)}</span> }
            : null,
        {
            label: t('vulnerabilityPage.detailProject', null, '所属项目'),
            value: projectName || t('vulnerabilityPage.projectUnbound', null, '未绑定项目')
        },
        vuln.conversation_tag ? { label: t('vulnerabilityPage.detailConversationTag', null, '对话标签'), value: vuln.conversation_tag } : null,
        vuln.conversation_id ? {
            label: t('vulnerabilityPage.detailConversationId', null, '会话ID'),
            value: <span className="vulnerability-inline-code">{vuln.conversation_id}</span>
        } : null,
        vuln.task_tag ? { label: t('vulnerabilityPage.detailTaskTag', null, '任务标签'), value: vuln.task_tag } : null,
        vuln.task_id ? { label: t('vulnerabilityPage.detailTaskId', null, '任务ID'), value: <span className="vulnerability-inline-code">{vuln.task_id}</span> } : null,
        vuln.task_queue_id ? { label: t('vulnerabilityPage.detailTaskQueueId', null, '任务队列ID'), value: <span className="vulnerability-inline-code">{vuln.task_queue_id}</span> } : null,
        vuln.case_id ? { label: t('vulnerabilityPage.mergedRecord', null, '已归并'), value: vuln.case_title || shortId(vuln.case_id, 12) } : null,
        vuln.assessment_id ? {
            label: t('vulnerabilityPage.assessmentTask', null, '深度测评'),
            value: vuln.assessment_name || shortId(vuln.assessment_id, 12)
        } : null,
        { label: t('vulnerabilityMd.labelCreated', null, '创建时间'), value: formatDateTime(vuln.created_at) },
        { label: t('vulnerabilityMd.labelUpdated', null, '更新时间'), value: formatDateTime(vuln.updated_at) }
    ].filter(Boolean) : [];

    const targetList = Array.isArray(vuln?.targets) ? vuln.targets : [];

    return (
        <SideSheet
            className="vulnerability-detail-drawer"
            visible={visible}
            onCancel={onClose}
            width={720}
            title={(
                <div className="vulnerability-drawer-title">
                    <span className="vulnerability-drawer-title-text" title={vuln?.title}>
                        {vuln?.title || t('vulnerabilityPage.inspectorTitle', null, '漏洞工作台')}
                    </span>
                    {vuln && (
                        <span className="vulnerability-drawer-meta">
                            <SeverityTag severity={vuln.severity} />
                            <StatusTag status={vuln.status} label={statusText(t, vuln.status)} />
                            {vuln.type ? <Text type="tertiary">{vuln.type}</Text> : null}
                        </span>
                    )}
                </div>
            )}
            footer={(
                <Space>
                    <Button onClick={onClose}>{t('common.close', null, '关闭')}</Button>
                    <Button icon={<IconRefresh />} onClick={() => context.reload()} loading={context.loading}>
                        {t('common.refresh', null, '刷新')}
                    </Button>
                    {canWrite && (
                        <Button icon={<IconEdit />} disabled={!vuln} onClick={() => onEdit(vuln)}>
                            {t('common.edit', null, '编辑')}
                        </Button>
                    )}
                    {canDelete && (
                        <Button type="danger" icon={<IconDelete />} disabled={!vuln} onClick={() => onDelete(vuln)}>
                            {t('common.delete', null, '删除')}
                        </Button>
                    )}
                </Space>
            )}
        >
            <ErrorBanner error={context.error} onRetry={context.reload} />
            {!context.error && !vuln && (
                <div className="vulnerability-drawer-loading"><Spin size="large" /></div>
            )}
            {vuln && (
                <Tabs type="line" activeKey={tab} onChange={key => setTab(key)}>
                    <Tabs.TabPane tab={t('vulnerabilityPage.inspector_overview', null, '概览')} itemKey="overview">
                        <KeyValueList items={overviewItems} columns={2} />
                        <Narrative title={t('vulnerabilityMd.headingDescription', null, '描述')} content={vuln.description} />
                        <Narrative title={t('vulnerabilityPage.detailImpact', null, '影响')} content={vuln.impact} />
                        <Narrative title={t('vulnerabilityPage.detailRecommendation', null, '修复建议')} content={vuln.recommendation} />
                    </Tabs.TabPane>

                    <Tabs.TabPane tab={t('vulnerabilityPage.inspector_evidence', null, '复现与证据')} itemKey="evidence">
                        <Narrative title={t('vulnerabilityPage.detailPreconditions', null, '前置条件')} content={vuln.preconditions} />
                        <Narrative title={t('vulnerabilityPage.detailReproductionSteps', null, '复现步骤')} content={vuln.reproduction_steps} />
                        <Narrative title={t('vulnerabilityPage.detailEvidence', null, '证据 / POC')} content={vuln.evidence} code />
                        <Narrative title={t('vulnerabilityPage.detailRetestNotes', null, '复测方式')} content={vuln.retest_notes} />
                        <Narrative title={t('vulnerabilityPage.detailRetestLog', null, '复测记录')} content={vuln.retest_log} code />
                        <Section title={t('vulnerabilityPage.inspector_history', null, '处置历史')}>
                            {history.length ? (
                                <Timeline>
                                    {history.map((item, index) => (
                                        <Timeline.Item
                                            key={`${item.kind}-${item.created_at}-${index}`}
                                            time={formatDateTime(item.created_at)}
                                            type={item.kind === 'retest' ? 'success' : 'default'}
                                        >
                                            <div className="vulnerability-history-item">
                                                <div className="vulnerability-history-head">
                                                    <Text strong>{historyText(t, item.kind)}</Text>
                                                    {item.status ? <StatusTag status={item.status} label={statusText(t, item.status)} /> : null}
                                                    {item.actor ? <Text type="tertiary">{item.actor}</Text> : null}
                                                </div>
                                                {item.evidence ? <pre className="vulnerability-code">{item.evidence}</pre> : null}
                                                {item.snapshot ? (
                                                    <Collapse className="vulnerability-snapshot">
                                                        <Collapse.Panel header={t('vulnerabilityPage.retestSnapshot', null, '复测时的证据快照')} itemKey="snapshot">
                                                            <Narrative title={t('vulnerabilityPage.detailReproductionSteps', null, '复现步骤')} content={item.snapshot.reproduction_steps} />
                                                            <Narrative title={t('vulnerabilityPage.detailEvidence', null, '证据 / POC')} content={item.snapshot.evidence} code />
                                                        </Collapse.Panel>
                                                    </Collapse>
                                                ) : null}
                                                {item.conversation_id ? (
                                                    <Button size="small" theme="borderless" onClick={() => openConversation(item.conversation_id)}>
                                                        {t('vulnerabilityPage.openRetestChat', null, '打开复测对话')}
                                                    </Button>
                                                ) : null}
                                            </div>
                                        </Timeline.Item>
                                    ))}
                                </Timeline>
                            ) : (
                                <Paragraph type="tertiary">{t('vulnerabilityPage.noHistory', null, '暂无结构化处置记录')}</Paragraph>
                            )}
                        </Section>
                    </Tabs.TabPane>

                    <Tabs.TabPane tab={t('vulnerabilityPage.detailRelations', null, '关联')} itemKey="relations">
                        <Section title={t('vulnerabilityPage.detailProject', null, '所属项目')}>
                            {vuln.project_id ? (
                                <div className="vulnerability-related-row">
                                    <Text>{projectName}</Text>
                                    <Text type="tertiary" className="vulnerability-inline-code">{vuln.project_id}</Text>
                                </div>
                            ) : (
                                <Paragraph type="tertiary">{t('vulnerabilityPage.projectUnbound', null, '未绑定项目')}</Paragraph>
                            )}
                        </Section>

                        <Section title={t('vulnerabilityPage.affectedTargets', null, '受影响目标')}>
                            {targetList.length ? (
                                <ul className="vulnerability-related-list">
                                    {targetList.map(target => (
                                        <li key={`${target.raw}-${target.host}`} className="vulnerability-related-item">
                                            <Text strong>{target.host || target.domain || target.raw}</Text>
                                            {target.raw && target.raw !== target.host ? (
                                                <span className="vulnerability-inline-code">{target.raw}</span>
                                            ) : null}
                                        </li>
                                    ))}
                                </ul>
                            ) : (
                                <Paragraph type="tertiary">{vuln.target || '—'}</Paragraph>
                            )}
                        </Section>

                        <Section title={t('vulnerabilityPage.linkedAssets', null, '资产库匹配')}>
                            {assets.length ? (
                                <ul className="vulnerability-related-list">
                                    {assets.map(asset => (
                                        <li key={asset.id || `${asset.host}-${asset.port}`} className="vulnerability-related-item">
                                            <Text strong>{hostPortText(asset)}</Text>
                                            <Text type="tertiary" className="vulnerability-inline-code">{shortId(asset.id, 12)}</Text>
                                        </li>
                                    ))}
                                </ul>
                            ) : (
                                <Paragraph type="tertiary">{t('vulnerabilityPage.noLinkedAssets', null, '未匹配到资产库记录')}</Paragraph>
                            )}
                        </Section>

                        <Section title={t('vulnerabilityPage.detailConversationId', null, '会话')}>
                            {vuln.conversation_id ? (
                                <div className="vulnerability-related-row">
                                    <span className="vulnerability-inline-code">{vuln.conversation_id}</span>
                                    <Button size="small" theme="borderless" onClick={() => openConversation(vuln.conversation_id)}>
                                        {t('vulnerabilityPage.openConversation', null, '查看对话')}
                                    </Button>
                                </div>
                            ) : (
                                <Paragraph type="tertiary">{t('vulnerabilityPage.standaloneRecord', null, '独立记录（无关联会话）')}</Paragraph>
                            )}
                            {vuln.task_id || vuln.task_queue_id ? (
                                <div className="vulnerability-related-row">
                                    <Text type="tertiary">{t('vulnerabilityPage.detailTaskId', null, '任务ID')}</Text>
                                    <span className="vulnerability-inline-code">{vuln.task_id || vuln.task_queue_id}</span>
                                </div>
                            ) : null}
                        </Section>

                        {vuln.assessment_id ? (
                            <Section title={t('vulnerabilityPage.assessmentTask', null, '深度测评')}>
                                <div className="vulnerability-related-row">
                                    <Text>{vuln.assessment_name || shortId(vuln.assessment_id, 12)}</Text>
                                    {vuln.assessment_step_title ? <Text type="tertiary">{vuln.assessment_step_title}</Text> : null}
                                </div>
                            </Section>
                        ) : null}

                        {members.length ? (
                            <Section title={vuln.case_title || t('vulnerabilityPage.mergedRecord', null, '已归并')}>
                                <div className="vulnerability-member-list">
                                    {members.map(member => (
                                        <Button
                                            key={member.id}
                                            size="small"
                                            theme="borderless"
                                            disabled={member.id === vuln.id}
                                            onClick={() => onOpenVuln(member.id)}
                                        >
                                            {member.title} · {statusText(t, member.status)}
                                        </Button>
                                    ))}
                                </div>
                            </Section>
                        ) : null}
                    </Tabs.TabPane>

                    <Tabs.TabPane tab={t('vulnerabilityDownload.title', null, '下载报告')} itemKey="download">
                        <div className="vulnerability-download-panel">
                            <Paragraph type="tertiary">
                                {t('vulnerabilityDownload.hint', null, '请选择下载格式')}
                            </Paragraph>
                            <RadioGroup value={format} onChange={event => setFormat(event.target.value)}>
                                <Radio value="docx">{t('vulnerabilityDownload.word', null, 'Word（.docx）')}</Radio>
                                <Radio value="md">{t('vulnerabilityDownload.markdown', null, 'Markdown（.md）')}</Radio>
                            </RadioGroup>
                            <Button theme="solid" type="primary" loading={downloading} onClick={handleDownload}>
                                {t('vulnerabilityDownload.download', null, '下载漏洞报告')}
                            </Button>
                            <Text type="tertiary">
                                {t('vulnerabilityPage.downloadHint', null, '报告由服务端按当前记录生成，包含描述、证据与修复建议。')}
                            </Text>
                        </div>
                    </Tabs.TabPane>
                </Tabs>
            )}
        </SideSheet>
    );
}
