# API Recipes

[English](../en-US/api-recipes.md)

本文给出外部脚本或插件常用的 API 调用配方。完整字段以 `/api-docs` 和 `/api/openapi/spec` 为准。

## Recipe 1：登录并验证

```bash
curl -k https://127.0.0.1:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"password":"<password>"}'
```

后续请求推荐使用：

```bash
Authorization: Bearer <token>
```

验证：

```bash
curl -k https://127.0.0.1:8080/api/auth/validate \
  -H "Authorization: Bearer <token>"
```

## Recipe 2：创建对话并发送消息

最简单方式是不先创建空对话，直接调用 Agent：

```bash
curl -k https://127.0.0.1:8080/api/eino-agent \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"message":"对 127.0.0.1 做授权的基础信息收集，只做只读操作"}'
```

如果需要先创建对话：

```bash
curl -k https://127.0.0.1:8080/api/conversations \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"title":"Web 测试"}'
```

然后把返回的 `conversationId` 放入 Agent 请求。

## Recipe 3：流式调用 Agent

```bash
curl -k -N https://127.0.0.1:8080/api/eino-agent/stream \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"message":"总结当前项目事实并列出下一步，只读"}'
```

注意：

- `-N` 禁用 curl 缓冲。
- 反向代理也要关闭 buffering。
- 收到 `done` 才算本轮结束。

## Recipe 4：调用多代理

```bash
curl -k -N https://127.0.0.1:8080/api/multi-agent/stream \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "message":"对授权目标做分阶段 Web 安全测试，先规划再执行只读步骤",
    "orchestration":"plan_execute"
  }'
```

可选 `orchestration`：

- `deep`
- `plan_execute`
- `supervisor`

## Recipe 5：上传附件

```bash
curl -k https://127.0.0.1:8080/api/chat-uploads \
  -H "Authorization: Bearer <token>" \
  -F "file=@./request.txt"
```

大文件建议上传后在消息中引用，不要直接塞进 prompt。

## Recipe 6：写入漏洞

```bash
curl -k https://127.0.0.1:8080/api/vulnerabilities \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "title":"示例 SQL 注入",
    "severity":"high",
    "target":"https://example.com/item?id=1",
    "description":"参数 id 存在可验证 SQL 注入",
    "evidence":"只读验证输出...",
    "remediation":"使用参数化查询"
  }'
```

字段以 OpenAPI 为准。

## Recipe 7：查询知识库

```bash
curl -k https://127.0.0.1:8080/api/knowledge/search \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query":"SQL 注入如何判断字段数",
    "riskType":"SQL Injection",
    "topK":5,
    "threshold":0.4
  }'
```

如果结果为空，先调用 categories 看风险类型名称是否匹配。

## Recipe 8：检查外部 MCP 状态

```bash
curl -k https://127.0.0.1:8080/api/external-mcp/stats \
  -H "Authorization: Bearer <token>"
```

如果服务 running 但 Agent 找不到工具，检查角色工具限制和 `tool_search`。

## Recipe 9：获取工具 schema

```bash
curl -k https://127.0.0.1:8080/api/config/tools/nmap/schema \
  -H "Authorization: Bearer <token>"
```

插件或自动化脚本应根据 schema 构造参数，不要猜字段名。

## Recipe 10：导出审计日志

```bash
curl -k "https://127.0.0.1:8080/api/audit/logs/export" \
  -H "Authorization: Bearer <token>" \
  -o audit.csv
```

导出文件可能包含敏感操作信息，应加密保存。

## Recipe 11：批量导入资产

先准备 `assets.json`：

```json
{
  "source": "api-import",
  "source_query": "cmdb-export-2026-07",
  "assets": [
    {
      "domain": "app.example.com",
      "port": 443,
      "protocol": "https",
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

提交：

```bash
curl -k https://127.0.0.1:8080/api/assets/import \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  --data-binary @assets.json
```

返回示例：

```json
{"created":2,"updated":0,"skipped":0}
```

注意：

- 调用者需要 `asset:write` 权限。
- 每条资产至少填写 `host`、`ip` 或 `domain`。
- 单次最多 100000 条；大批量请求建议使用文件配合 `--data-binary`，不要把 JSON 直接写进命令行。
- 已存在的“IP + 域名 + 端口 + 协议”（IP 与域名都缺失时为 Host）会合并更新并计入 `updated`。
- 如需从 XLSX/CSV 操作，使用 Web 端 **资产库 → 批量导入**；接口本身接收 JSON，不接收 multipart 文件。
