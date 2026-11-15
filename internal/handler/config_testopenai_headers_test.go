package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"cyberstrike-ai/internal/config"
)

func TestFirstOMLXConnectionProbeUsesDetectedLocalOutputLimit(t *testing.T) {
	var completions int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"local-model","owned_by":"omlx","max_model_len":65536}]}`))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		completions++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["max_tokens"] != float64(5) || payload["max_completion_tokens"] != nil {
			t.Errorf("first probe missed local output cap: %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fixture","model":"local-model","choices":[{"message":{"role":"assistant","content":"Hi"}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	h := &ConfigHandler{config: &config.Config{}, logger: zap.NewNop()}
	body, _ := json.Marshal(map[string]any{"provider": "openai_compatible", "base_url": srv.URL + "/v1", "api_key": "fixture", "model": "local-model"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/config/test-openai", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	h.TestOpenAI(c)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["success"] != true || result["context_backend"] != "omlx" || result["context_profile"] != "local_auto" || completions != 1 {
		t.Fatalf("unexpected probe result: %s, completions=%d", w.Body.String(), completions)
	}
}

// TestTestOpenAIForwardsCustomHeaders 校验「测试连接」与真实对话一样会带上渠道自定义请求头：
// 静态头原样发送，%session_id% 用探测值填充，Authorization 仍按 api_key 生成。
func TestTestOpenAIForwardsCustomHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotSession, gotFlag, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = r.Header.Get("X-Opencode-Session")
		gotFlag = r.Header.Get("X-Custom-Flag")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","model":"test-model","choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer srv.Close()

	h := &ConfigHandler{config: &config.Config{}, logger: zap.NewNop()}

	body := `{"provider":"openai","base_url":"` + srv.URL + `/v1","api_key":"test-key","model":"test-model",` +
		`"headers":{"X-Opencode-Session":"%session_id%","X-Custom-Flag":"hello"}}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/config/test-openai", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.TestOpenAI(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != true {
		t.Fatalf("probe failed: %s", w.Body.String())
	}
	if gotSession != "connect-test" {
		t.Fatalf("X-Opencode-Session = %q, want connect-test", gotSession)
	}
	if gotFlag != "hello" {
		t.Fatalf("X-Custom-Flag = %q, want hello", gotFlag)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

// TestTestOpenAIWithoutHeadersKeepsBuiltinAuth 确认未配置 headers 时行为不变。
func TestTestOpenAIWithoutHeadersKeepsBuiltinAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"m","choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer srv.Close()

	h := &ConfigHandler{config: &config.Config{}, logger: zap.NewNop()}
	body := `{"provider":"openai","base_url":"` + srv.URL + `/v1","api_key":"plain-key","model":"m"}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/config/test-openai", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.TestOpenAI(c)

	if gotAuth != "Bearer plain-key" {
		t.Fatalf("Authorization = %q, want Bearer plain-key", gotAuth)
	}
}
