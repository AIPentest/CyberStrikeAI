/**
 * 共享展示组件：页面骨架、指标卡、表格、空状态、标签、Markdown 与 JSON 视图。
 * 业务页面统一使用这些组件，避免各页重复写样式。
 */
import { useMemo, useState } from 'react';
import {
    Banner, Button, Card, Collapsible, Descriptions, Empty, Modal, SideSheet, Skeleton, Space, Table, Tag, Toast, Typography
} from '@douyinfe/semi-ui';
import { marked } from 'marked';
import DOMPurify from 'dompurify';
import { IconChevronDown, IconChevronUp, IconCopy, IconInbox, IconRefresh } from '@douyinfe/semi-icons';
import { useT } from '../lib/i18n';
import { SEVERITY_LABEL, SEVERITY_ORDER, severityColor, statusColor } from '../lib/format';
import { errorMessage } from '../lib/http';

const { Title, Text, Paragraph } = Typography;

export function toastSuccess(content) {
    Toast.success({ content, duration: 2 });
}

export function toastWarning(content) {
    Toast.warning({ content, duration: 3 });
}

export function toastError(error, fallback = '操作失败') {
    Toast.error({ content: errorMessage(error, fallback), duration: 4 });
}

export function PageHeader({ title, description, extra, children }) {
    return (
        <header className="cs-page-header">
            <div className="cs-page-header-main">
                <Title heading={3} className="cs-page-title">{title}</Title>
                {description && <Text type="tertiary" className="cs-page-desc">{description}</Text>}
                {children}
            </div>
            {extra && <Space className="cs-page-header-extra" wrap>{extra}</Space>}
        </header>
    );
}

export function SectionCard({ title, description, extra, children, className = '', bodyClassName = '' }) {
    return (
        <Card
            className={`cs-section-card ${className}`}
            title={title ? <span className="cs-section-title">{title}</span> : undefined}
            headerExtraContent={extra}
            bordered={false}
        >
            {description && <Paragraph type="tertiary" className="cs-section-desc">{description}</Paragraph>}
            <div className={bodyClassName}>{children}</div>
        </Card>
    );
}

export function StatGrid({ metrics, columns = 4 }) {
    return (
        <div className="cs-stat-grid" style={{ '--cs-stat-columns': columns }}>
            {metrics.map(metric => (
                <StatCard key={metric.id || metric.label} {...metric} />
            ))}
        </div>
    );
}

export function StatCard({ label, value, valueTitle, detail, tone, severity, icon, onClick, loading }) {
    const content = (
        <>
            <span className="cs-stat-head">
                <Text type="tertiary" className="cs-stat-label">{label}</Text>
                {icon && <span className={`cs-stat-icon ${tone ? `tone-${tone}` : ''}`} aria-hidden="true">{icon}</span>}
            </span>
            <Skeleton loading={loading} placeholder={<Skeleton.Title style={{ width: '60%', height: 36, margin: '8px 0' }} />}>
                <span className={`cs-stat-value ${tone ? `tone-${tone}` : ''}`} data-severity={severity} title={valueTitle}>
                    {value ?? '—'}
                </span>
            </Skeleton>
            {detail && <span className="cs-stat-detail">{detail}</span>}
        </>
    );
    return onClick ? (
        <Button className="cs-stat-card is-clickable" theme="borderless" onClick={onClick} aria-label={String(label)}>{content}</Button>
    ) : (
        <Card className="cs-stat-card" bordered={false}>{content}</Card>
    );
}

export function DataTable({
    columns, dataSource, rowKey = 'id', loading, pagination, emptyText, onRefresh,
    toolbar, size = 'default', className = '', ...rest
}) {
    const t = useT();
    const mergedPagination = pagination === false ? false : {
        pageSize: 10,
        showSizeChanger: true,
        pageSizeOpts: [10, 20, 50, 100],
        showTotal: true,
        ...(pagination || {})
    };
    return (
        <div className={`cs-table-wrap ${className}`}>
            {(toolbar || onRefresh) && (
                <div className="cs-table-toolbar">
                    <div className="cs-table-toolbar-main">{toolbar}</div>
                    {onRefresh && (
                        <Button icon={<IconRefresh />} theme="borderless" onClick={onRefresh} loading={loading}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                    )}
                </div>
            )}
            <div className="cs-table-scroll">
                <Table
                    columns={columns}
                    dataSource={dataSource || []}
                    rowKey={rowKey}
                    loading={loading}
                    pagination={mergedPagination}
                    size={size}
                    empty={<Empty image={<IconInbox size="extra-large" />} title={emptyText || t('common.noData', null, '暂无数据')} description={null} className="cs-table-empty" />}
                    {...rest}
                />
            </div>
        </div>
    );
}

export function ErrorBanner({ error, onRetry }) {
    const t = useT();
    if (!error) return null;
    return (
        <Banner
            type="danger"
            bordered
            closeIcon={null}
            className="cs-error-banner"
            description={<Space wrap>
                <span>{errorMessage(error, t('common.loadFailed', null, '加载失败'))}</span>
                {onRetry && <Button size="small" onClick={onRetry}>{t('common.refresh', null, '刷新')}</Button>}
            </Space>}
        />
    );
}

export function SeverityTag({ severity, size = 'small' }) {
    const key = String(severity || '').toLowerCase();
    return (
        <Tag size={size} color={severityColor(key)} shape="circle" className="cs-severity-tag"
            data-severity={SEVERITY_ORDER.includes(key) ? key : undefined}>
            {SEVERITY_LABEL[key] || severity || '—'}
        </Tag>
    );
}

export function StatusTag({ status, label, size = 'small' }) {
    const key = String(status || '').toLowerCase();
    return <Tag size={size} color={statusColor(key)} shape="square">{label || status || '—'}</Tag>;
}

export function CopyButton({ text, label, size = 'default', theme = 'borderless' }) {
    const t = useT();
    return (
        <Button
            size={size}
            theme={theme}
            icon={<IconCopy />}
            aria-label={label || t('common.copy', null, '复制')}
            onClick={async () => {
                try {
                    await navigator.clipboard.writeText(String(text ?? ''));
                    toastSuccess(t('common.copied', null, '已复制'));
                } catch (error) {
                    toastError(error, t('common.copyFailed', null, '复制失败'));
                }
            }}
        >
            {label}
        </Button>
    );
}

export function MarkdownView({ content, className = '' }) {
    const html = useMemo(() => {
        const raw = marked.parse(String(content ?? ''), { breaks: true, gfm: true });
        return DOMPurify.sanitize(raw, { ADD_ATTR: ['target', 'rel'] });
    }, [content]);
    return <div className={`cs-markdown ${className}`} dangerouslySetInnerHTML={{ __html: html }} />;
}

export function JsonViewer({ value, collapsed = false, title }) {
    const [open, setOpen] = useState(!collapsed);
    const t = useT();
    const text = useMemo(() => {
        try {
            return JSON.stringify(value, null, 2);
        } catch (error) {
            return String(value);
        }
    }, [value]);
    return (
        <div className="cs-json-viewer">
            <div className="cs-json-viewer-head">
                <Text strong>{title || 'JSON'}</Text>
                <Space spacing={4}>
                    <CopyButton text={text} />
                    <Button theme="borderless" size="small" icon={open ? <IconChevronUp /> : <IconChevronDown />}
                        aria-expanded={open} aria-label={t(open ? 'common.collapse' : 'common.expand', null, open ? '收起' : '展开')}
                        onClick={() => setOpen(current => !current)} />
                </Space>
            </div>
            <Collapsible isOpen={open}>
                <pre className="cs-json-viewer-body">{text}</pre>
            </Collapsible>
        </div>
    );
}

export function KeyValueList({ items, columns = 2 }) {
    return (
        <Descriptions className="cs-description-list" layout="horizontal" align="plain" column={columns}
            data={items.filter(Boolean).map(item => ({ key: item.label, value: item.value ?? '—' }))} />
    );
}

export function confirmAction({ title, content, okText, danger = true, onOk }) {
    Modal.confirm({
        title,
        content,
        okText: okText || '确定',
        cancelText: '取消',
        okButtonProps: danger ? { type: 'danger' } : undefined,
        onOk
    });
}

export function useConfirmDelete() {
    return (title, onOk) => confirmAction({ title, content: null, onOk });
}

/**
 * Drawer：Semi Design v2 以 SideSheet 提供抽屉，这里保留 Drawer 名称，
 * 统一 title/visible/onCancel/width/footer 的调用方式，页面无需关心底层组件。
 */
export function Drawer({ title, visible, onCancel, width = 560, footer, children, placement = 'right', bodyStyle, className, style, ...rest }) {
    return (
        <SideSheet
            title={title}
            visible={visible}
            onCancel={onCancel}
            width={width}
            footer={footer}
            placement={placement}
            bodyStyle={bodyStyle}
            className={`cs-drawer ${className || ''}`}
            style={style}
            {...rest}
        >
            {children}
        </SideSheet>
    );
}
