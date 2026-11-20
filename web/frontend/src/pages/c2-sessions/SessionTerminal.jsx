/**
 * C2 会话终端：下发 shell 任务并轮询任务结果，输出回显到本地终端视图。
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Input, Space, Tag } from '@douyinfe/semi-ui';
import { IconSend } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import { toastError, toastWarning } from '../../components/ui';
import * as c2 from './components/c2-api';

let seq = 0;
const nextId = () => { seq += 1; return seq; };

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

export default function SessionTerminal({ session, canWrite, onTaskCreated }) {
    const t = useT();
    const [entries, setEntries] = useState([]);
    const [command, setCommand] = useState('');
    const [running, setRunning] = useState(false);
    const [currentTaskId, setCurrentTaskId] = useState('');
    const alive = useRef(true);
    const logRef = useRef(null);

    useEffect(() => () => { alive.current = false; }, []);

    useEffect(() => {
        const node = logRef.current;
        if (node) node.scrollTop = node.scrollHeight;
    }, [entries]);

    const append = useCallback(entry => {
        setEntries(current => [...current, { id: nextId(), ...entry }]);
    }, []);

    const waitResult = async taskId => {
        let delay = 600;
        for (let attempt = 0; attempt < 40; attempt += 1) {
            if (!alive.current) return null;
            const payload = await c2.getTask(taskId);
            const task = payload?.task;
            if (!task) return null;
            if (['success', 'failed', 'cancelled'].includes(task.status)) return task;
            await sleep(delay);
            delay = Math.min(Math.round(delay * 1.4), 4000);
        }
        return null;
    };

    const run = async raw => {
        const cmd = String(raw ?? command).trim();
        if (!cmd) return;
        if (!canWrite) {
            toastWarning(t('c2.sessions.writeRequired', null, '当前账号没有下发任务的权限'));
            return;
        }
        setCommand('');
        append({ kind: 'cmd', text: cmd });
        setRunning(true);
        try {
            const created = await c2.createSessionTask(session.id, {
                task_type: 'shell',
                payload: { command: cmd, timeout_seconds: 60 }
            });
            const taskId = created?.task?.id;
            if (!taskId) {
                append({ kind: 'err', text: t('c2.sessions.termNoSession', null, '下发失败：未返回任务 ID') });
                return;
            }
            setCurrentTaskId(taskId);
            onTaskCreated?.();
            const task = await waitResult(taskId);
            if (!task) {
                append({ kind: 'warn', text: t('c2.sessions.termWaitTimeout', null, '[等待结果超时]') });
                return;
            }
            if (task.resultText) append({ kind: task.status === 'success' ? 'out' : 'warn', text: task.resultText });
            if (task.error) append({ kind: 'err', text: task.error });
            if (task.status === 'cancelled') append({ kind: 'warn', text: t('c2.tasks.cancelled', null, '已取消') });
        } catch (error) {
            append({ kind: 'err', text: error?.message || t('c2.sessions.termStatusErr', null, '执行失败') });
            toastError(error);
        } finally {
            if (alive.current) {
                setRunning(false);
                setCurrentTaskId('');
            }
        }
    };

    const cancel = async () => {
        if (!currentTaskId) return;
        try {
            await c2.cancelTask(currentTaskId);
            append({ kind: 'warn', text: t('c2.tasks.toastCancelled', null, '任务已取消') });
        } catch (err) {
            toastError(err);
        }
    };

    return (
        <div className="c2-terminal">
            <div className="c2-terminal-bar">
                <Space spacing={8}>
                    <Tag size="small" color={running ? 'amber' : 'green'}>
                        {running ? t('c2.sessions.termStatusExec', null, '执行中…') : t('c2.sessions.termStatusReady', null, '就绪')}
                    </Tag>
                    <Button size="small" theme="borderless" onClick={() => setEntries([])}>{t('c2.sessions.clearTerminal', null, '清屏')}</Button>
                    {running && currentTaskId && <Button size="small" theme="borderless" type="danger" onClick={cancel}>{t('c2.tasks.cancelBtn', null, '取消')}</Button>}
                </Space>
            </div>
            <div className="c2-terminal-log" ref={logRef}>
                <div className="c2-terminal-welcome">
                    {t('c2.sessions.terminalWelcome', null, 'C2 终端 — 回车执行；命令以 shell 任务下发并等待结果')}
                </div>
                {entries.map(entry => (
                    <pre key={entry.id} className={`c2-terminal-line kind-${entry.kind}`}>
                        {entry.kind === 'cmd' ? `$ ${entry.text}` : entry.text}
                    </pre>
                ))}
            </div>
            <div className="c2-terminal-input">
                <Input
                    value={command}
                    onChange={setCommand}
                    onEnterPress={() => (running ? toastWarning(t('c2.sessions.termWaitFinish', null, '请等待当前命令执行完成')) : run())}
                    placeholder={t('c2.sessions.commandPlaceholder', null, '输入命令后回车下发')}
                    disabled={running || !canWrite}
                    prefix="$"
                />
                <Button theme="solid" type="primary" icon={<IconSend />} loading={running} disabled={!canWrite} onClick={() => run()}>
                    {t('c2.sessions.execute', null, '执行命令')}
                </Button>
            </div>
        </div>
    );
}
