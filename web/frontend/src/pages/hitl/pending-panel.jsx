/** 待审批：GET /hitl/pending，决策 POST /hitl/decision，忽略 POST /hitl/dismiss。 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Button, Empty, Input, Pagination, Space, Spin, Tag, TextArea, Typography
} from '@douyinfe/semi-ui';
import { useDebounced, useResource } from '../../components/hooks';
import {
    CopyButton, ErrorBanner, JsonViewer, toastError, toastSuccess, toastWarning, confirmAction
} from '../../components/ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import ContextBlocks from './blocks';
import {
    approvalDeadlineText, formatHitlTime, modeLabel, normalizeMode,
    parsePayload, payloadArguments, reviewerLabel, reviewerValue
} from './utils';

const { Text } = Typography;

function PendingItem({ item, canWrite, onDone }) {
    const payload = useMemo(() => parsePayload(item.payload), [item.payload]);
    const args = useMemo(() => payloadArguments(payload), [payload]);
    const allowEdit = normalizeMode(item.mode) === 'review_edit';
    const reviewer = reviewerValue(item.reviewer);
    const [comment, setComment] = useState('');
    const [argsText, setArgsText] = useState(() => (allowEdit && args ? JSON.stringify(args, null, 2) : ''));
    const [busy, setBusy] = useState('');
    const [inlineError, setInlineError] = useState('');

    const decide = useCallback(async decision => {
        if (busy) return;
        let editedArguments;
        if (allowEdit && argsText.trim()) {
            try {
                editedArguments = JSON.parse(argsText);
            } catch (error) {
                setInlineError(`修改后的参数不是合法 JSON：${error.message}`);
                return;
            }
            if (!editedArguments || typeof editedArguments !== 'object' || Array.isArray(editedArguments)) {
                setInlineError('修改后的参数必须是 JSON 对象');
                return;
            }
        }
        setInlineError('');
        setBusy(decision);
        try {
            await api.post('/hitl/decision', {
                interruptId: item.id,
                decision,
                comment: comment.trim(),
                editedArguments
            });
            toastSuccess(decision === 'approve' ? '已通过，工具将继续执行' : '已拒绝本次工具调用');
            onDone?.();
        } catch (error) {
            if (error?.status === 409) {
                await api.post('/hitl/dismiss', { interruptId: item.id }).catch(() => null);
                toastWarning('该审批已被处理，列表已刷新');
                onDone?.();
                return;
            }
            toastError(error, '提交审批失败');
        } finally {
            setBusy('');
        }
    }, [allowEdit, argsText, busy, comment, item.id, onDone]);

    const dismiss = useCallback(() => {
        confirmAction({
            title: '忽略该审批',
            content: '忽略后该审批记为取消（reject），本次工具调用不会继续执行，记录可在审批日志中查看。',
            okText: '忽略',
            onOk: async () => {
                try {
                    await api.post('/hitl/dismiss', { interruptId: item.id });
                    toastSuccess('已忽略该审批');
                    onDone?.();
                } catch (error) {
                    toastError(error, '忽略审批失败');
                }
            }
        });
    }, [item.id, onDone]);

    return (
        <div className="hitl-pending-card">
            <div className="hitl-pending-head">
                <Space spacing={6} wrap>
                    <Tag size="small" color="blue" shape="square">{item.toolName || '—'}</Tag>
                    <Tag size="small" color={allowEdit ? 'violet' : 'grey'}>{modeLabel(item.mode)}</Tag>
                    <Tag size="small" color={reviewer === 'audit_agent' ? 'green' : 'grey'}>{reviewerLabel(item.reviewer)}</Tag>
                </Space>
                <Space spacing={6}>
                    <Text type="tertiary" size="small">{formatHitlTime(item.createdAt)}</Text>
                    <Button size="small" theme="borderless" disabled={!canWrite || Boolean(busy)} onClick={dismiss}>
                        忽略
                    </Button>
                </Space>
            </div>

            <div className="hitl-pending-meta">
                <span className="hitl-pending-meta-label">会话</span>
                <span className="hitl-mono">{item.conversationId || '—'}</span>
                {item.conversationId && <CopyButton text={item.conversationId} size="small" />}
                <span className="hitl-pending-deadline">{approvalDeadlineText(payload)}</span>
            </div>

            <ContextBlocks payload={payload} />

            {args && <JsonViewer title="请求参数" value={args} collapsed />}

            <div className="hitl-pending-editor">
                <span className="hitl-context-label">
                    {allowEdit ? '修改后的参数（可编辑，留空表示沿用原参数）' : '审批模式：仅可通过/拒绝，参数编辑已禁用'}
                </span>
                <TextArea
                    rows={allowEdit ? 6 : 3}
                    value={allowEdit ? argsText : (args ? JSON.stringify(args, null, 2) : '')}
                    disabled={!allowEdit}
                    spellCheck={false}
                    className="hitl-args-textarea"
                    onChange={value => { setArgsText(value); if (inlineError) setInlineError(''); }}
                />
            </div>

            {inlineError && <div className="hitl-inline-error">{inlineError}</div>}

            <Input
                value={comment}
                placeholder="审批备注（可选），例如：只读命令，允许执行"
                onChange={value => setComment(value)}
            />

            <div className="hitl-pending-actions">
                <Button
                    type="danger"
                    theme="light"
                    disabled={!canWrite || Boolean(busy)}
                    loading={busy === 'reject'}
                    onClick={() => decide('reject')}
                >
                    拒绝
                </Button>
                <Button
                    theme="solid"
                    disabled={!canWrite || Boolean(busy)}
                    loading={busy === 'approve'}
                    onClick={() => decide('approve')}
                >
                    {allowEdit && argsText.trim() && argsText.trim() !== (args ? JSON.stringify(args, null, 2) : '')
                        ? '保存修改并放行'
                        : '通过'}
                </Button>
            </div>

            <JsonViewer title="完整载荷" value={payload} collapsed />
        </div>
    );
}

export default function PendingPanel({ canWrite, onCountChange, refreshKey = 0 }) {
    const t = useT();
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [searchInput, setSearchInput] = useState('');
    const q = useDebounced(searchInput, 350);

    const list = useResource(
        () => api.get('/hitl/pending', { page, pageSize, q }),
        [page, pageSize, q, refreshKey]
    );

    const items = list.data?.items || [];
    const total = Number(list.data?.total) || 0;

    useEffect(() => {
        onCountChange?.(total);
    }, [onCountChange, total]);

    const handlePageSize = useCallback(size => {
        setPageSize(size);
        setPage(1);
    }, []);

    return (
        <div className="hitl-pending-panel">
            <div className="hitl-panel-toolbar">
                <Input
                    showClear
                    value={searchInput}
                    style={{ width: 300 }}
                    placeholder={t('hitl.searchPlaceholder', null, '搜索工具、会话或载荷…')}
                    onChange={value => { setSearchInput(value); setPage(1); }}
                />
                <Text type="tertiary" size="small">
                    {t('hitl.pendingTitle', null, '待审批请求')} · {total}
                </Text>
            </div>

            <ErrorBanner error={list.error} onRetry={() => list.reload()} />

            {list.loading && !items.length && (
                <div className="hitl-panel-loading"><Spin /></div>
            )}

            {!list.loading && !items.length && !list.error && (
                <Empty
                    title={t('hitl.emptyState', null, '暂无待审批请求')}
                    description="工具调用命中敏感规则时会在此等待裁决。"
                    style={{ padding: 48 }}
                />
            )}

            <div className="hitl-pending-list">
                {items.map(item => (
                    <PendingItem
                        key={item.id}
                        item={item}
                        canWrite={canWrite}
                        onDone={() => {
                            if (items.length <= 1 && page > 1) setPage(page - 1);
                            else list.reload();
                        }}
                    />
                ))}
            </div>

            {total > 0 && (
                <div className="hitl-panel-pagination">
                    <Pagination
                        currentPage={page}
                        pageSize={pageSize}
                        total={total}
                        showSizeChanger
                        pageSizeOpts={[10, 20, 50]}
                        showTotal
                        onPageChange={next => setPage(next)}
                        onPageSizeChange={handlePageSize}
                    />
                </div>
            )}
        </div>
    );
}
