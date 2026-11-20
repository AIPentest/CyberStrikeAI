package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestUpdateConfigEditsOneAIChannel(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	initial := `ai:
  default_channel: primary
  channels:
    primary:
      name: Primary
      model: model-one
      api_key: fixture-primary
    secondary:
      name: Secondary
      model: model-two
      api_key: fixture-secondary
      headers:
        X-Gateway-Token: fixture-header
      reasoning:
        extra_request_fields:
          example_option: true
`
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &ConfigHandler{configPath: path, config: cfg, logger: zap.NewNop()}
	router := gin.New()
	router.PUT("/config", h.UpdateConfig)
	update := func(body string, status int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("status = %d, want %d: %s", res.Code, status, res.Body.String())
		}
	}

	update(`{"ai_channel":{"channel_id":"secondary","name":"Local","model":"manual-model","context_window":65536}}`, http.StatusOK)
	if cfg.AI.DefaultChannel != "primary" || cfg.OpenAI.Model != "model-one" {
		t.Fatal("editing another channel changed the default")
	}
	update(`{"ai_channel":{"channel_id":"primary","model":"updated-primary"}}`, http.StatusOK)
	update(`{"ai_channel":{"channel_id":"secondary","make_default":true,"api_key":"********"}}`, http.StatusOK)
	update(`{"ai_channel":{"channel_id":"secondary","max_total_tokens":-1}}`, http.StatusBadRequest)
	update(`{"ai_channel":{"channel_id":"missing","model":"unused"}}`, http.StatusBadRequest)

	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	secondary := saved.AI.Channels["secondary"]
	if saved.AI.DefaultChannel != "secondary" || saved.OpenAI.Model != "manual-model" || secondary.Name != "Local" || secondary.ContextWindow != 65536 {
		t.Fatal("selected channel edits or new default did not survive reload")
	}
	if saved.OpenAI.APIKey != "fixture-secondary" || secondary.Headers["X-Gateway-Token"] != "fixture-header" ||
		secondary.Reasoning.ExtraRequestFields["example_option"] != true {
		t.Fatal("editing the channel dropped its saved configuration")
	}
	if primary := saved.AI.Channels["primary"]; primary.Model != "updated-primary" || primary.APIKey != "fixture-primary" {
		t.Fatal("editing the other channel overwrote primary")
	}
	if len(saved.AI.Channels) != 2 {
		t.Fatal("editing channels changed the number of channels")
	}

	h.configPath = filepath.Join(t.TempDir(), "missing.yaml")
	update(`{"ai_channel":{"channel_id":"secondary","model":"unsaved-model"}}`, http.StatusInternalServerError)
	if cfg.OpenAI.Model != "manual-model" || cfg.AI.Channels["secondary"].Model != "manual-model" {
		t.Fatal("a failed save left the edited model in memory")
	}
}
