package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// normalizeEinoUsagePayload bridges DeepSeek's cache counters to the OpenAI
// details understood by Eino. Standard provider fields always take precedence.
func normalizeEinoUsagePayload(payload []byte) []byte {
	if !bytes.Contains(payload, []byte(`"prompt_cache_hit_tokens"`)) {
		return payload
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(payload, &root) != nil {
		return payload
	}
	var usage map[string]json.RawMessage
	if json.Unmarshal(root["usage"], &usage) != nil {
		return payload
	}
	hit, ok := usage["prompt_cache_hit_tokens"]
	if !ok {
		return payload
	}
	var count int64
	if json.Unmarshal(hit, &count) != nil || count < 0 {
		return payload
	}
	details := map[string]json.RawMessage{}
	if raw := usage["prompt_tokens_details"]; len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &details) != nil {
			return payload
		}
	}
	if _, exists := details["cached_tokens"]; exists {
		return payload
	}
	details["cached_tokens"] = hit
	usage["prompt_tokens_details"], _ = json.Marshal(details)
	root["usage"], _ = json.Marshal(usage)
	out, err := json.Marshal(root)
	if err != nil {
		return payload
	}
	return out
}

// JSON responses are normalized lazily so RoundTrip retains its normal timing.
// Streaming responses are normalized one data line at a time by the SSE reader.
type einoUsageNormalizingRoundTripper struct{ base http.RoundTripper }

func (rt *einoUsageNormalizingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err == nil && resp != nil && resp.Body != nil && resp.StatusCode == http.StatusOK && strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		resp.Body = &einoUsageJSONBody{upstream: resp.Body}
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
	}
	return resp, err
}

type einoUsageJSONBody struct {
	upstream io.ReadCloser
	reader   *bytes.Reader
	err      error
}

func (b *einoUsageJSONBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.reader == nil {
		raw, err := io.ReadAll(b.upstream)
		b.err = err
		if err == nil {
			raw = normalizeEinoUsagePayload(raw)
		}
		b.reader = bytes.NewReader(raw)
	}
	n, err := b.reader.Read(p)
	if err == io.EOF && b.err != nil {
		return n, b.err
	}
	return n, err
}
func (b *einoUsageJSONBody) Close() error { return b.upstream.Close() }
