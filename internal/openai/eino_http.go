package openai

import (
	"net/http"

	"cyberstrike-ai/internal/config"
)

// NewEinoHTTPClient adds OpenAI-compatible request fixes, SSE error decoding,
// and successful-stream heartbeat sanitation.
// Claude channels use Eino's native agenticclaude model and never enter here.
func NewEinoHTTPClient(cfg *config.OpenAIConfig, base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	cloned := *base
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	transport = &reasoningToolChoiceCompatRoundTripper{base: transport, cfg: cfg}
	transport = &einoSSEErrorRoundTripper{base: transport}
	transport = &einoSSESanitizingRoundTripper{base: transport}
	cloned.Transport = transport
	return &cloned
}
