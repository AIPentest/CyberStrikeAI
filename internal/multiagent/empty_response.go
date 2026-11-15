package multiagent

import (
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

const defaultEmptyResponseContinueMaxAttempts = 5

// IsEmptyResponseResult 判断 Run 是否以「未捕获助手正文」占位结束（非真实用户可见回复）。
func IsEmptyResponseResult(result *RunResult) bool {
	if result == nil {
		return false
	}
	return isEmptyResponseText(result.Response)
}

func isEmptyResponseText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Contains(s, "no assistant text was captured") ||
		strings.Contains(s, "未捕获到助手文本输出")
}

// HasResumeTrace 轨迹非空，续跑才有上下文可恢复。
func HasResumeTrace(result *RunResult) bool {
	if result == nil {
		return false
	}
	s := strings.TrimSpace(result.LastAgentTraceInput)
	return s != "" && s != "[]" && s != "null"
}

// EmptyResponseContinueMaxAttemptsFromConfig 无助手正文时 Handler 层退避续跑上限；0=默认 5。
func EmptyResponseContinueMaxAttemptsFromConfig(mw *config.AgentTuningConfig) int {
	if mw != nil && mw.EmptyResponseContinueMaxAttempts > 0 {
		return mw.EmptyResponseContinueMaxAttempts
	}
	return defaultEmptyResponseContinueMaxAttempts
}

// EmptyResponseContinueBackoff 与 run_retry 相同指数退避（2s, 4s, 8s… capped）。
func EmptyResponseContinueBackoff(attempt int, mw *config.AgentTuningConfig) time.Duration {
	maxBackoff := defaultRunRetryMaxBackoff
	if mw != nil && mw.RunRetryMaxBackoffSec > 0 {
		maxBackoff = time.Duration(mw.RunRetryMaxBackoffSec) * time.Second
	}
	return transientRetryBackoff(attempt, maxBackoff)
}

// FormatEmptyResponseContinueUserMessage 系统自动续跑时注入的 user 轮次（不写入 messages 表气泡）。
func FormatEmptyResponseContinueUserMessage() string {
	return strings.TrimSpace(`【系统自动续跑 / Auto resume】
上一轮 Agent 会话未产出可见助手正文（可能流式中断或仅完成工具调用）。请基于已有轨迹与工具结果继续推进，并给出阶段性总结；勿重复已完成步骤。`)
}
