package openai

import (
	"context"
	"net/http"
	"strings"

	"cyberstrike-ai/internal/config"
)

// sessionHeaderPlaceholder 是自定义请求头值里代表「当前对话 ID」的占位符。
// 写入方式（配置文件或设置页）示例：
//
//	headers:
//	  x-opencode-session: %session_id%
//
// 实际下发时会替换为当前对话的 conversation id。
// 注意：不要使用 {{...}} 语法，index.html 是 Go 模板，会被当作模板函数解析。
const sessionHeaderPlaceholder = "%session_id%"

type sessionIDContextKey struct{}

// WithSessionID 把当前对话 ID 注入 ctx。handler 在任务开始时注入一次，
// 后续 Eino ChatModel 与内置 Client 发出的 HTTP 请求都能在请求上下文里读到。
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		return nil
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDContextKey{}, id)
}

// SessionIDFromContext 读取 ctx 中的对话 ID，未注入时返回空串。
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(sessionIDContextKey{}).(string)
	return v
}

// renderHeaderValue 把值里的 %session_id% 替换为当前对话 ID。
// 第二个返回值为 false 表示该头依赖会话 ID 但上下文里取不到，应当跳过下发，
// 避免把字面量 "%session_id%" 发给上游造成脏数据。
func renderHeaderValue(value, sessionID string) (string, bool) {
	if !strings.Contains(value, sessionHeaderPlaceholder) {
		return value, true
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return "", false
	}
	return strings.ReplaceAll(value, sessionHeaderPlaceholder, id), true
}

// normalizedCustomHeaders 返回规范化后的自定义请求头：
// 去掉名称两端空白，丢弃空名称/空值，便于安全地写入 http.Header。
func normalizedCustomHeaders(cfg *config.OpenAIConfig) map[string]string {
	if cfg == nil || len(cfg.Headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(cfg.Headers))
	for name, value := range cfg.Headers {
		name = strings.TrimSpace(name)
		if name == "" || strings.TrimSpace(value) == "" {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyCustomHeaders 把渠道配置的自定义请求头写入请求。
// 同名的内置头（Authorization、Content-Type 等）会被配置值覆盖，
// 因此只接受私有鉴权头的第三方 OpenAI 兼容网关也能正常调用。
// 值中的 %session_id% 会替换为请求上下文里的当前对话 ID。
func applyCustomHeaders(req *http.Request, cfg *config.OpenAIConfig) {
	if req == nil || req.Header == nil {
		return
	}
	headers := normalizedCustomHeaders(cfg)
	if len(headers) == 0 {
		return
	}
	sessionID := SessionIDFromContext(req.Context())
	for name, value := range headers {
		rendered, ok := renderHeaderValue(value, sessionID)
		if !ok {
			continue
		}
		req.Header.Set(name, rendered)
	}
}

// headersRoundTripper 为经过 Eino ChatModel 的请求注入渠道自定义请求头。
type headersRoundTripper struct {
	base http.RoundTripper
	cfg  *config.OpenAIConfig
}

func (rt *headersRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := http.DefaultTransport
	if rt != nil && rt.base != nil {
		base = rt.base
	}
	if rt == nil || req == nil {
		return base.RoundTrip(req)
	}
	applyCustomHeaders(req, rt.cfg)
	return base.RoundTrip(req)
}
