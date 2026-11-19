package multiagent

import (
	"context"
	"cyberstrike-ai/internal/config"
	"errors"
	"math/rand/v2"
	"strings"
	"time"
)

const defaultRunRetryMaxBackoff = 30 * time.Second
const modelOutputRecoveryKey = "_cyberstrike_model_output_recovery"

func transientRetryBackoff(attempt int, maxBackoff time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 30 {
		attempt = 30
	}
	ceiling := time.Duration(1<<uint(attempt+1)) * time.Second
	if maxBackoff > 0 && ceiling > maxBackoff {
		ceiling = maxBackoff
	}
	if ceiling <= 1 {
		return ceiling
	}
	half := ceiling / 2
	return half + time.Duration(rand.Int64N(int64(ceiling-half)+1))
}

func ClientRunErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if _, detail := piModelTransportFailure(err.Error()); detail != "" {
		return detail
	}
	return err.Error()
}

func piModelTransportFailure(raw string) (code, detail string) {
	text := strings.ToLower(raw)
	switch {
	case strings.Contains(text, "und_err_headers_timeout"):
		return "model_headers_timeout", "等待模型服务响应头超时"
	case strings.Contains(text, "und_err_body_timeout"):
		return "model_stream_timeout", "模型响应流长时间未收到新数据"
	case strings.Contains(text, "stream ended without finish_reason"):
		return "model_stream_incomplete", "模型响应流提前关闭，未收到结束标记"
	default:
		return "", ""
	}
}

func ClientRunErrorFields(err error) map[string]interface{} {
	fields := map[string]interface{}{}
	if err == nil {
		return fields
	}
	kind := "runtime_error"
	text := strings.ToLower(err.Error())
	modelReason, _ := piModelTransportFailure(text)
	switch {
	case errors.Is(err, context.Canceled):
		kind = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "timeout"
	case strings.Contains(text, "context") && strings.Contains(text, "exceed"):
		kind = "context_overflow"
	case strings.Contains(text, "401") || strings.Contains(text, "403"):
		kind = "authentication"
	case modelReason != "":
		kind = "model_transient"
		fields["reasonCode"] = modelReason
	case strings.Contains(text, "429") || strings.Contains(text, "503") || strings.Contains(text, "timeout") || strings.Contains(text, "connection"):
		kind = "model_transient"
	}
	fields["errorKind"] = kind
	fields["technicalError"] = err.Error()
	return fields
}
func agentToolTimeoutMinutes(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	return cfg.Agent.ToolTimeoutMinutes
}
