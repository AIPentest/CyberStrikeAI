package openai

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/config"
)

// reasoningToolChoiceCompatRoundTripper strips thinking/reasoning fields from
// chat/completions requests that force tool_choice, which some gateways reject
// when thinking mode is enabled on the same request.
type reasoningToolChoiceCompatRoundTripper struct {
	base http.RoundTripper
	cfg  *config.OpenAIConfig
}

func (rt *reasoningToolChoiceCompatRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt == nil || rt.base == nil || req == nil || req.Body == nil {
		if rt != nil && rt.base != nil {
			return rt.base.RoundTrip(req)
		}
		return http.DefaultTransport.RoundTrip(req)
	}
	if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return rt.base.RoundTrip(req)
	}

	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}

	patched, handled, err := patchNativeOpenAIReasoning(req, body, rt.cfg)
	if err != nil {
		return nil, err
	}
	var perr error
	if !handled {
		if isDeepSeekToolChoiceCompatProfile(rt.cfg) {
			patched, perr = StripToolChoiceForThinkingMode(body)
		} else {
			patched, perr = StripReasoningIfForcedToolChoice(body)
		}
	}
	if perr != nil {
		patched = body
	}
	req.Body = io.NopCloser(bytes.NewReader(patched))
	req.ContentLength = int64(len(patched))
	req.Header.Set("Content-Length", strconv.Itoa(len(patched)))
	return rt.base.RoundTrip(req)
}

func isDeepSeekToolChoiceCompatProfile(cfg *config.OpenAIConfig) bool {
	if cfg == nil {
		return false
	}
	profile := strings.ToLower(strings.TrimSpace(cfg.Reasoning.ProfileEffective()))
	if profile == "deepseek" || profile == "deepseek_compat" {
		return true
	}
	if profile != "" && profile != "auto" {
		return false
	}
	return cfg.IsDeepSeekEndpointOrModel()
}
