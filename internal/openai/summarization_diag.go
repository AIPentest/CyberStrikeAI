package openai

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

// SummarizationRequestHeader marks chat/completion requests issued by Eino summarization
// middleware (via model.WithExtraHeader). The diagnostic transport logs empty-choices bodies
// only for these requests so main-agent traffic stays quiet.
const SummarizationRequestHeader = "X-CyberStrike-Summarization"

const summarizationDiagBodyMaxBytes = 8192

// AttachSummarizationDiagTransport wraps client.Transport to log raw API bodies when
// summarization receives HTTP 200 with an empty choices array.
func AttachSummarizationDiagTransport(client *http.Client, logger *zap.Logger) {
	if client == nil || logger == nil {
		return
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &summarizationDiagRoundTripper{base: base, logger: logger}
}

type summarizationDiagRoundTripper struct {
	base   http.RoundTripper
	logger *zap.Logger
}

func (rt *summarizationDiagRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if !isSummarizationRequest(req) || !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "json") {
		return resp, err
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return resp, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))

	if rt.logger != nil && resp.StatusCode >= http.StatusBadRequest {
		rt.logger.Warn("eino summarization: API request rejected",
			zap.Int("status", resp.StatusCode),
			zap.String("request_id", responseRequestID(resp)),
			zap.Int("response_bytes", len(body)),
			zap.String("raw_body", truncateForLog(string(body), summarizationDiagBodyMaxBytes)),
		)
	} else if rt.logger != nil && summarizationResponseEmptyChoices(body) {
		rt.logger.Warn("eino summarization: API returned empty choices",
			zap.Int("status", resp.StatusCode),
			zap.String("request_id", responseRequestID(resp)),
			zap.Int("response_bytes", len(body)),
			zap.String("raw_body", truncateForLog(string(body), summarizationDiagBodyMaxBytes)),
		)
	}
	return resp, err
}

func responseRequestID(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	for _, key := range []string{"x-request-id", "request-id", "x-trace-id"} {
		if value := strings.TrimSpace(resp.Header.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func isSummarizationRequest(req *http.Request) bool {
	if req == nil {
		return false
	}
	return strings.TrimSpace(req.Header.Get(SummarizationRequestHeader)) == "1"
}

func summarizationResponseEmptyChoices(body []byte) bool {
	var parsed struct {
		Choices []any `json:"choices"`
	}
	if err := sonic.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return len(parsed.Choices) == 0
}

func truncateForLog(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "...(truncated)"
}
