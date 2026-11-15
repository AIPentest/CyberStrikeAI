# 配置画像

[English](../en-US/configuration-profiles.md)

本文给出几套常用配置画像。它们不是完整 `config.yaml`，而是部署时最容易影响安全和可用性的关键段落。

## 本地开发画像

目标：方便调试，允许较多本地能力。

常用启动：

```bash
chmod +x run.sh && ./run.sh
```

```yaml
server:
  host: 127.0.0.1
  port: 8080
  tls_enabled: true
  tls_auto_self_sign: true
auth:
  session_duration_hours: 12
audit:
  enabled: true
  retention_days: 7
c2:
  enabled: false
multi_agent:
  enabled: true
  skills:
    filesystem_tools: true
```

适用：

- 本地功能开发。
- 调试前端和 Handler。
- 调试 Skills、本地文件工具。

不适用：

- 多人共享。
- 公网访问。

## 内网团队画像

目标：团队共享，保留审计，限制高风险能力。

```yaml
server:
  host: 127.0.0.1
  port: 8080
  tls_enabled: false
auth:
  session_duration_hours: 12
audit:
  enabled: true
  retention_days: 30
monitor:
  retention_days: 90
c2:
  enabled: false
mcp:
  enabled: false
hitl:
  default_reviewer: human
  tool_whitelist: [read_file, glob, grep, tool_search]
```

配合：

- Nginx/Traefik 终止 HTTPS。
- 反向代理 IP 白名单。
- 定期备份 `data/`。

## 只启用知识库画像

目标：把 CyberStrikeAI 作为知识增强助手，尽量关闭攻击面。

```yaml
c2:
  enabled: false
mcp:
  enabled: false
knowledge:
  enabled: true
  base_path: knowledge_base
  retrieval:
    top_k: 5
    similarity_threshold: 0.4
multi_agent:
  skills:
    filesystem_tools: false
```

建议：

- 角色只绑定知识库和只读工具。
- 禁用外部 MCP。
- 不保存真实客户敏感材料。

## 高审计生产画像

目标：生产红队或长期安全平台。

```yaml
auth:
  session_duration_hours: 8
audit:
  enabled: true
  retention_days: 90
  max_detail_bytes: 8192
monitor:
  retention_days: 180
hitl:
  default_reviewer: human
  retention_days: 180
  tool_whitelist: [read_file, glob, grep, tool_search]
c2:
  enabled: false
agent_runtime:
  engine: pi
```

配合：

- 反向代理认证。
- 独立运行用户。
- 日志采集。
- 备份加密。
- 明确项目结束清理流程。

## C2 演练画像

目标：只在授权演练窗口临时启用 C2。

```yaml
c2:
  enabled: true
hitl:
  default_reviewer: human
  tool_whitelist: [read_file, glob, grep, tool_search]
audit:
  enabled: true
monitor:
  retention_days: 180
```

操作要求：

- 演练前确认授权范围。
- Listener 端口和 Web 管理端口分离。
- 演练结束执行 C2 清理 Runbook。
- 结束后恢复 `c2.enabled: false`。

## 外部 MCP 自动化画像

目标：接入可信的内部工具服务。

```yaml
external_mcp:
  servers: {}
multi_agent:
  runtime:
    tool_search_enable: true
    tool_search_min_tools: 20
hitl:
  default_reviewer: audit_agent
  tool_whitelist: [read_file, glob, grep, tool_search]
```

建议：

- 每个 MCP 工具都写清楚 schema。
- 高风险 MCP 工具不进白名单。
- stdio MCP 用独立工作目录。
- HTTP MCP 必须有认证。

## 画像选择决策

| 需求 | 选择 |
| --- | --- |
| 单人开发 | 本地开发画像 |
| 多人内网使用 | 内网团队画像 |
| 文档/知识问答 | 只启用知识库画像 |
| 长期生产平台 | 高审计生产画像 |
| 授权 C2 演练 | C2 演练画像 |
| 接内部工具平台 | 外部 MCP 自动化画像 |
