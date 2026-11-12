# Pi Token 用量监控

模型用量来自 Pi 返回的 usage，Go 在模型轮次结束后汇总，通过 `agent_usage_summary` 写入现有统计和时间线。辅助 HTTP 调用与供应商账单可能有不同覆盖范围，因此本地统计不等于账户账单。

## 口径

- 输入总量由 Pi 的 input、cacheRead、cacheWrite 相加；输出使用 output。
- 总量为输入与输出之和；缓存读取和缓存写入已经包含在输入中，不能重复叠加。
- 模型调用次数按已观察到的轮次统计，重试会产生新的轮次。
- 未报告的细分不能推断为没有发生。推理 Token 等没有独立字段时，不根据文本长度伪造账单计数。

## 查看

首页 Token 卡片和设置页 Token 用量面板继续支持周期、模型、项目和明细查询。接口为 `GET /api/usage/tokens` 与 `GET /api/conversations/:id/token-usage`。

已有模型用量表保持兼容。旧时间线用量事件可作为历史记录补齐；新记录仅由原生 Pi 事件生成。原生流式消息只传输增量，完整消息在结束时提交，避免重复累计与逐块重复发送整份内容。

实现：`internal/multiagent/pi_runner.go`、`internal/database/model_token_usage.go`、前端用量页面。基础模型 usage 语义见 [Pi AI](https://github.com/earendil-works/pi/tree/v1.5.2/packages/ai)。
