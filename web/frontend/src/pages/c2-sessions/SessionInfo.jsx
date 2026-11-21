/**
 * C2 会话信息：身份、系统环境、网络与信标、时间线与备注。
 */
import { Button, Space, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../lib/i18n';
import { JsonViewer, KeyValueList, SectionCard } from '../../components/ui';
import { formatDateTime } from '../../lib/format';

const { Text } = Typography;

export default function SessionInfo({ session, canWrite, onEditNote }) {
    const t = useT();

    const identity = [
        { label: t('c2.sessions.infoSessionId', null, '会话 ID'), value: <Text copyable={{ text: session.id }} className="c2-mono">{session.id}</Text> },
        { label: t('c2.sessions.infoImplantUuid', null, '植入体 UUID'), value: <Text className="c2-mono">{session.implantUuid || '—'}</Text> },
        { label: t('c2.sessions.infoHostname', null, '主机名'), value: session.hostname || '—' },
        { label: t('c2.sessions.infoUsername', null, '用户名'), value: session.username || '—' },
        { label: t('c2.sessions.infoProcess', null, '进程'), value: session.processName || '—' },
        { label: t('c2.sessions.infoPid', null, 'PID'), value: session.pid || '—' }
    ];

    const system = [
        { label: t('c2.sessions.infoOs', null, '操作系统'), value: session.os || '—' },
        { label: t('c2.sessions.infoArch', null, '架构'), value: session.arch || '—' },
        { label: t('c2.sessions.infoAdmin', null, '管理员'), value: session.isAdmin ? t('c2.sessions.adminYes', null, '是') : t('c2.sessions.adminNo', null, '否') },
        { label: t('c2.sessions.infoInternalIp', null, '内网 IP'), value: <span className="c2-mono">{session.internalIp || '—'}</span> },
        { label: t('c2.sessions.ip', null, '外网 IP'), value: <span className="c2-mono">{session.externalIp || '—'}</span> },
        { label: t('c2.sessions.infoSleep', null, 'Sleep'), value: `${session.sleepSeconds || 0}s ± ${session.jitterPercent || 0}%` }
    ];

    const timeline = [
        { label: t('c2.sessions.infoFirstSeen', null, '首次上线'), value: formatDateTime(session.firstSeenAt) },
        { label: t('c2.sessions.infoLastCheckin', null, '上次心跳'), value: formatDateTime(session.lastCheckIn) },
        { label: t('c2.sessions.status', null, '状态'), value: session.status || '—' },
        { label: t('c2.sessions.listener', null, '监听器'), value: <span className="c2-mono">{session.listenerId || '—'}</span> }
    ];

    return (
        <div className="c2-session-info">
            <SectionCard title={t('c2.sessions.infoSectionIdentity', null, '身份信息')}>
                <KeyValueList items={identity} columns={2} />
            </SectionCard>
            <SectionCard title={t('c2.sessions.infoSectionSystem', null, '系统环境')}>
                <KeyValueList items={system} columns={2} />
            </SectionCard>
            <SectionCard title={t('c2.sessions.infoSectionNetwork', null, '网络与信标')}>
                <KeyValueList items={timeline} columns={2} />
            </SectionCard>
            <SectionCard
                title={t('c2.sessions.infoSectionNote', null, '备注')}
                extra={(
                    <Space>
                        <Button size="small" disabled={!canWrite} onClick={onEditNote}>{t('c2.sessions.noteEdit', null, '编辑')}</Button>
                    </Space>
                )}
            >
                <Text type={session.note ? undefined : 'tertiary'}>{session.note || t('c2.sessions.infoNoteEmpty', null, '暂无备注')}</Text>
            </SectionCard>
            {session.metadata && Object.keys(session.metadata).length > 0 && (
                <SectionCard title={t('c2.sessions.infoMetadata', null, '原始元数据')}>
                    <JsonViewer value={session.metadata} collapsed />
                </SectionCard>
            )}
        </div>
    );
}
