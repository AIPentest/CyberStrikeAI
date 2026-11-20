import { Button, Tag, Typography } from '@douyinfe/semi-ui';
import { formatDateTime } from '../../lib/format';
import { statusText } from './labels';

const { Text, Paragraph } = Typography;
const PREVIEW_LIMIT = 6;

export default function GraphBrief({ task, t, onIntent, onGoIntents }) {
    const steps = task.steps || [];
    const titles = new Map(steps.map(step => [step.id, step.title || step.id]));
    const completed = steps.filter(step => step.status === 'completed').length;
    const summary = task.summary?.trim();

    return (
        <details className="assessment-graph-brief">
            <summary>
                <Text strong>目标拆解与阶段总结</Text>
                <Text type="tertiary" size="small" className="assessment-graph-brief-counts">
                    {steps.length ? `${steps.length} 个意图 · ${completed} 个已完成` : '暂无意图'}
                </Text>
            </summary>
            <div className="assessment-graph-brief-grid">
                <section className="assessment-graph-brief-content">
                    <Text strong>{t('deepAssessments.objective', null, '目标')}</Text>
                    <Paragraph className="assessment-pre">{task.objective || task.name || '—'}</Paragraph>
                    {steps.length ? (
                        <>
                            <ul className="assessment-graph-intents" aria-label="目标拆解">
                                {steps.slice(-PREVIEW_LIMIT).map(step => {
                                    const dependencies = (step.depends_on || []).map(id => titles.get(id) || id);
                                    return (
                                        <li key={step.id}>
                                            <div className="assessment-graph-intent-head">
                                                <Button size="small" theme="borderless" onClick={() => onIntent(step.id)} title={titles.get(step.id)}>
                                                    {titles.get(step.id)}
                                                </Button>
                                                {step.status ? <Tag size="small">{statusText(t, step.status)}</Tag> : null}
                                            </div>
                                            {dependencies.length ? (
                                                <Text size="small" type="tertiary">
                                                    上游依赖 · {dependencies.slice(0, 2).join('、')}
                                                    {dependencies.length > 2 ? ` 等 ${dependencies.length} 项` : ''}
                                                </Text>
                                            ) : null}
                                        </li>
                                    );
                                })}
                            </ul>
                            {steps.length > PREVIEW_LIMIT ? (
                                <Button size="small" theme="borderless" onClick={onGoIntents}>查看全部意图</Button>
                            ) : null}
                        </>
                    ) : (
                        <Text type="tertiary" size="small">已有计划中的意图会显示在这里。</Text>
                    )}
                </section>
                <section className="assessment-graph-brief-content">
                    <Text strong>阶段总结</Text>
                    <Paragraph className="assessment-pre assessment-graph-summary">
                        {summary || '当前还没有保存的阶段总结。'}
                    </Paragraph>
                    {summary && task.updated_at ? (
                        <Text size="small" type="tertiary">测评更新于 {formatDateTime(task.updated_at)}</Text>
                    ) : null}
                </section>
            </div>
        </details>
    );
}
