/**
 * C2 会话工作区：头部元信息 + 终端/文件/任务/信息四个 Tab。
 */
import { useState } from 'react';
import { Button, Input, Space, Tabs, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { Drawer, ErrorBanner, confirmAction, toastError, toastSuccess, toastWarning } from '../../components/ui';
import * as c2 from './components/c2-api';
import { SESSION_STATUS_COLOR } from './components/c2-shared';
import SessionTerminal from './SessionTerminal';
import SessionFiles from './SessionFiles';
import SessionTasks from './SessionTasks';
import SessionInfo from './SessionInfo';

const { Text } = Typography;

export default function SessionDetail({ session, canWrite, canDelete, onChanged, onDeleted }) {
    const t = useT();
    const [tab, setTab] = useState('terminal');
    const [sleepModal, setSleepModal] = useState(null);
    const [noteModal, setNoteModal] = useState(null);
    const [saving, setSaving] = useState(false);
    const { data, loading, error, reload } = useResource(() => c2.getSession(session.id), [session.id]);
    const detail = data?.session || session;

    const openSleep = () => setSleepModal({
        sleepSeconds: String(detail.sleepSeconds || 5),
        jitterPercent: String(detail.jitterPercent || 0)
    });
    const openNote = () => setNoteModal({ note: detail.note || '' });

    const saveSleep = async () => {
        const sleepSeconds = Number(sleepModal.sleepSeconds);
        const jitterPercent = Number(sleepModal.jitterPercent);
        if (!Number.isFinite(sleepSeconds) || sleepSeconds < 1) {
            toastWarning(t('c2.sessions.toastSleepInvalid', null, 'Sleep 间隔至少为 1 秒'));
            return;
        }
        if (!Number.isFinite(jitterPercent) || jitterPercent < 0 || jitterPercent > 100) {
            toastWarning(t('c2.sessions.toastSleepInvalid', null, '抖动百分比需在 0–100 之间'));
            return;
        }
        setSaving(true);
        try {
            await c2.setSessionSleep(detail.id, { sleepSeconds, jitterPercent });
            toastSuccess(t('c2.sessions.toastSleepUpdated', null, 'Sleep 设置已更新'));
            setSleepModal(null);
            reload({ silent: true });
            onChanged?.();
        } catch (err) {
            toastError(err);
        } finally {
            setSaving(false);
        }
    };

    const saveNote = async () => {
        const note = String(noteModal.note || '').trim();
        if (note.length > 2000) {
            toastWarning(t('c2.sessions.toastNoteTooLong', null, '备注最多 2000 字'));
            return;
        }
        setSaving(true);
        try {
            await c2.setSessionNote(detail.id, note);
            toastSuccess(t('c2.sessions.toastNoteSaved', null, '备注已保存'));
            setNoteModal(null);
            reload({ silent: true });
            onChanged?.();
        } catch (err) {
            toastError(err);
        } finally {
            setSaving(false);
        }
    };

    const remove = () => {
        confirmAction({
            title: t('c2.sessions.cardDeleteSession', null, '删除会话'),
            content: t('c2.sessions.confirmDeleteSession', null, '从服务器删除此会话及其关联任务与文件记录？'),
            onOk: async () => {
                try {
                    await c2.deleteSession(detail.id);
                    toastSuccess(t('c2.sessions.toastSessionDeleted', null, '会话记录已删除'));
                    onDeleted?.();
                } catch (err) {
                    toastError(err);
                }
            }
        });
    };

    const items = [
        { tab: t('c2.sessions.terminal', null, '终端'), itemKey: 'terminal', children: <SessionTerminal session={detail} canWrite={canWrite} onTaskCreated={reload} /> },
        { tab: t('c2.sessions.files', null, '文件'), itemKey: 'files', children: <SessionFiles session={detail} canWrite={canWrite} /> },
        { tab: t('c2.sessions.tasks', null, '任务'), itemKey: 'tasks', children: <SessionTasks session={detail} canDelete={canDelete} onChanged={reload} /> },
        { tab: t('c2.sessions.info', null, '信息'), itemKey: 'info', children: <SessionInfo session={detail} canWrite={canWrite} onEditNote={openNote} /> }
    ];

    return (
        <div className="c2-session-detail">
            <div className="c2-session-head">
                <div className="c2-session-title">
                    <Text strong className="c2-session-hostname">{detail.hostname || t('c2.sessions.unknownHost', null, '未知主机')}</Text>
                    <Space spacing={6} wrap>
                        <Tag size="small" color={SESSION_STATUS_COLOR[detail.status] || 'grey'}>
                            {t(`c2.sessions.${detail.status || 'active'}`, null, detail.status || '—')}
                        </Tag>
                        <Tag size="small">{detail.os || '—'}/{detail.arch || '—'}</Tag>
                        {detail.isAdmin && <Tag size="small" color="red">{t('c2.sessions.rootBadge', null, 'ROOT')}</Tag>}
                        <Text type="tertiary" size="small">{detail.username || '—'}@{detail.internalIp || detail.externalIp || '—'}</Text>
                        <Text type="tertiary" size="small">sleep {detail.sleepSeconds || 0}s ± {detail.jitterPercent || 0}%</Text>
                    </Space>
                </div>
                <Space spacing={6} wrap>
                    <Button size="small" icon={<IconRefresh />} loading={loading} onClick={() => reload()}>{t('common.refresh', null, '刷新')}</Button>
                    <Button size="small" disabled={!canWrite} onClick={openSleep}>{t('c2.sessions.btnSleep', null, 'Sleep')}</Button>
                    <Button size="small" disabled={!canWrite} onClick={openNote}>{t('c2.sessions.noteEdit', null, '备注')}</Button>
                    <Button size="small" type="danger" disabled={!canDelete} onClick={remove}>{t('c2.sessions.cardDeleteSession', null, '删除会话')}</Button>
                </Space>
            </div>

            <ErrorBanner error={error} onRetry={reload} />

            <Tabs type="button" activeKey={tab} onChange={setTab} items={items} />

            <Drawer
                visible={Boolean(sleepModal)}
                title={t('c2.sessions.sleepModalTitle', null, '心跳配置')}
                width={440}
                onCancel={() => setSleepModal(null)}
                onOk={saveSleep}
                confirmLoading={saving}
            >
                <Text type="tertiary" size="small">{t('c2.sessions.sleepModalHint', null, '保存后将写入服务端并下发 sleep 任务。')}</Text>
                <div className="c2-session-modal-field">
                    <Text size="small">{t('c2.sessions.promptSleepSeconds', null, 'Sleep 间隔（秒）')}</Text>
                    <Input
                        value={sleepModal?.sleepSeconds || ''}
                        onChange={value => setSleepModal(current => ({ ...current, sleepSeconds: value }))}
                    />
                </div>
                <div className="c2-session-modal-field">
                    <Text size="small">{t('c2.sessions.promptJitterPercent', null, '抖动百分比（0–100）')}</Text>
                    <Input
                        value={sleepModal?.jitterPercent || ''}
                        onChange={value => setSleepModal(current => ({ ...current, jitterPercent: value }))}
                    />
                </div>
            </Drawer>

            <Drawer
                visible={Boolean(noteModal)}
                title={t('c2.sessions.infoNote', null, '备注')}
                width={480}
                onCancel={() => setNoteModal(null)}
                onOk={saveNote}
                confirmLoading={saving}
            >
                <TextArea
                    value={noteModal?.note || ''}
                    onChange={value => setNoteModal(current => ({ ...current, note: value }))}
                    placeholder={t('c2.sessions.notePlaceholder', null, '添加备注，便于识别该会话…')}
                    autosize={{ minRows: 4, maxRows: 10 }}
                />
            </Drawer>
        </div>
    );
}
