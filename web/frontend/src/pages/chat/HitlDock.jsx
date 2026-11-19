/** 待审批停靠区：把内联审批卡堆在发送区上方，随流式事件与轮询刷新。 */
import { Button, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import HitlCard from './HitlCard';
import { useT } from '../../lib/i18n';

const { Text } = Typography;

export default function HitlDock({ items, busyId, readOnly, onRefresh, onDecide, onDismiss }) {
    const t = useT();
    const list = Array.isArray(items) ? items : [];
    if (!list.length) return null;
    return (
        <div className="chat-hitl-dock">
            <div className="chat-hitl-dock-head">
                <Text strong>{t('chat.pendingApprovals', { count: list.length }, `待审批 ${list.length}`)}</Text>
                <Button size="small" theme="borderless" icon={<IconRefresh />} onClick={onRefresh} />
            </div>
            {list.map(item => (
                <HitlCard
                    key={item.id || item.interruptId}
                    item={item}
                    readOnly={readOnly}
                    busy={busyId === String(item.id || item.interruptId)}
                    onApprove={(data, comment) => onDecide(data, 'approve', comment)}
                    onReject={(data, comment) => onDecide(data, 'reject', comment)}
                    onDismiss={onDismiss}
                />
            ))}
        </div>
    );
}
