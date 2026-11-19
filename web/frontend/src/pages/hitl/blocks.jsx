/** HITL 载荷上下文区块：用户消息 / 思考 / 推理 / 计划 / 上次执行结果。 */
import { contextBlocks, executionResult } from './utils';

export default function ContextBlocks({ payload }) {
    const blocks = contextBlocks(payload);
    const exec = executionResult(payload);
    if (!blocks.length && !exec) return null;
    return (
        <div className="hitl-context-blocks">
            {blocks.map(block => (
                <div className="hitl-context-block" key={block.key}>
                    <span className="hitl-context-label">{block.label}</span>
                    <pre className="hitl-context-text">{String(block.value)}</pre>
                </div>
            ))}
            {exec && (
                <div className={`hitl-context-block ${exec.success ? 'hitl-state-success' : 'hitl-state-failed'}`}>
                    <span className="hitl-context-label">{`执行结果（${exec.success ? '成功' : '失败'}）`}</span>
                    <pre className="hitl-context-text">{exec.text}</pre>
                </div>
            )}
        </div>
    );
}
