package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func setupTestRouter() (*gin.Engine, *ExternalMCPHandler, string) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// 创建临时配置文件
	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		panic(err)
	}
	tmpFile.WriteString("server:\n  host: 0.0.0.0\n  port: 8080\n")
	tmpFile.Close()
	configPath := tmpFile.Name()

	logger := zap.NewNop()
	manager := mcp.NewExternalMCPManager(logger)
	cfg := &config.Config{
		ExternalMCP: config.ExternalMCPConfig{
			Servers: make(map[string]config.ExternalMCPServerConfig),
		},
	}

	handler := NewExternalMCPHandler(manager, cfg, configPath, logger)

	api := router.Group("/api")
	api.GET("/external-mcp", handler.GetExternalMCPs)
	api.GET("/external-mcp/stats", handler.GetExternalMCPStats)
	api.GET("/external-mcp/:name", handler.GetExternalMCP)
	api.PUT("/external-mcp/:name", handler.AddOrUpdateExternalMCP)
	api.DELETE("/external-mcp/:name", handler.DeleteExternalMCP)
	api.POST("/external-mcp/:name/start", handler.StartExternalMCP)
	api.POST("/external-mcp/:name/stop", handler.StopExternalMCP)

	return router, handler, configPath
}

func cleanupTestConfig(configPath string) {
	os.Remove(configPath)
	os.Remove(configPath + ".backup")
}

func TestExternalMCPConnectionReceivesExpandedSnapshot(t *testing.T) {
	t.Setenv("CSAI_TEST_MCP_COMMAND", "not-executed")
	t.Setenv("CSAI_TEST_MCP_VALUE", "expanded-test-value")
	router, h, configPath := setupTestRouter()
	defer cleanupTestConfig(configPath)
	body, err := json.Marshal(AddOrUpdateExternalMCPRequest{Config: config.ExternalMCPServerConfig{
		Command: "${CSAI_TEST_MCP_COMMAND}", Args: []string{"${CSAI_TEST_MCP_VALUE}"}, Disabled: true,
		Env: map[string]string{"VALUE": "${CSAI_TEST_MCP_VALUE}"}, Headers: map[string]string{"X-Value": "${CSAI_TEST_MCP_VALUE}"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/external-mcp/test-env", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	cfg := h.manager.GetConfigs()["test-env"]
	if cfg.Command != "not-executed" || len(cfg.Args) != 1 || cfg.Args[0] != "expanded-test-value" || cfg.Env["VALUE"] != "expanded-test-value" || cfg.Headers["X-Value"] != "expanded-test-value" {
		t.Fatalf("connection received unexpanded snapshot: %#v", cfg)
	}
	if !cfg.Disabled || cfg.ExternalMCPEnable {
		t.Fatal("disabled connection was activated")
	}
}

func TestExternalMCPHandler_AddOrUpdateExternalMCP_Stdio(t *testing.T) {
	router, _, configPath := setupTestRouter()
	defer cleanupTestConfig(configPath)

	// 测试添加stdio模式的配置（官方格式：有 command 时 type 可省略）
	configJSON := `{
		"command": "python3",
		"args": ["/path/to/script.py", "--server", "http://example.com"],
		"description": "Test stdio MCP",
		"timeout": 300,
		"external_mcp_enable": true
	}`

	var configObj config.ExternalMCPServerConfig
	if err := json.Unmarshal([]byte(configJSON), &configObj); err != nil {
		t.Fatalf("解析配置JSON失败: %v", err)
	}

	reqBody := AddOrUpdateExternalMCPRequest{
		Config: configObj,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/api/external-mcp/test-stdio", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w.Code, w.Body.String())
	}

	// 验证配置已添加
	req2 := httptest.NewRequest("GET", "/api/external-mcp/test-stdio", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w2.Code, w2.Body.String())
	}

	var response ExternalMCPResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if response.Config.Command != "python3" {
		t.Errorf("期望command为python3，实际%s", response.Config.Command)
	}
	if len(response.Config.Args) != 3 {
		t.Errorf("期望args长度为3，实际%d", len(response.Config.Args))
	}
	if response.Config.Description != "Test stdio MCP" {
		t.Errorf("期望description为'Test stdio MCP'，实际%s", response.Config.Description)
	}
	if response.Config.Timeout != 300 {
		t.Errorf("期望timeout为300，实际%d", response.Config.Timeout)
	}
}

func TestExternalMCPHandler_AddOrUpdateExternalMCP_HTTP(t *testing.T) {
	router, _, configPath := setupTestRouter()
	defer cleanupTestConfig(configPath)

	// 测试添加HTTP模式的配置（使用官方 type 字段）
	configJSON := `{
		"type": "http",
		"url": "http://127.0.0.1:8081/mcp",
		"external_mcp_enable": true
	}`

	var configObj config.ExternalMCPServerConfig
	if err := json.Unmarshal([]byte(configJSON), &configObj); err != nil {
		t.Fatalf("解析配置JSON失败: %v", err)
	}

	reqBody := AddOrUpdateExternalMCPRequest{
		Config: configObj,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/api/external-mcp/test-http", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w.Code, w.Body.String())
	}

	// 验证配置已添加
	req2 := httptest.NewRequest("GET", "/api/external-mcp/test-http", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w2.Code, w2.Body.String())
	}

	var response ExternalMCPResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if response.Config.Type != "http" {
		t.Errorf("期望type为http，实际%s", response.Config.Type)
	}
	if response.Config.URL != "http://127.0.0.1:8081/mcp" {
		t.Errorf("期望url为'http://127.0.0.1:8081/mcp'，实际%s", response.Config.URL)
	}
}

func TestExternalMCPHandler_AddOrUpdateExternalMCP_InvalidConfig(t *testing.T) {
	router, _, configPath := setupTestRouter()
	defer cleanupTestConfig(configPath)

	testCases := []struct {
		name        string
		configJSON  string
		expectedErr string
	}{
		{
			name:        "缺少command和url",
			configJSON:  `{"external_mcp_enable": true}`,
			expectedErr: "must specify command (stdio mode) or url + type (http/sse mode)",
		},
		{
			name:        "stdio模式缺少command",
			configJSON:  `{"args": ["test"], "external_mcp_enable": true}`,
			expectedErr: "stdio mode requires command",
		},
		{
			name:        "http模式缺少url",
			configJSON:  `{"type": "http", "external_mcp_enable": true}`,
			expectedErr: "HTTP mode requires url",
		},
		{
			name:        "无效的type",
			configJSON:  `{"type": "invalid", "external_mcp_enable": true}`,
			expectedErr: "unsupported transport mode",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var configObj config.ExternalMCPServerConfig
			if err := json.Unmarshal([]byte(tc.configJSON), &configObj); err != nil {
				t.Fatalf("解析配置JSON失败: %v", err)
			}

			reqBody := AddOrUpdateExternalMCPRequest{
				Config: configObj,
			}

			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("PUT", "/api/external-mcp/test-invalid", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("期望状态码400，实际%d: %s", w.Code, w.Body.String())
			}

			var response map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}

			errorMsg := response["error"].(string)
			// 对于stdio模式缺少command的情况，错误信息可能略有不同
			if tc.name == "stdio模式缺少command" {
				if !strings.Contains(errorMsg, "stdio") && !strings.Contains(errorMsg, "command") {
					t.Errorf("期望错误信息包含'stdio'或'command'，实际'%s'", errorMsg)
				}
			} else if !strings.Contains(errorMsg, tc.expectedErr) {
				t.Errorf("期望错误信息包含'%s'，实际'%s'", tc.expectedErr, errorMsg)
			}
		})
	}
}

func TestExternalMCPHandler_DeleteExternalMCP(t *testing.T) {
	router, handler, configPath := setupTestRouter()
	defer cleanupTestConfig(configPath)

	// 先添加一个配置
	configObj := config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: true,
	}
	handler.manager.AddOrUpdateConfig("test-delete", configObj)

	// 删除配置
	req := httptest.NewRequest("DELETE", "/api/external-mcp/test-delete", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w.Code, w.Body.String())
	}

	// 验证配置已删除
	req2 := httptest.NewRequest("GET", "/api/external-mcp/test-delete", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotFound {
		t.Errorf("期望状态码404，实际%d: %s", w2.Code, w2.Body.String())
	}
}

func TestExternalMCPStatusError(t *testing.T) {
	manager := mcp.NewExternalMCPManager(zap.NewNop())
	if got := externalMCPStatusError(manager, "x", "connected"); got != "" {
		t.Fatalf("connected status should not return error, got %q", got)
	}
	if got := externalMCPStatusError(manager, "x", "connecting"); got != "" {
		t.Fatalf("connecting status should not return error, got %q", got)
	}
}

func TestExternalMCPHandler_GetExternalMCPs(t *testing.T) {
	router, handler, _ := setupTestRouter()

	// 添加多个配置
	handler.manager.AddOrUpdateConfig("test1", config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: true,
	})
	handler.manager.AddOrUpdateConfig("test2", config.ExternalMCPServerConfig{
		URL:               "http://127.0.0.1:8081/mcp",
		ExternalMCPEnable: false,
	})

	req := httptest.NewRequest("GET", "/api/external-mcp", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	servers := response["servers"].(map[string]interface{})
	if len(servers) != 2 {
		t.Errorf("期望2个服务器，实际%d", len(servers))
	}
	if _, ok := servers["test1"]; !ok {
		t.Error("期望包含test1")
	}
	if _, ok := servers["test2"]; !ok {
		t.Error("期望包含test2")
	}

	stats := response["stats"].(map[string]interface{})
	if int(stats["total"].(float64)) != 2 {
		t.Errorf("期望总数为2，实际%d", int(stats["total"].(float64)))
	}
}

func TestExternalMCPHandler_GetExternalMCPStats(t *testing.T) {
	router, handler, _ := setupTestRouter()

	// 添加配置
	handler.manager.AddOrUpdateConfig("enabled1", config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: true,
	})
	handler.manager.AddOrUpdateConfig("enabled2", config.ExternalMCPServerConfig{
		URL:               "http://127.0.0.1:8081/mcp",
		ExternalMCPEnable: true,
	})
	handler.manager.AddOrUpdateConfig("disabled1", config.ExternalMCPServerConfig{
		Command: "python3",
	})

	req := httptest.NewRequest("GET", "/api/external-mcp/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w.Code, w.Body.String())
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if int(stats["total"].(float64)) != 3 {
		t.Errorf("期望总数为3，实际%d", int(stats["total"].(float64)))
	}
	if int(stats["enabled"].(float64)) != 2 {
		t.Errorf("期望启用数为2，实际%d", int(stats["enabled"].(float64)))
	}
	if int(stats["disabled"].(float64)) != 1 {
		t.Errorf("期望停用数为1，实际%d", int(stats["disabled"].(float64)))
	}
}

func TestExternalMCPHandler_StartStopExternalMCP(t *testing.T) {
	router, handler, configPath := setupTestRouter()
	defer cleanupTestConfig(configPath)

	// 添加一个禁用的配置
	handler.manager.AddOrUpdateConfig("test-start-stop", config.ExternalMCPServerConfig{
		Command: "python3",
	})

	// 测试启动（可能会失败，因为没有真实的服务器）
	req := httptest.NewRequest("POST", "/api/external-mcp/test-start-stop/start", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 启动可能会失败，但应该返回合理的状态码
	if w.Code != http.StatusOK {
		// 如果启动失败，应该是400或500
		if w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError {
			t.Errorf("期望状态码200/400/500，实际%d: %s", w.Code, w.Body.String())
		}
	}

	// 测试停止
	req2 := httptest.NewRequest("POST", "/api/external-mcp/test-start-stop/stop", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("期望状态码200，实际%d: %s", w2.Code, w2.Body.String())
	}
}

func TestExternalMCPHandler_GetExternalMCP_NotFound(t *testing.T) {
	router, _, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/external-mcp/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("期望状态码404，实际%d: %s", w.Code, w.Body.String())
	}
}

func TestExternalMCPHandler_DeleteExternalMCP_NotFound(t *testing.T) {
	router, _, configPath := setupTestRouter()
	defer cleanupTestConfig(configPath)

	req := httptest.NewRequest("DELETE", "/api/external-mcp/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 删除不存在的配置可能返回200（幂等操作）或404，都是合理的
	if w.Code != http.StatusNotFound && w.Code != http.StatusOK {
		t.Errorf("期望状态码404或200，实际%d: %s", w.Code, w.Body.String())
	}
}

func TestExternalMCPHandler_AddOrUpdateExternalMCP_EmptyName(t *testing.T) {
	router, _, _ := setupTestRouter()

	configObj := config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: true,
	}

	reqBody := AddOrUpdateExternalMCPRequest{
		Config: configObj,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/api/external-mcp/", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// 空名称应该返回404或400
	if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Errorf("期望状态码404或400，实际%d: %s", w.Code, w.Body.String())
	}
}

func TestExternalMCPHandler_AddOrUpdateExternalMCP_InvalidJSON(t *testing.T) {
	router, _, _ := setupTestRouter()

	// 发送无效的JSON
	body := []byte(`{"config": invalid json}`)
	req := httptest.NewRequest("PUT", "/api/external-mcp/test", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望状态码400，实际%d: %s", w.Code, w.Body.String())
	}
}

func TestExternalMCPHandler_UpdateExistingConfig(t *testing.T) {
	router, handler, configPath := setupTestRouter()
	defer cleanupTestConfig(configPath)

	// 先添加配置
	config1 := config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: true,
	}
	handler.manager.AddOrUpdateConfig("test-update", config1)

	// 更新配置
	config2 := config.ExternalMCPServerConfig{
		URL:               "http://127.0.0.1:8081/mcp",
		ExternalMCPEnable: true,
	}

	reqBody := AddOrUpdateExternalMCPRequest{
		Config: config2,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/api/external-mcp/test-update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w.Code, w.Body.String())
	}

	// 验证配置已更新
	req2 := httptest.NewRequest("GET", "/api/external-mcp/test-update", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("期望状态码200，实际%d: %s", w2.Code, w2.Body.String())
	}

	var response ExternalMCPResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if response.Config.URL != "http://127.0.0.1:8081/mcp" {
		t.Errorf("期望url为'http://127.0.0.1:8081/mcp'，实际%s", response.Config.URL)
	}
	if response.Config.Command != "" {
		t.Errorf("期望command为空，实际%s", response.Config.Command)
	}
}
