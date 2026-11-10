package openai

import (
	"context"
	"net/http"
	"strings"

	"cyberstrike-ai/internal/config"
)

// sessionHeaderPlaceholder is the placeholder in a custom-header value that stands for the 'current conversation ID'.
// Writing method (config file or settings page) example:
//
//	headers:
//	 x-opencode-session: %session_id%
//
// At actual dispatch it is replaced with the current conversation's conversation id.
// Note: do not use {{...}} syntax; index.html is a Go template and would be parsed as a template function.
const sessionHeaderPlaceholder = "%session_id%"

type sessionIDContextKey struct{}

// WithSessionID Injects the current conversation ID into ctx. The handler injects it once at task start,
// Subsequent HTTP requests made by the Eino ChatModel and the built-in Client can all read it from the request context.
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

// SessionIDFromContext Reads the conversation ID from ctx, returning an empty string when none is injected.
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(sessionIDContextKey{}).(string)
	return v
}

// renderHeaderValue Replaces %session_id% in the value with the current conversation ID.
// A second return value of false means the header depends on the session ID but it is not available in the context, so it should be skipped,
// to avoid treating a literal "%session_id%" sent upstream, causing dirty data.
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

// normalizedCustomHeaders Returns the normalized custom request headers:
// Trims whitespace from both ends of the name and discards empty names/values, for safe writing http.Header.
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

// applyCustomHeaders Writes the channel config's custom request headers into the request.
// Built-in headers of the same name (Authorization, Content-Type, etc.) are overridden by the configured values,
// so third-party OpenAI-compatible gateways that only accept a private auth header can also be called normally.
// %session_id% in the value is replaced with the current conversation from the request context ID.
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

// headersRoundTripper Injects the channel's custom request headers into requests going through the Eino ChatModel.
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
	cloned := req.Clone(req.Context())
	applyCustomHeaders(cloned, rt.cfg)
	return base.RoundTrip(cloned)
}
