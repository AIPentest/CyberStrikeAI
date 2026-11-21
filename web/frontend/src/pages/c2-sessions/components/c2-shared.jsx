/**
 * C2 页面共享常量、时间快捷筛选与展示小工具。
 */
import { Button, Space, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../../lib/i18n';
import { formatDateTime, relativeTime } from '../../../lib/format';
import './c2-shared.css';

const { Text } = Typography;

export const LISTENER_TYPES = [
    { value: 'http_beacon', label: 'HTTP Beacon' },
    { value: 'https_beacon', label: 'HTTPS Beacon' },
    { value: 'tcp_reverse', label: 'TCP 反向' },
    { value: 'websocket', label: 'WebSocket' }
];

export const LISTENER_STATUS_LABEL = {
    running: '运行中',
    stopped: '已停止',
    error: '异常'
};

export const HEARTBEAT_TYPES = new Set(['http_beacon', 'https_beacon', 'websocket']);

export const ONELINER_KINDS = {
    tcp_reverse: [
        { value: 'bash', label: 'Bash (/dev/tcp)' },
        { value: 'nc', label: 'Netcat (-e)' },
        { value: 'nc_mkfifo', label: 'Netcat (mkfifo)' },
        { value: 'python', label: 'Python' },
        { value: 'perl', label: 'Perl' },
        { value: 'powershell', label: 'PowerShell' }
    ],
    http_beacon: [{ value: 'curl_beacon', label: 'Curl Beacon (HTTP)' }],
    https_beacon: [{ value: 'curl_beacon', label: 'Curl Beacon (HTTP)' }],
    websocket: [{ value: 'curl_beacon', label: 'Curl Beacon (HTTP)' }]
};

export const TASK_TYPES = [
    { value: 'shell', label: 'shell' },
    { value: 'exec', label: 'exec' },
    { value: 'ls', label: 'ls' },
    { value: 'pwd', label: 'pwd' },
    { value: 'cd', label: 'cd' },
    { value: 'ps', label: 'ps' },
    { value: 'kill_proc', label: 'kill_proc' },
    { value: 'download', label: 'download' },
    { value: 'upload', label: 'upload' },
    { value: 'screenshot', label: 'screenshot' },
    { value: 'sleep', label: 'sleep' },
    { value: 'exit', label: 'exit' },
    { value: 'self_delete', label: 'self_delete' },
    { value: 'port_fwd', label: 'port_fwd' },
    { value: 'socks_start', label: 'socks_start' },
    { value: 'socks_stop', label: 'socks_stop' },
    { value: 'load_assembly', label: 'load_assembly' },
    { value: 'persist', label: 'persist' }
];

export const TASK_STATUS_OPTIONS = [
    { value: 'queued', label: '队列中' },
    { value: 'sent', label: '已发送' },
    { value: 'running', label: '执行中' },
    { value: 'success', label: '成功' },
    { value: 'failed', label: '失败' },
    { value: 'cancelled', label: '已取消' }
];

export const EVENT_LEVEL_OPTIONS = [
    { value: 'info', label: '信息' },
    { value: 'warn', label: '警告' },
    { value: 'critical', label: '严重' }
];

export const EVENT_CATEGORY_OPTIONS = [
    { value: 'listener', label: '监听器' },
    { value: 'session', label: '会话' },
    { value: 'task', label: '任务' },
    { value: 'payload', label: 'Payload' },
    { value: 'opsec', label: 'OPSEC' }
];

export const SESSION_STATUS_OPTIONS = [
    { value: 'active', label: '活跃' },
    { value: 'sleeping', label: '休眠' },
    { value: 'dead', label: '离线' },
    { value: 'killed', label: '已终止' }
];

/** 任务/事件轮询间隔：需求要求 5 秒轮询。 */
export const POLL_INTERVAL = 5000;

export const TIME_PRESETS = [
    { value: '15m', label: '最近15分钟', ms: 15 * 60 * 1000 },
    { value: '1h', label: '最近1小时', ms: 60 * 60 * 1000 },
    { value: '24h', label: '最近24小时', ms: 24 * 60 * 60 * 1000 },
    { value: '7d', label: '最近7天', ms: 7 * 24 * 60 * 60 * 1000 },
    { value: 'all', label: '全部', ms: 0 }
];

/** 快捷时间窗 → since（RFC3339）；all 返回空串。 */
export function presetToSince(preset) {
    const item = TIME_PRESETS.find(entry => entry.value === preset);
    if (!item || !item.ms) return '';
    return new Date(Date.now() - item.ms).toISOString();
}

export function TimePresetBar({ value, onChange, ns = 'tasks' }) {
    const t = useT();
    return (
        <Space spacing={6} wrap className="c2-time-presets">
            <Text type="tertiary" size="small">{t('c2.tasks.timePresets', null, '快捷')}</Text>
            {TIME_PRESETS.map(preset => (
                <Button
                    key={preset.value}
                    size="small"
                    theme={preset.value === value ? 'solid' : 'borderless'}
                    type={preset.value === value ? 'primary' : 'tertiary'}
                    onClick={() => onChange(preset.value)}
                >
                    {t(`c2.${ns}.preset${preset.value === 'all' ? 'All' : preset.value}`, null, preset.label)}
                </Button>
            ))}
        </Space>
    );
}

export function formatDurationMs(ms) {
    const value = Number(ms);
    if (!Number.isFinite(value) || value <= 0) return '—';
    if (value < 1000) return `${Math.round(value)}ms`;
    if (value < 60_000) return `${(value / 1000).toFixed(1)}秒`;
    return `${Math.round(value / 60_000)}分钟`;
}

export function TimeText({ value }) {
    const t = useT();
    if (!value) return <Text type="tertiary">—</Text>;
    return (
        <span className="c2-time-text" title={formatDateTime(value)}>
            {relativeTime(value, t)}
        </span>
    );
}

export function truncateMiddle(text, max = 42) {
    const value = String(text || '');
    if (value.length <= max) return value;
    const side = Math.floor((max - 1) / 2);
    return `${value.slice(0, side)}…${value.slice(value.length - side)}`;
}

export const LISTENER_STATUS_COLOR = { running: 'green', stopped: 'grey', error: 'red' };
export const TASK_STATUS_COLOR = {
    queued: 'amber', sent: 'blue', running: 'blue', success: 'green', failed: 'red', cancelled: 'grey'
};
export const SESSION_STATUS_COLOR = { active: 'green', sleeping: 'amber', dead: 'grey', killed: 'red' };
export const EVENT_LEVEL_COLOR = { info: 'blue', warn: 'amber', critical: 'red' };
