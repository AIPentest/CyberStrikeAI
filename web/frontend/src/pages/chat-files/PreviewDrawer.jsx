/**
 * 附件预览 Drawer：文本走 /chat-uploads/content，图片走 /chat-uploads/download 二进制。
 */
import { useEffect, useState } from 'react';
import { Button, SideSheet, Spin, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../lib/i18n';
import { CopyButton, ErrorBanner, toastError } from '../../components/ui';
import { formatBytes } from '../../lib/format';
import { downloadFileByPath, fetchBlobUrl, getContent } from './api';

const { Text } = Typography;

const IMAGE_EXT = /\.(png|jpe?g|gif|webp|bmp|svg|ico)$/i;
const TEXT_EXT = /\.(txt|log|md|markdown|json|ya?ml|csv|tsv|xml|html?|css|jsx?|tsx?|py|go|java|c|h|cpp|sh|sql|ini|conf|toml|env|pem)$/i;

function kindOf(item) {
    const name = item?.name || '';
    if (IMAGE_EXT.test(name)) return 'image';
    if (TEXT_EXT.test(name)) return 'text';
    return 'binary';
}

export default function PreviewDrawer({ item, onClose }) {
    const t = useT();
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [text, setText] = useState('');
    const [imageUrl, setImageUrl] = useState('');
    const [mode, setMode] = useState('text');

    useEffect(() => {
        let revoked = '';
        let cancelled = false;
        if (!item) {
            setText('');
            setImageUrl('');
            setError(null);
            return () => {};
        }
        const kind = kindOf(item);
        setMode(kind);
        setLoading(true);
        setError(null);
        setText('');
        setImageUrl('');

        const load = async () => {
            try {
                if (kind === 'image') {
                    const result = await fetchBlobUrl(item.relativePath);
                    if (cancelled) {
                        URL.revokeObjectURL(result.url);
                        return;
                    }
                    revoked = result.url;
                    setImageUrl(result.url);
                } else if (kind === 'text') {
                    const payload = await getContent(item.relativePath);
                    if (cancelled) return;
                    setText(payload?.content ?? '');
                }
            } catch (err) {
                if (!cancelled) {
                    setError(err);
                    // 文本接口拒绝二进制内容时退化为提示下载
                    if (kind === 'text') setMode('binary');
                }
            } finally {
                if (!cancelled) setLoading(false);
            }
        };
        load();
        return () => {
            cancelled = true;
            if (revoked) URL.revokeObjectURL(revoked);
        };
    }, [item]);

    const download = async () => {
        if (!item) return;
        try {
            await downloadFileByPath(item.relativePath, item.name);
        } catch (err) {
            toastError(err, t('chatFilesPage.errorGeneric', null, '操作失败，请稍后重试。'));
        }
    };

    return (
        <SideSheet
            visible={Boolean(item)}
            title={item?.name || t('chatFilesPage.previewTitle', null, '文件预览')}
            width={720}
            onCancel={onClose}
            footer={(
                <Button onClick={download}>{t('chatFilesPage.download', null, '下载')}</Button>
            )}
        >
            {item && (
                <div className="chat-files-preview">
                    <div className="chat-files-preview-meta">
                        <Text type="tertiary" size="small">{formatBytes(item.size)}</Text>
                        <Text type="tertiary" size="small" className="chat-files-mono">{item.relativePath}</Text>
                        <CopyButton text={item.relativePath} label={t('chatFilesPage.copyPath', null, '复制路径')} size="small" />
                    </div>

                    <ErrorBanner error={mode === 'binary' ? null : error} />

                    {loading && <div className="chat-files-preview-loading"><Spin /></div>}

                    {!loading && mode === 'image' && imageUrl && (
                        <img className="chat-files-preview-image" src={imageUrl} alt={item.name} />
                    )}
                    {!loading && mode === 'text' && (
                        <pre className="chat-files-preview-text">{text}</pre>
                    )}
                    {!loading && mode === 'binary' && (
                        <Text type="tertiary">
                            {t('chatFilesPage.editBinaryHint', null, '图片、压缩包等二进制文件无法在此以文本方式编辑，请使用「下载」后在本地查看或处理。')}
                        </Text>
                    )}
                </div>
            )}
        </SideSheet>
    );
}
