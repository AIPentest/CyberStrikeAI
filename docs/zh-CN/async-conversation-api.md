# 异步对话 API

长任务使用 `POST /api/eino-agent/runs`（单代理）或 `POST /api/multi-agent/runs`（多代理，需启用配置）。请求体与对应同步接口相同；支持已有 conversationId，省略时创建会话。原同步接口仍等待最终结果，连接断开会取消同步执行。

```sh
curl -X POST "$CSAI_URL/api/eino-agent/runs" \
  -H "Authorization: Bearer $CSAI_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: example-task-001' \
  -d '{"message":"整理本次任务的计划"}'
```

HTTP 202 示例：

```json
{"runId":"...","conversationId":"...","assistantMessageId":"...","status":"running","statusUrl":"/api/eino-agent/runs/..."}
```

收到回执后可关闭连接。`GET statusUrl` 返回 `status`、`result`、标识及时间戳。`result` 为原执行入口的最终响应或错误；任务尚未结束时为空对象。单次任务总预算为600分钟，独立工具和模型限制仍有效。以查询到的终态为准，不以 token usage summary 判断成功。

- 幂等键必填，1–200字节，按认证用户隔离。同键、相同请求返回原回执；同键不同请求返回409。回执丢失时使用原键和原请求重试。新一轮执行使用新键。
- 同一会话已有任务时返回409，不追加该冲突请求的消息。
- 使用 `POST /api/agent-loop/cancel` 和 `{"conversationId":"..."}` 显式取消当前会话任务。取消是会话级操作，请先确认当前运行与待取消回执一致。
- 通过 `/api/conversations/{conversationId}` 查看对话；通过 `/api/messages/{assistantMessageId}/process-details` 读取保存的过程；活动任务也可订阅 `/api/agent-loop/task-events?conversationId=...`。订阅不回放历史。
- 状态可为 running、completed、failed、cancelled、timeout、interrupted，以及交付检查或资源清理状态。只有执行响应的 finalized=true 才表示成功最终交付。
- 提交需要 agent:execute 与 chat:write；查询仍要求接口权限及对话访问权限，后台执行保留提交者主体。
- 回执与结果保存在服务端数据库。当前运行模型为单实例独占数据库；服务启动会将遗留 running 标记 interrupted。不会自动重放未完成工具；需要用户决定是否在原会话继续。
- 异步能力不保证外部工具永不失败，也不承诺进程崩溃后的自动续跑。数据库不可写时提交返回错误，不能将此视为已接收。
