/**
 * WebShell 虚拟终端：逐条下发命令并把回显写回输出区。
 * 输出只保留在内存中，命令历史由上层持久化到连接状态。
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Input, Space, Tag } from '@douyinfe/semi-ui';
import { IconSend } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import { toastError, toastSuccess, toastWarning } from '../../components/ui';
import { execCommand } from './api';

let seq = 0;
const nextId = () => { seq += 1; return seq; };

export default function TerminalTab({ connection, canWrite, onRecordCommand }) {
    const t = useT();
    const [entries, setEntries] = useState([]);
    const [command, setCommand] = useState('');
    const [running, setRunning] = useState(false);
    const logRef = useRef(null);

    const append = useCallback(entry => {
        setEntries(current => [...current, { id: nextId(), ...entry }]);
    }, []);

    useEffect(() => {
        setEntries([]);
        setCommand('');
    }, [connection?.id]);

    useEffect(() => {
        const node = logRef.current;
        if (node) node.scrollTop = node.scrollHeight;
    }, [entries]);

    const run = async raw => {
        const cmd = String(raw ?? command).trim();
        if (!cmd) return;
        if (!canWrite) {
            toastWarning(t('webshell.webshellWriteRequired', null, '当前账号没有执行权限'));
            return;
        }
        setCommand('');
        append({ kind: 'cmd', text: cmd });
        onRecordCommand?.(cmd);
        setRunning(true);
        try {
            const result = await execCommand({ connection, command: cmd });
            if (result?.output) append({ kind: 'out', text: result.output });
            if (result?.error) append({ kind: 'err', text: result.error });
            if (!result?.ok && !result?.error) {
                append({ kind: 'err', text: t('webshell.execError', null, '执行失败') });
            }
        } catch (error) {
            append({ kind: 'err', text: error?.message || t('webshell.execError', null, '执行失败') });
            toastError(error);
        } finally {
            setRunning(false);
        }
    };

    const copyLog = async () => {
        const text = entries.map(entry => (entry.kind === 'cmd' ? `$ ${entry.text}` : entry.text)).join('\n');
        try {
            await navigator.clipboard.writeText(text);
            toastSuccess(t('webshell.terminalCopyOk', null, '日志已复制'));
        } catch (error) {
            toastError(error, t('webshell.terminalCopyFail', null, '复制失败'));
        }
    };

    return (
        <div className="webshell-terminal">
            <div className="webshell-terminal-bar">
                <Space spacing={8}>
                    <Tag size="small" color={running ? 'amber' : 'green'}>
                        {running ? t('webshell.terminalRunning', null, '执行中') : t('webshell.terminalIdle', null, '空闲')}
                    </Tag>
                    <Button size="small" theme="borderless" onClick={() => setEntries([])}>{t('webshell.clearScreen', null, '清屏')}</Button>
                    <Button size="small" theme="borderless" onClick={copyLog}>{t('webshell.copyTerminalLog', null, '复制日志')}</Button>
                </Space>
            </div>
            <div className="webshell-terminal-log" ref={logRef}>
                <div className="webshell-terminal-welcome">
                    {t('webshell.terminalWelcome', null, 'WebShell 虚拟终端 — 输入命令后按回车执行（Ctrl+L 清屏）')}
                </div>
                {entries.map(entry => (
                    <pre key={entry.id} className={`webshell-terminal-line kind-${entry.kind}`}>
                        {entry.kind === 'cmd' ? `$ ${entry.text}` : entry.text}
                    </pre>
                ))}
                {running && <div className="webshell-terminal-pending">{t('webshell.running', null, '执行中…')}</div>}
            </div>
            <div className="webshell-terminal-input">
                <Input
                    value={command}
                    onChange={setCommand}
                    onEnterPress={() => (running ? toastWarning(t('webshell.waitFinish', null, '请等待当前命令执行完成')) : run())}
                    onKeyDown={event => {
                        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'l') {
                            event.preventDefault();
                            setEntries([]);
                        }
                    }}
                    placeholder={t('webshell.commandPlaceholder', null, '输入命令后回车执行')}
                    disabled={running || !canWrite}
                    prefix="$"
                />
                <Button
                    theme="solid"
                    type="primary"
                    icon={<IconSend />}
                    loading={running}
                    disabled={!canWrite}
                    onClick={() => run()}
                >
                    {t('webshell.runCommand', null, '执行')}
                </Button>
            </div>
        </div>
    );
}
