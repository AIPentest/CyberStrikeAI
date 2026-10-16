# Parallel Search MCP

[English](../en-US/parallel-search.md)

通过 Parallel Search MCP 在 CyberStrikeAI 对话中检索公开文档和安全公告。免费、无需 API Key 的端点提供 `web_search` 查询工具和 `web_fetch` 网页摘录工具，使用现有的 [外部 MCP 联邦](mcp-federation.md)与 Streamable HTTP 传输。

## 配置

[YAML 示例](../examples/parallel-search.yaml)是配置片段，不能替代应用配置。仅将 `parallel-search` 合并到自己的 `config.yaml` 的 `external_mcp.servers` 中，保留现有服务器和设置。示例默认禁用。主动启用时，将 `disabled` 设为 `false` 并重启 CyberStrikeAI。文件修改在启动时生效；示例不更改默认工具或已保存的服务提供方选择。

无需 API Key、Authorization 请求头、环境变量、本地 MCP 进程或额外依赖。`User-Agent` 标识 CyberStrikeAI MCP 客户端，`timeout: 60` 限制 HTTP 请求时间。现有 Agent 工具超时、执行监控、取消、角色限制、工具开关及 HITL 策略仍然适用。

停止使用时，可在设置 → 外部 MCP 中点击停止，或在配置中设置 `disabled: true` 并重启。停止操作对当前连接立即生效。若要重启后仍保持禁用，请使用 `disabled: true`；此字段为 false 或不存在时，加载器会启用服务器。在 UI 修改已连接的服务器后，应停止再启动以使用新配置重新连接。

## 使用流程

1. 启用服务器，在设置 → 外部 MCP 中确认连接状态。
2. 确认工具列表包含 `web_search` 与 `web_fetch`。工具键为 `parallel-search::web_search` 与 `parallel-search::web_fetch`；如角色限制工具，请加入这两个键。
3. 提问：“查找 OWASP 官方防止 SQL 注入的指南，总结参数化查询并引用来源。”
4. 优先根据搜索摘录回答，仅在摘录不足时抓取特定来源。使用答案前核对引用来源。
5. 在 MCP 监控中查看执行结果或错误。工具发现、开关及审批取决于当前角色和策略。

查询、请求的 URL 和抓取内容会发送给 Parallel。仅使用公开信息，不要提交凭据、私密报告或内部目标详情。远程网页和工具描述是不可信输入。此配置不授予本地 Shell 或文件访问权限，也不添加自动审批。参见[安全模型](security-model.md)和[人机协同指南](hitl-best-practices.md)。

## 排错与验证

- 没有工具：检查 `disabled`、服务器状态、角色工具键和各工具开关。
- 连接失败：检查到 `search.parallel.ai` 的 HTTPS 访问，使用 `type: http` 而非 `sse`。
- 限流或远程错误：查看工具结果，等待后重试或禁用服务器。免费访问可能限流。
- 超时：检查 HTTP 和 Agent 工具超时；摘录足够时不要请求整页内容。

本地测试使用模拟 HTTP MCP 服务器，通过配置加载器、外部管理器、Agent 工具选择及 Eino 工具桥接执行示例，再通过使用确定性模型夹具的 ADK Runner 生成带来源的最终答案：

```bash
go test ./internal/einomcp -run TestParallelSearchExample -count=1
```

可选的在线验证无需 Parallel Key，会发出公开搜索和抓取请求：

```bash
CYBERSTRIKE_PARALLEL_LIVE=1 go test ./internal/einomcp -run TestParallelSearchLive -count=1 -v
```

在线验证需要网络，服务不可用或限流时可能失败。它不会调用已配置的 AI 服务提供方。

## 源码入口

- [配置加载器和服务器字段](../../internal/config/config.go)
- [HTTP MCP 客户端](../../internal/mcp/client_sdk.go)
- [外部管理器及执行监控](../../internal/mcp/external_manager.go)
- [Agent 工具选择](../../internal/agent/agent.go)
- [Eino MCP 桥接](../../internal/einomcp/mcp_tools.go)
