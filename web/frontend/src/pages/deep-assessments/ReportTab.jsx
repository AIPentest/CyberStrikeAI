/** 报告：Markdown 预览、复制与下载。 */
import { useState } from 'react';
import { Button, Empty, Space } from '@douyinfe/semi-ui';
import { api, downloadFile } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import {
    SectionCard, ErrorBanner, MarkdownView, CopyButton, toastError, toastSuccess
} from '../../components/ui';

export default function ReportTab({ id, task, reloadToken }) {
    const t = useT();
    const [downloading, setDownloading] = useState(false);
    const resource = useResource(
        () => api.get(`/deep-assessments/${id}/report`),
        [id, reloadToken]
    );
    const content = typeof resource.data === 'string' ? resource.data : '';
    const fileLabel = String(task?.name || id).replace(/[\\/:*?"<>|\s]+/g, '_').slice(0, 60) || id;

    const download = async () => {
        setDownloading(true);
        try {
            const name = await downloadFile(`/deep-assessments/${id}/report`, {
                filename: `assessment-${fileLabel}.md`
            });
            toastSuccess(`已下载 ${name}`);
        } catch (error) {
            toastError(error, '下载失败');
        } finally {
            setDownloading(false);
        }
    };

    return (
        <div className="assessment-tab">
            <SectionCard
                title={t('deepAssessments.reportTitle', null, '测评报告')}
                description="报告按当前已保存状态生成，包含验收矩阵、证据对照与运行记录。"
                extra={(
                    <Space>
                        <CopyButton text={content} label="复制 Markdown" />
                        <Button size="small" onClick={() => resource.reload()} loading={resource.loading}>刷新</Button>
                        <Button size="small" theme="solid" type="primary" onClick={download} loading={downloading} disabled={!content}>
                            {t('deepAssessments.report', null, '下载报告')}
                        </Button>
                    </Space>
                )}
            >
                <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
                {content ? (
                    <div className="assessment-report">
                        <MarkdownView content={content} />
                    </div>
                ) : (
                    !resource.loading ? <Empty title="报告为空" description="开始测评并保存进展后可生成报告。" style={{ padding: 40 }} /> : null
                )}
            </SectionCard>
        </div>
    );
}
