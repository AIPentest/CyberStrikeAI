package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNormalizeEinoDeepSeekCacheUsage(t *testing.T) {
	for _, raw := range []string{
		`{"id":9007199254740993,"usage":{"prompt_tokens":100,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`,
		`{"usage":{"prompt_tokens_details":null,"prompt_cache_hit_tokens":80}}`,
	} {
		out := normalizeEinoUsagePayload([]byte(raw))
		var root map[string]json.RawMessage
		if err := json.Unmarshal(out, &root); err != nil {
			t.Fatal(err)
		}
		var usage struct {
			Details struct {
				Cached int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		}
		if err := json.Unmarshal(root["usage"], &usage); err != nil || usage.Details.Cached != 80 {
			t.Fatalf("normalized = %s, err = %v", out, err)
		}
		if id := root["id"]; id != nil && string(id) != "9007199254740993" {
			t.Fatalf("ID precision lost: %s", id)
		}
	}
	for _, raw := range []string{
		`[DONE]`, `{"error":{"message":"failed"}}`, `{"usage":{"prompt_cache_hit_tokens":-1}}`,
		`{"usage":{"prompt_cache_hit_tokens":80,"prompt_tokens_details":{"cached_tokens":0}}}`,
	} {
		if out := string(normalizeEinoUsagePayload([]byte(raw))); out != raw {
			t.Fatalf("unexpected change: %s", out)
		}
	}
}

func TestEinoDeepSeekCacheUsageJSONAndStream(t *testing.T) {
	payload := `{"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":80}}`
	for _, stream := range []bool{false, true} {
		contentType, body := "application/json", payload
		if stream {
			contentType, body = "text/event-stream", ": ping\ndata: "+payload+"\n\ndata: [DONE]\n"
		}
		client := NewEinoHTTPClient(nil, &http.Client{Transport: usageTestTransport(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		resp, err := client.Get("http://example.invalid/v1/chat/completions")
		if err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || !strings.Contains(string(out), `"cached_tokens":80`) {
			t.Fatalf("stream=%v body=%s err=%v", stream, out, err)
		}
		if stream && !strings.Contains(string(out), "data: [DONE]") {
			t.Fatal("lost stream terminator")
		}
	}
}

type usageTestTransport func(*http.Request) (*http.Response, error)

func (f usageTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
