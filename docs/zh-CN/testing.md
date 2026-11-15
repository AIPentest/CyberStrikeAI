# 测试指南

CyberStrikeAI 的测试包括 Go 单测、配置验证、API 手测、MCP 工具验证和前端冒烟测试。

## Go 单测

运行全部内部测试：

```bash
go test ./internal/...
```

运行指定包：

```bash
go test ./internal/workflow
go test ./internal/multiagent
go test ./internal/handler
```

常见重点包：

- `internal/security`
- `internal/mcp`
- `internal/multiagent`
- `internal/workflow`
- `internal/knowledge`
- `internal/project`
- `internal/handler`
- `internal/c2`

## 构建测试

```bash
go build -o cyberstrike-ai ./cmd/server
```

构建通过不代表功能正确，但能发现入口、依赖和静态类型问题。

## 配置验证

启动前检查：

- YAML 缩进。
- 模型配置。
- 数据库路径可写。
- `tools_dir`、`roles_dir`、`skills_dir`、`agents_dir` 是否存在。
- HTTPS 证书路径是否正确。

启动后在 Web 设置页测试：

- OpenAI 兼容模型。
- 视觉模型。
- 工具列表。
- 外部 MCP 状态。

## API 手测

访问：

```text
/api-docs
```

重点验证：

- 登录。
- `/api/agent/stream`。
- `/api/config`。
- `/api/config/tools`。
- `/api/knowledge/search`。
- `/api/monitor`。

流式接口经过反向代理时要验证输出是否实时。

## 工具测试

新增或修改 `tools/*.yaml` 后：

- 在工具列表中确认 schema。
- 用低风险参数执行。
- 检查错误输出是否可读。
- 检查超时是否生效。
- 检查 HITL 是否按预期拦截。

不要用生产目标测试新工具。

## MCP 测试

外部 MCP：

- stdio：先在终端独立运行命令。
- HTTP/SSE：用 curl 检查连通性。
- Web 页面启动后检查 `/api/external-mcp/stats`。
- 在对话中确认工具是否可被 `tool_search` 找到。

## 知识库测试

步骤：

1. 放入小型 Markdown 文档。
2. 扫描知识库。
3. 重建索引。
4. 搜索文档中的关键词和同义表达。
5. 查看检索日志。

如果使用真实 embedding API，注意配额和速率限制。

## 前端冒烟

修改前端后至少验证：

- 登录和退出。
- 侧边栏对话列表。
- 新建对话和流式回复。
- 设置页面保存。
- 相关业务页面增删改查。
- 中英文切换。
- 浏览器控制台无明显错误。

## 高风险模块测试

C2、WebShell、终端、批量任务只能在授权测试环境验证。测试前确认：

- 目标是本机、靶机或演练环境。
- 命令无破坏性。
- HITL 策略符合预期。
- 测试后清理会话、payload、上传文件和任务结果。

## 测试金字塔

建议测试分层：

| 层级 | 目标 | 示例 |
| --- | --- | --- |
| 单元测试 | 纯逻辑正确 | 表达式、chunk、脱敏、超时格式 |
| Handler 测试 | HTTP 行为 | 参数校验、状态码、权限 |
| 集成测试 | 多模块协作 | 外部 MCP、知识库索引、HITL |
| 冒烟测试 | 用户路径可用 | 登录、对话、工具、设置 |
| 授权靶场测试 | 高风险能力安全 | C2、WebShell、终端 |

不要用端到端手测代替单元测试，也不要用单元测试代替高风险靶场验证。

## 回归测试重点

修改这些模块时必须扩大测试范围：

- `internal/handler/config.go`：测模型、知识库、MCP、C2、机器人配置应用。
- `internal/multiagent/`：测流式、工具调用、摘要、重试、HITL。
- `internal/security/`：测认证、Shell、超时、无输出。
- `internal/database/`：测旧数据兼容。
- `web/static/js/chat.js`：测对话、过程详情、攻击链、分组。

## 测试数据管理

不要用真实客户数据做测试。建议准备：

- 小型 Markdown 知识库样例。
- 本地假 MCP Server。
- 本地可控 HTTP 目标。
- 无害 WebShell 模拟端。
- 临时 SQLite 数据库。

测试完成后删除临时数据库和上传文件，避免污染开发环境。

## 失败用例比成功用例更重要

至少覆盖：

- 模型 API 401/429/500。
- MCP 进程启动失败。
- 工具超时。
- HITL 拒绝。
- 知识库索引中断。
- 数据库不可写。
- WebShell 目标返回非 200。
- C2 关闭时访问接口。

这些才是用户真实会遇到的问题。

## 源码锚点

已有测试集中在：

- `internal/handler/*_test.go`
- `internal/multiagent/*_test.go`
- `internal/workflow/*_test.go`
- `internal/knowledge/*_test.go`
- `internal/security/*_test.go`
- `internal/mcp/*_test.go`
- `internal/c2/*_test.go`
