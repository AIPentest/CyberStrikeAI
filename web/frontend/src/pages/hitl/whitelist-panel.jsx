/** 工具白名单：GET/PUT/POST /hitl/tool-whitelist（PUT 整表替换，POST 合并追加）。 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Space, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { ErrorBanner, SectionCard, toastError, toastSuccess } from '../../components/ui';
import { useResource } from '../../components/hooks';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { parseToolTokens, reviewerLabel } from './utils';

const { Text } = Typography;

export default function WhitelistPanel({ canWrite, refreshKey = 0 }) {
    const t = useT();
    const { data, loading, error, reload } = useResource(
        () => api.get('/hitl/tool-whitelist'),
        [refreshKey]
    );
    const [text, setText] = useState('');
    const [saving, setSaving] = useState('');

    useEffect(() => {
        if (data) setText((data.toolWhitelist || []).join('\n'));
    }, [data]);

    const tokens = useMemo(() => parseToolTokens(text), [text]);
    const baseline = useMemo(() => (data?.toolWhitelist || []).join('\n'), [data]);
    const dirty = text.trim() !== baseline.trim();

    const save = useCallback(async () => {
        setSaving('put');
        try {
            const payload = await api.put('/hitl/tool-whitelist', { toolWhitelist: tokens });
            toastSuccess(t('hitl.whitelistSaved', null, '白名单已保存'));
            setText((payload?.toolWhitelist || tokens).join('\n'));
            reload({ silent: true });
        } catch (err) {
            toastError(err, t('hitl.whitelistSaveFailed', null, '保存白名单失败'));
        } finally {
            setSaving('');
        }
    }, [reload, t, tokens]);

    const merge = useCallback(async () => {
        setSaving('post');
        try {
            const payload = await api.post('/hitl/tool-whitelist', { sensitiveTools: tokens });
            toastSuccess(payload?.hitlGlobalWhitelistMerged ? '已合并到全局白名单' : '没有新增工具，白名单未变化');
            setText((payload?.hitlGlobalToolWhitelist || tokens).join('\n'));
            reload({ silent: true });
        } catch (err) {
            toastError(err, '合并白名单失败');
        } finally {
            setSaving('');
        }
    }, [reload, tokens]);

    return (
        <SectionCard
            title={t('hitl.whitelistLabel', null, '免审批工具白名单')}
            description={t('hitl.whitelistHint', null, '每行一个或逗号分隔；保存后写入 config.yaml 全局白名单并立即生效。')}
            extra={(
                <Space spacing={8}>
                    <Button size="small" icon={<IconRefresh />} theme="borderless" onClick={() => reload()} loading={loading}>
                        {t('common.refresh', null, '刷新')}
                    </Button>
                    <Button size="small" disabled={!canWrite || !dirty} loading={saving === 'put'} onClick={save}>
                        {t('common.save', null, '保存')}
                    </Button>
                    <Button size="small" theme="light" disabled={!canWrite || !tokens.length} loading={saving === 'post'} onClick={merge}>
                        合并追加
                    </Button>
                </Space>
            )}
        >
            <ErrorBanner error={error} onRetry={() => reload()} />
            <div className="hitl-whitelist-meta">
                <Text type="tertiary" size="small">
                    {`共 ${tokens.length} 项`}
                    {data?.defaultReviewer ? ` · 默认审批方：${reviewerLabel(data.defaultReviewer)}` : ''}
                </Text>
                {dirty && <Tag size="small" color="amber">有未保存修改</Tag>}
            </div>
            <TextArea
                rows={10}
                value={text}
                spellCheck={false}
                disabled={!canWrite}
                className="hitl-whitelist-textarea"
                placeholder="execute_command&#10;write_file&#10;browser_visit"
                onChange={value => setText(value)}
            />
            {tokens.length > 0 && (
                <div className="hitl-whitelist-tags">
                    {tokens.map(token => <Tag key={token} size="small" color="grey">{token}</Tag>)}
                </div>
            )}
        </SectionCard>
    );
}
