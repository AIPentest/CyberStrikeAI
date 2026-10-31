package handler

import (
	"bytes"
	"context"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/security"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxySettingsPersistMaskAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("version: test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executor := security.NewExecutor(&config.SecurityConfig{Tools: []config.ToolConfig{{Name: "http-framework-test", Enabled: true, Command: executable, Args: []string{"-test.run=^TestProxySettingsChild$"}}}}, nil, zap.NewNop())
	h := &ConfigHandler{config: cfg, configPath: path, logger: zap.NewNop(), executor: executor}
	r := gin.New()
	r.PUT("/config", h.UpdateConfig)
	r.GET("/config", h.GetConfig)
	put := func(p config.ProxyConfig) *httptest.ResponseRecorder {
		data, _ := json.Marshal(map[string]interface{}{"proxy": p})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	policy := config.ProxyConfig{Mode: "fixed", URL: "http://user:test-secret@localhost:8080", MinSwitchInterval: 60}
	if w := put(policy); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	result, err := executor.ExecuteTool(context.Background(), "http-framework-test", map[string]interface{}{})
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "test-secret") {
		t.Fatal("saved proxy not applied to executor", err)
	}
	raw, _ := os.ReadFile(path)
	var disk config.Config
	if err := yaml.Unmarshal(raw, &disk); err != nil || disk.Proxy.URL != policy.URL {
		t.Fatal("proxy not persisted", err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/config", nil))
	if strings.Contains(w.Body.String(), "test-secret") {
		t.Fatal("secret exposed")
	}
	var response struct {
		Proxy config.ProxyConfig `json:"proxy"`
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response.Proxy.URL != maskedSecret {
		t.Fatal("not masked")
	}
	if w := put(response.Proxy); w.Code != 200 || cfg.Proxy.URL != policy.URL {
		t.Fatal("masked save lost secret")
	}
	invalid := policy
	invalid.Mode = "pool"
	invalid.Pool = "bad"
	invalid.URL = ""
	if w := put(invalid); w.Code != 400 || cfg.Proxy != policy {
		t.Fatal("invalid save mutated state")
	}
	h.configPath = filepath.Join(t.TempDir(), "missing", "settings.yaml")
	changed := policy
	changed.URL = "http://localhost:9090"
	if w := put(changed); w.Code != 500 || cfg.Proxy != policy {
		t.Fatal("failed save changed proxy config")
	}
}

func TestProxySettingsChild(t *testing.T) {
	if raw := os.Getenv("CSAI_HTTP_PROXY_CONFIG"); raw != "" {
		fmt.Print(raw)
		os.Exit(0)
	}
}
