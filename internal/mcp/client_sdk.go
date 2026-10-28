// Package mcp 外部 MCP 客户端 - 基于官方 go-sdk 实现，保证协议兼容性
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

const (
	clientName    = "CyberStrikeAI"
	clientVersion = "1.0.0"
)

// sdkClient 基于官方 MCP Go SDK 的外部 MCP 客户端，实现 ExternalMCPClient 接口
type sdkClient struct {
	session *mcp.ClientSession
	client  *mcp.Client
	logger  *zap.Logger
	mu      sync.RWMutex
	status  string // "disconnected", "connecting", "connected", "error"
}

// newSDKClientFromSession 用已连接成功的 session 构造（供 createSDKClient 内部使用）
func newSDKClientFromSession(session *mcp.ClientSession, client *mcp.Client, logger *zap.Logger) *sdkClient {
	return &sdkClient{
		session: session,
		client:  client,
		logger:  logger,
		status:  "connected",
	}
}

// lazySDKClient 延迟连接：Initialize() 时才调用官方 SDK 建立连接，对外实现 ExternalMCPClient
type lazySDKClient struct {
	serverCfg     config.ExternalMCPServerConfig
	logger        *zap.Logger
	sessionCancel context.CancelFunc
	inner         ExternalMCPClient // connected SDK client
	mu            sync.RWMutex
	status        string
}

func newLazySDKClient(serverCfg config.ExternalMCPServerConfig, logger *zap.Logger) *lazySDKClient {
	return &lazySDKClient{
		serverCfg: serverCfg,
		logger:    logger,
		status:    "connecting",
	}
}

func (c *lazySDKClient) setStatus(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = s
}

func (c *lazySDKClient) GetStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.inner != nil {
		return c.inner.GetStatus()
	}
	return c.status
}

func (c *lazySDKClient) IsConnected() bool {
	c.mu.RLock()
	inner := c.inner
	c.mu.RUnlock()
	if inner != nil {
		return inner.IsConnected()
	}
	return false
}

func (c *lazySDKClient) Initialize(ctx context.Context) error {
	c.mu.Lock()
	if c.inner != nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	type connectResult struct {
		inner ExternalMCPClient
		err   error
	}
	resultCh := make(chan connectResult)
	abandoned := make(chan struct{})
	go func() {
		inner, err := createSDKClient(sessionCtx, c.serverCfg, c.logger)
		select {
		case resultCh <- connectResult{inner: inner, err: err}:
		case <-abandoned:
			if inner != nil {
				_ = inner.Close()
			}
			sessionCancel()
		}
	}()

	var result connectResult
	select {
	case result = <-resultCh:
	case <-ctx.Done():
		close(abandoned)
		sessionCancel()
		c.setStatus("error")
		return ctx.Err()
	}

	if err := ctx.Err(); err != nil {
		sessionCancel()
		if result.inner != nil {
			_ = result.inner.Close()
		}
		c.setStatus("error")
		return err
	}

	if result.err != nil {
		sessionCancel()
		c.setStatus("error")
		return result.err
	}

	c.mu.Lock()
	if c.inner != nil {
		c.mu.Unlock()
		sessionCancel()
		if result.inner != nil {
			_ = result.inner.Close()
		}
		return nil
	}
	c.inner = result.inner
	c.sessionCancel = sessionCancel
	c.mu.Unlock()
	c.setStatus("connected")
	return nil
}

func (c *lazySDKClient) ListTools(ctx context.Context) ([]Tool, error) {
	c.mu.RLock()
	inner := c.inner
	c.mu.RUnlock()
	if inner == nil {
		return nil, fmt.Errorf("not connected")
	}
	return inner.ListTools(ctx)
}

func (c *lazySDKClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error) {
	c.mu.RLock()
	inner := c.inner
	c.mu.RUnlock()
	if inner == nil {
		return nil, fmt.Errorf("not connected")
	}
	return inner.CallTool(ctx, name, args)
}

func (c *lazySDKClient) Close() error {
	c.mu.Lock()
	inner := c.inner
	sessionCancel := c.sessionCancel
	c.inner = nil
	c.sessionCancel = nil
	c.mu.Unlock()
	c.setStatus("disconnected")
	if sessionCancel != nil {
		sessionCancel()
	}
	if inner != nil {
		return inner.Close()
	}
	return nil
}

// markDisconnected 在检测到传输层断连时关闭底层 session，避免 IsConnected 仍返回 true。
func (c *lazySDKClient) markDisconnected() {
	c.mu.Lock()
	inner := c.inner
	sessionCancel := c.sessionCancel
	c.inner = nil
	c.sessionCancel = nil
	c.mu.Unlock()
	if sessionCancel != nil {
		sessionCancel()
	}
	if inner != nil {
		_ = inner.Close()
	}
	c.setStatus("disconnected")
}

func (c *sdkClient) setStatus(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = s
}

func (c *sdkClient) GetStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *sdkClient) IsConnected() bool {
	return c.GetStatus() == "connected"
}

func (c *sdkClient) Initialize(ctx context.Context) error {
	// sdkClient 由 createSDKClient 在 Connect 成功后才创建，因此 Initialize 时已经连接
	// 此方法仅用于满足 ExternalMCPClient 接口，实际连接在 createSDKClient 中完成
	return nil
}

func (c *sdkClient) ListTools(ctx context.Context) ([]Tool, error) {
	if c.session == nil {
		return nil, fmt.Errorf("not connected")
	}
	res, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return sdkToolsToOur(res.Tools), nil
}

func (c *sdkClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error) {
	if c.session == nil {
		return nil, fmt.Errorf("not connected")
	}
	params := &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	}
	res, err := c.session.CallTool(ctx, params)
	if err != nil {
		return nil, err
	}
	return sdkCallToolResultToOurs(res), nil
}

func (c *sdkClient) Close() error {
	c.setStatus("disconnected")
	if c.session != nil {
		err := c.session.Close()
		c.session = nil
		return err
	}
	return nil
}

// sdkToolsToOur 将 SDK 的 []*mcp.Tool 转为我们的 []Tool
func sdkToolsToOur(tools []*mcp.Tool) []Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		schema := make(map[string]interface{})
		if t.InputSchema != nil {
			// SDK InputSchema 可能为 *jsonschema.Schema 或 map，统一转为 map
			if m, ok := t.InputSchema.(map[string]interface{}); ok {
				schema = m
			} else {
				_ = json.Unmarshal(mustJSON(t.InputSchema), &schema)
			}
		}
		desc := t.Description
		shortDesc := desc
		if t.Annotations != nil && t.Annotations.Title != "" {
			shortDesc = t.Annotations.Title
		}
		out = append(out, Tool{
			Name:             t.Name,
			Description:      desc,
			ShortDescription: shortDesc,
			InputSchema:      schema,
		})
	}
	return out
}

// sdkCallToolResultToOurs 将 SDK 的 *mcp.CallToolResult 转为我们的 *ToolResult
func sdkCallToolResultToOurs(res *mcp.CallToolResult) *ToolResult {
	if res == nil {
		return &ToolResult{Content: []Content{}}
	}
	content := sdkContentToOurs(res.Content)
	blocked, _ := res.Meta[toolGuardBlockedMetaKey].(bool)
	return &ToolResult{
		Content: content,
		IsError: res.IsError,
		Blocked: blocked,
	}
}

func sdkContentToOurs(list []mcp.Content) []Content {
	if len(list) == 0 {
		return nil
	}
	out := make([]Content, 0, len(list))
	for _, c := range list {
		switch v := c.(type) {
		case *mcp.TextContent:
			out = append(out, Content{Type: "text", Text: v.Text})
		default:
			out = append(out, Content{Type: "text", Text: fmt.Sprintf("%v", c)})
		}
	}
	return out
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// createSDKClient 根据配置创建并连接外部 MCP 客户端（使用官方 SDK），返回实现 ExternalMCPClient 的 *sdkClient
// 若连接失败返回 (nil, error)。ctx 用于连接超时与取消。
func createSDKClient(ctx context.Context, serverCfg config.ExternalMCPServerConfig, logger *zap.Logger) (ExternalMCPClient, error) {
	timeout := time.Duration(serverCfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	transport := serverCfg.GetTransportType()
	if transport == "" {
		return nil, fmt.Errorf("config lacks command or url and specifies no type/transport")
	}

	// 构造 ClientOptions：KeepAlive 心跳
	var clientOpts *mcp.ClientOptions
	if serverCfg.KeepAlive > 0 {
		clientOpts = &mcp.ClientOptions{
			KeepAlive: time.Duration(serverCfg.KeepAlive) * time.Second,
		}
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    clientName,
		Version: clientVersion,
	}, clientOpts)

	var t mcp.Transport
	switch transport {
	case "stdio":
		if serverCfg.Command == "" {
			return nil, fmt.Errorf("stdio transport requires a configured command")
		}
		// 必须用 exec.Command 而非 CommandContext：doConnect 返回后 ctx 会被 cancel，
		// 若用 CommandContext(ctx) 会立刻杀掉子进程，导致 ListTools 等后续请求失败、显示 0 工具
		cmd := exec.Command(serverCfg.Command, serverCfg.Args...)
		if len(serverCfg.Env) > 0 {
			cmd.Env = append(cmd.Env, envMapToSlice(serverCfg.Env)...)
		}
		ct := &mcp.CommandTransport{Command: cmd}
		if serverCfg.TerminateDuration > 0 {
			ct.TerminateDuration = time.Duration(serverCfg.TerminateDuration) * time.Second
		}
		t = ct
	case "sse":
		if serverCfg.URL == "" {
			return nil, fmt.Errorf("sse transport requires a configured url")
		}
		// SSE 是长连接（GET 流持续打开），不能设置 http.Client.Timeout（会在超时后杀掉整个连接导致 EOF）。
		// 超时由每次 ListTools/CallTool 的 context 单独控制。
		httpClient := httpClientForLongLived(serverCfg.Headers)
		t = &mcp.SSEClientTransport{
			Endpoint:   serverCfg.URL,
			HTTPClient: httpClient,
		}
	case "http":
		if serverCfg.URL == "" {
			return nil, fmt.Errorf("http transport requires a configured url")
		}
		httpClient := httpClientWithTimeoutAndHeaders(timeout, serverCfg.Headers)
		st := &mcp.StreamableClientTransport{
			Endpoint:   serverCfg.URL,
			HTTPClient: httpClient,
		}
		if serverCfg.MaxRetries > 0 {
			st.MaxRetries = serverCfg.MaxRetries
		}
		t = st
	default:
		return nil, fmt.Errorf("unsupported transport mode: %s (supported: stdio, sse, http)", transport)
	}

	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	return newSDKClientFromSession(session, client, logger), nil
}

func envMapToSlice(env map[string]string) []string {
	m := make(map[string]string)
	for _, s := range os.Environ() {
		if i := strings.IndexByte(s, '='); i > 0 {
			m[s[:i]] = s[i+1:]
		}
	}
	for k, v := range env {
		m[k] = v
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func httpClientWithTimeoutAndHeaders(timeout time.Duration, headers map[string]string) *http.Client {
	transport := http.DefaultTransport
	if len(headers) > 0 {
		transport = &headerRoundTripper{
			headers: headers,
			base:    http.DefaultTransport,
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// httpClientForLongLived 创建不设超时的 HTTP 客户端，用于 SSE 等长连接传输。
// SSE 的 GET 流会持续打开，http.Client.Timeout 会在超时后强制关闭连接导致 EOF。
// 超时由调用方通过 context 控制。
func httpClientForLongLived(headers map[string]string) *http.Client {
	transport := http.DefaultTransport
	if len(headers) > 0 {
		transport = &headerRoundTripper{
			headers: headers,
			base:    http.DefaultTransport,
		}
	}
	return &http.Client{
		Transport: transport,
		// 不设 Timeout，SSE 长连接的超时由 per-request context 控制
	}
}

type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.base.RoundTrip(req)
}
