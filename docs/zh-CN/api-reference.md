# API 参考

CyberStrikeAI 内置 OpenAPI 规格和 API 文档页面。启动服务后访问：

```text
/api-docs
```

OpenAPI JSON：

```text
GET /api/openapi/spec
```

`/api/openapi/spec` 需要登录认证，避免未授权用户直接枚举接口结构。

## 认证

登录：

```http
POST /api/auth/login
Content-Type: application/json

{"password":"your-password"}
```

认证成功后，前端通常使用 Cookie 会话。外部客户端也可参考 OpenAPI 中的 Bearer Token 描述，按实际返回字段接入。

常用认证接口：

- `POST /api/auth/login`
- `POST /api/auth/logout`
- `POST /api/auth/change-password`
- `GET /api/auth/validate`

## 对话与 Agent

单代理：

- `POST /api/eino-agent`
- `POST /api/eino-agent/stream`

多代理：

- `POST /api/multi-agent`
- `POST /api/multi-agent/stream`

多代理请求体通过 `orchestration` 指定：

- `deep`
- `plan_execute`
- `supervisor`

常用请求体字段：

| 字段 | 说明 |
| --- | --- |
| `message` | 用户消息，必填。 |
| `conversationId` | 继续已有对话；为空时创建新对话。 |
| `projectId` | 新对话绑定项目；为空时可跟随 `config.project.default_project_id`。 |
| `role` | 使用指定角色。 |
| `aiChannelId` | 选择 `ai.channels` 中的通道 ID；为空时使用 `ai.default_channel`。 |
| `reasoning` | 会话级推理覆盖，受通道 `reasoning.allow_client_reasoning` 控制。 |
| `hitl` | 会话级人机协同配置。 |

对话管理：

- `POST /api/conversations`
- `GET /api/conversations`
- `GET /api/conversations/:id`
- `PUT /api/conversations/:id`
- `DELETE /api/conversations/:id`
- `POST /api/conversations/:id/delete-turn`
- `GET /api/messages/:id/process-details`

## 文件管理来源

文件管理页面和 `/api/chat-uploads` 列表接口会把对话相关文件按来源归类。底层目录仍使用项目 ID 或会话 ID 保持稳定，界面会优先显示项目名或对话标题，完整 ID 可在提示或路径中查看。

| 来源 | `source` | 典型目录 | 说明 | 可变更性 |
| --- | --- | --- | --- | --- |
| 工作目录 | `workspace` | `tmp/workspace/projects/<projectId>/...`、`tmp/workspace/conversations/<conversationId>/...` | Agent 执行任务时保存下载文件、分析脚本、中间结果和生成的 CSV/XLSX/Markdown 等。用户反馈“AI 生成的文件找不到”时，通常先看这里。 | 只读展示；支持复制路径、下载、导出。 |
| 会话产物 | `conversation_artifact` | `data/conversation_artifacts/<conversationId>/...` | 系统按会话归档的交付物或会话级产物，例如总结、报告、模型中间件生成的归档内容。 | 只读展示；支持复制路径、下载、导出。 |
| 工具输出 | `reduction` | `tmp/reduction/projects/<projectId>/...`、`tmp/reduction/conversations/<conversationId>/...` | 超长工具输出、扫描原文或被截断前落盘的结果缓存。适合回看完整工具输出。 | 只读展示；支持复制路径、下载、导出。 |
| 对话附件 | `upload` | `chat_uploads/<date>/<conversationId>/...` | 用户在对话或文件管理页手动上传的附件。需要让 AI 引用某文件时，可复制服务器绝对路径粘贴到对话中。 | 可上传、新建目录、编辑文本文件、重命名、删除、复制路径、下载、导出。 |

相关接口：

- `GET /api/chat-uploads`：按来源、项目、会话、文件名筛选文件。
- `GET /api/chat-uploads/path`：把文件管理中的相对路径或内部虚拟路径解析为服务器绝对路径，用于复制文件或目录路径。
- `GET /api/chat-uploads/download`：下载指定文件。
- `GET /api/chat-uploads/export`：导出当前筛选结果为 ZIP。
- `POST /api/chat-uploads`：上传到对话附件目录。

## 项目、漏洞、攻击链

项目：

- `GET /api/projects`
- `POST /api/projects`
- `GET /api/projects/:id`
- `PUT /api/projects/:id`
- `DELETE /api/projects/:id`
- `GET /api/projects/:id/facts`
- `POST /api/projects/:id/facts`
- `GET /api/projects/:id/fact-graph`

漏洞：

- `GET /api/vulnerabilities`
- `POST /api/vulnerabilities`
- `GET /api/vulnerabilities/:id`
- `PUT /api/vulnerabilities/:id`
- `DELETE /api/vulnerabilities/:id`
- `GET /api/vulnerabilities/export`

攻击链：

- `GET /api/attack-chain/:conversationId`
- `POST /api/attack-chain/:conversationId/regenerate`

## 资产管理与批量导入

资产接口：

- `GET /api/assets`：分页查询资产；
- `GET /api/assets/selection`：按当前筛选条件解析跨页选择，最多返回 10000 条；
- `GET /api/assets/stats`：获取资产统计，`days` 仅支持 `7`、`30` 或 `90`；
- `POST /api/assets/import`：新增或去重更新资产，单次最多 100000 条；
- `POST /api/assets/scan-links`：批量记录扫描关联，单次最多 10000 条；
- `PUT /api/assets/bulk`：原子批量更新最多 10000 个资产；
- `PUT /api/assets/project-binding`：批量绑定项目，单次最多 10000 个资产 ID；
- `POST /api/assets/batch-delete`：原子批量删除最多 10000 个资产；
- `POST /api/assets/merge`：合并 2-100 个具有共同身份的重复资产；
- `PUT /api/assets/:id`：更新资产；
- `DELETE /api/assets/:id`：删除资产。

`GET /api/assets` 和 `GET /api/assets/selection` 使用相同的筛选与排序参数；`selection` 会忽略分页参数并返回全部匹配项（最多 10000 条）：

| 类别 | 参数 |
| --- | --- |
| 分页（仅列表） | `page`、`page_size`（最大 100） |
| 常用 | `q`、`status`、`project_id`、`risk_level`、`min_vulnerabilities`、`max_vulnerabilities` |
| 目标与来源 | `host`、`ip`、`domain`、`port`、`protocol`、`source`、`tag` |
| 责任与业务 | `responsible_person`、`department`、`business_system`、`environment`、`criticality` |
| 地理 | `country`、`province`、`city` |
| 扫描 | `scan_state=never|scanned`、`scan_overdue_days`、`last_scan_before`、`last_scan_after` |
| 发现时间 | `first_seen_before`、`first_seen_after`、`last_seen_before`、`last_seen_after` |
| 排序 | `sort_by`、`sort_order=asc|desc` |

时间参数接受 RFC3339 或 `YYYY-MM-DD`。`sort_by` 支持 `last_seen_at`、`last_scan_at`、`first_seen_at`、`created_at`、`updated_at`、`host`、`port`、`risk_level` 和 `vulnerability_count`。

`POST /api/assets/import` 接收 JSON，而不是 XLSX/CSV 文件。Web 端会在浏览器中解析模板、预览并转换为该请求格式：

```http
POST /api/assets/import
Authorization: Bearer <token>
Content-Type: application/json

{
  "source": "manual-import",
  "source_query": "asset-import-2026-07.xlsx",
  "assets": [
    {
      "host": "https://app.example.com:443",
      "domain": "app.example.com",
      "port": 443,
      "protocol": "https",
      "title": "Example App",
      "server": "nginx",
      "project_id": "<project-id>",
      "responsible_person": "Alice",
      "department": "Security",
      "business_system": "Customer Portal",
      "environment": "production",
      "criticality": "critical",
      "tags": ["production", "internet"],
      "status": "active"
    },
    {
      "ip": "192.0.2.10",
      "port": 22,
      "protocol": "ssh",
      "status": "active"
    }
  ]
}
```

请求规则：

- `assets` 必须包含 `1-100000` 条；
- 每条资产的 `host`、`ip`、`domain` 至少一项非空；
- `port` 范围为 `0-65535`；
- `status` 仅支持 `active` 或 `inactive`；
- `environment` 支持空值或 `production`、`staging`、`testing`、`development`、`other`；
- `criticality` 支持空值或 `critical`、`high`、`medium`、`low`；
- 标签最多 30 个，单个最多 64 个字符；
- `project_id` 非空时，调用者必须有权访问该项目；
- 需要 `asset:write` 权限；
- 服务端按“IP + 域名 + 端口 + 协议”（IP 与域名都缺失时为 Host）去重，并在同一事务中处理本次请求。

成功响应：

```json
{
  "created": 120,
  "updated": 8,
  "skipped": 2
}
```

- `created`：新建数量；
- `updated`：命中去重键并合并更新的数量；
- `skipped`：空记录或因资源归属不可更新而跳过的数量。

字段校验失败返回 `400`，且响应 `error` 会包含出错资产的顺序。项目无权访问返回 `403`。批量导入的模板字段和 UI 操作见[资产管理指南](asset-management.md#从表格批量导入)。

批量编辑示例：

```http
PUT /api/assets/bulk
Content-Type: application/json

{
  "asset_ids": ["<asset-id-1>", "<asset-id-2>"],
  "responsible_person": "Alice",
  "department": "Security",
  "environment": "production",
  "criticality": "high",
  "add_tags": ["internet-facing"],
  "remove_tags": ["untriaged"]
}
```

批量字段均为可选；未提供的字段保持原值。`add_tags` 和 `remove_tags` 会在事务内去重处理。批量编辑、项目绑定和批量删除会先验证全部资产的可访问性，任一 ID 不存在或无权访问时整批失败。

重复资产合并示例：

```http
POST /api/assets/merge
Content-Type: application/json

{
  "asset_ids": ["<primary-id>", "<duplicate-id>"],
  "primary_id": "<primary-id>"
}
```

每个待删除记录必须与主资产共享域名、IP 或 Host。主资产已有字段优先，空字段从其他记录补齐，标签取并集；调用者需要更新主资产和删除其余资产的权限。

## 工具、MCP、配置

配置：

- `GET /api/config`
- `PUT /api/config`
- `POST /api/config/apply`
- `GET /api/config/tools`
- `GET /api/config/tools/:name/schema`
- `POST /api/config/test-openai`
- `POST /api/config/test-vision`
- `POST /api/config/list-models`

MCP：

- `POST /api/mcp`
- `GET /api/external-mcp`
- `PUT /api/external-mcp/:name`
- `POST /api/external-mcp/:name/start`
- `POST /api/external-mcp/:name/stop`
- `DELETE /api/external-mcp/:name`

## 知识库、Skills、角色、Agent

知识库：

- `GET /api/knowledge/categories`
- `GET /api/knowledge/items`
- `POST /api/knowledge/scan`
- `POST /api/knowledge/index`
- `POST /api/knowledge/search`

角色：

- `GET /api/roles`
- `POST /api/roles`
- `GET /api/roles/:name`
- `PUT /api/roles/:name`
- `DELETE /api/roles/:name`

Skills：

- `GET /api/skills`
- `POST /api/skills`
- `GET /api/skills/:name`
- `PUT /api/skills/:name`
- `DELETE /api/skills/:name`
- `GET /api/skills/:name/files`
- `GET /api/skills/:name/file`
- `PUT /api/skills/:name/file`

Markdown 子代理：

- `GET /api/multi-agent/markdown-agents`
- `POST /api/multi-agent/markdown-agents`
- `GET /api/multi-agent/markdown-agents/:filename`
- `PUT /api/multi-agent/markdown-agents/:filename`
- `DELETE /api/multi-agent/markdown-agents/:filename`

## 高风险能力

WebShell：

- `GET /api/webshell/connections`
- `POST /api/webshell/connections`
- `POST /api/webshell/exec`
- `POST /api/webshell/file`

C2：

- `GET /api/c2/listeners`
- `POST /api/c2/listeners`
- `GET /api/c2/sessions`
- `POST /api/c2/tasks`
- `POST /api/c2/payloads/build`

终端：

- `POST /api/terminal/run`
- `POST /api/terminal/run/stream`
- `GET /api/terminal/ws`

这些接口应只开放给可信管理员，并配合 HTTPS、强密码、网络隔离和审计。

## 调用建议

- 优先使用 `/api-docs` 查看完整参数和响应结构。
- 流式接口使用 SSE，反向代理需关闭缓冲。
- 所有修改类接口都应处理 401、403、404、409、500。
- 外部集成建议创建最小权限网络路径，不要把 Web 管理面直接暴露到公网。

## 认证细节

认证中间件会按顺序提取 token：

1. `Authorization: Bearer <token>`
2. `Authorization: <token>`
3. 查询参数 `?token=<token>`
4. Cookie `auth_token`

这意味着外部脚本最稳妥的方式是使用 `Authorization: Bearer`。查询参数虽然支持，但容易进入代理日志，不建议生产使用。

## SSE 客户端注意事项

`/api/eino-agent/stream` 和 `/api/multi-agent/stream` 是长连接。客户端应处理：

- 网络中断后不要盲目重放破坏性请求。
- 收到 `error` 事件后读取错误正文。
- 收到 `done` 才视为本轮结束。
- 代理层不能缓冲。
- 请求体中的 `conversationId` 决定是否接续已有对话。

## API 稳定性分层

| API 类型 | 稳定性 | 集成建议 |
| --- | --- | --- |
| `/api/auth/*` | 高 | 可直接集成 |
| `/api/eino-agent*` | 高 | 推荐外部对话入口 |
| `/api/openapi/spec` | 高 | 用于生成客户端 |
| `/api/assets/*` | 高 | 资产管理与批量导入 |
| `/api/config*` | 中 | 管理工具使用，谨慎自动化 |
| `/api/c2/*`、`/api/webshell/*` | 中 | 高风险，必须加权限边界 |
| 前端私有调用细节 | 低 | 不建议插件依赖 |

## Curl 示例

登录并提取 token 的返回字段可能随实现调整，建议先看 `/api-docs`。如果已有 token：

```bash
curl -k https://127.0.0.1:8080/api/conversations \
  -H "Authorization: Bearer <token>"
```

发送非流式单代理请求：

```bash
curl -k https://127.0.0.1:8080/api/eino-agent \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"message":"对 127.0.0.1 做授权的基础信息收集，先不要执行高风险操作"}'
```

## 源码锚点

- 路由：`internal/app/app.go`
- 认证：`internal/security/auth_middleware.go`
- OpenAPI：`internal/handler/openapi.go`
- 单代理：`internal/handler/eino_single_agent.go`
- 多代理：`internal/handler/multi_agent.go`
- 资产接口：`internal/handler/asset.go`
- 资产存储与去重：`internal/database/asset.go`
