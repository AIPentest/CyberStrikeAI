import { useEffect, useRef, useState } from 'react';
import { Button, TextArea, Typography } from '@douyinfe/semi-ui';
import { Drawer, ErrorBanner } from '../../components/ui';
import { api } from '../../lib/http';
import { formatDateTime } from '../../lib/format';

export default function ProgressQuestions({ id }) {
    const [open, setOpen] = useState(false);
    const [draft, setDraft] = useState('');
    const [turns, setTurns] = useState([]);
    const [pending, setPending] = useState(false);
    const [error, setError] = useState(null);
    const request = useRef(null);
    const latest = useRef(null);

    useEffect(() => () => request.current?.abort(), []);
    useEffect(() => {
        if (open && turns.length) latest.current?.scrollIntoView({ block: 'nearest' });
    }, [open, turns]);

    const ask = async () => {
        const question = draft.trim();
        if (!question || request.current) return;
        const controller = new AbortController();
        request.current = controller;
        setPending(true);
        setError(null);
        try {
            const history = turns.slice(-3).map(turn => ({
                question: turn.question.slice(0, 600), answer: turn.answer.slice(0, 1000)
            }));
            const data = await api.post(`/deep-assessments/${encodeURIComponent(id)}/question`,
                { question, history }, { signal: controller.signal, timeout: 95000 });
            setTurns(previous => [...previous, { question, answer: data.answer, updated: data.updated_at }]);
            setDraft('');
        } catch (failure) {
            if (!controller.signal.aborted) setError(failure);
        } finally {
            request.current = null;
            setPending(false);
        }
    };

    return (
        <>
            <Button onClick={() => setOpen(true)}>测评问答</Button>
            <Drawer visible={open} title="测评问答" onCancel={() => setOpen(false)} width={540}
                footer={(
                    <form className="assessment-question-form" onSubmit={event => {
                        event.preventDefault();
                        event.currentTarget.elements.namedItem('question')?.focus();
                        ask();
                    }}>
                        <TextArea name="question" aria-label="询问测评进展" placeholder="当前在做什么？有哪些方向正在等待处理？"
                            rows={3} maxLength={2000} value={draft} onChange={setDraft} readOnly={pending} />
                        <Button htmlType="submit" theme="solid" loading={pending} disabled={pending || !draft.trim()}>发送</Button>
                    </form>
                )}
            >
                <Typography.Paragraph type="tertiary">只读问答。模型根据提问时的测评记录回答，离开本次测评页面后清空对话。</Typography.Paragraph>
                <div role="log" aria-label="问答记录" aria-live="polite">
                    {turns.map((turn, index) => (
                        <section className="assessment-question-turn" key={index} ref={index === turns.length - 1 ? latest : undefined}>
                            <Typography.Text strong>你</Typography.Text>
                            <p>{turn.question}</p>
                            <Typography.Text strong>模型</Typography.Text>
                            <p>{turn.answer}</p>
                            <Typography.Text size="small" type="tertiary">任务更新于 {formatDateTime(turn.updated)}</Typography.Text>
                        </section>
                    ))}
                    {pending ? <Typography.Text type="tertiary">正在读取进展并生成回答…</Typography.Text> : null}
                </div>
                <ErrorBanner error={error} onRetry={ask} />
            </Drawer>
        </>
    );
}
