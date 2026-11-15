# Pi Token 用量监控

模型用量来自 Pi 返回的 usage，Go 在模型轮次结束后汇总，通过 `agent_usage_summary` 写入现有统计和时间线。辅助 HTTP 调用与供应商账单可能有不同覆盖范围，因此本地统计不等于账户账单。

## 口径

- 输入总量由 Pi 的 input、cacheRead、cacheWrite 相加；输出使用 output。
- 总量为输入与输出之和；缓存读取和缓存写入已经包含在输入中，不能重复叠加。
- 供应商单独报告的推理 Token 已包含在输出中，面板单列展示。
- 模型调用次数按已观察到的轮次统计，重试会产生新的轮次。
- 未报告的细分不能推断为没有发生。推理 Token 等没有独立字段时，不根据文本长度伪造账单计数。
- 深度评估使用其持久预算账本；供应商未报告用量时采用原有保守计费策略。

## 查看

首页 Token 卡片和设置页 Token 用量面板继续支持周期、模型、项目和明细查询。接口为 `GET /api/usage/tokens` 与 `GET /api/conversations/:id/token-usage`。

已有模型用量表保持兼容。旧时间线用量事件可作为历史记录补齐；新记录仅由原生 Pi 事件生成。原生流式消息只传输增量，完整消息在结束时提交，避免重复累计与逐块重复发送整份内容。

实现：`internal/multiagent/pi_runner.go`、`internal/database/model_token_usage.go`、前端用量页面。基础模型 usage 语义见 [Pi AI](https://github.com/earendil-works/pi/tree/v1.5.2/packages/ai)。

## DeepSeek

在 AI 通道的推理设置中，可用 `off` 处理简单问答和格式转换，或用 `auto` 配合 `low` 保留低强度推理。官方接口未指定强度时默认使用 `high`，`medium` 也映射到 `high`。通道的「单次输出上限」通过 `max_tokens` 发送，推理和正文共用额度。对照面板里的输出、推理 Token 和缓存命中率，可比较相同任务在不同强度下的用量。[DeepSeek 推理模式](https://api-docs.deepseek.com/guides/thinking_mode/) · [Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)

官方接口自动缓存可复用的请求前缀。Pi 按名称固定 MCP 工具定义的顺序，以减少相同工具目录在请求之间的前缀变化。缓存命中以供应商返回的用量为准。[DeepSeek 上下文缓存](https://api-docs.deepseek.com/guides/kv_cache/)

后台默认根据官方 DeepSeek 地址、DeepSeek 线路或模型名自动启用按需工具发现、JSON 回包精简与可回读的大结果预览。用户配置模型后即可使用。验证节省效果时，对照完整任务的输入、输出、缓存命中和调用次数，并检查任务产物与证据完整性。具体阈值见[配置说明](configuration.md#deepseek-输入优化)。
