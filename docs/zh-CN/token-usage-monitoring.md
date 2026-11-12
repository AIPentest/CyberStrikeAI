# Token 用量监控：调研与实现

[English](../en-US/token-usage-monitoring.md)

调研时间：2026-10-11。

## 调研结论

| 参考 | 官方能力 | 本项目采用的设计 |
| --- | --- | --- |
| [ZCode 使用统计](https://zcode.z.ai/cn/docs/usage-stats) | 本地会话统计与远端编程套餐额度分别展示；支持时间范围、日趋势和模型占比 | 首页增加可展开的本地用量面板，提供 7/30/90 天、日趋势、模型占比与明细 |
| [DeepSeek 上下文缓存](https://api-docs.deepseek.com/guides/kv_cache/) | 响应 usage 的 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` 区分输入缓存命中和未命中 | 将命中字段补入 OpenAI 标准缓存读取字段；不把未命中错误映射为缓存写入 |
| [Eino v0.9.21](https://github.com/cloudwego/eino/releases/tag/v0.9.21)、[schema PR #1309](https://github.com/cloudwego/eino/pull/1309) | 稳定版新增 `PromptTokenDetails.CacheWriteTokens`，原有缓存读取、输入、输出、推理字段继续保留 | 使用稳定版的标准字段贯穿流式归并、运行汇总、数据库和 UI |
| [Eino-ext PR #1015](https://github.com/cloudwego/eino-ext/pull/1015) | 模型适配器传递缓存写入计数 | 升级 agenticclaude 0.1.7、agenticopenai 0.2.4 与对应 OpenAI ACL，而非仅升级框架 |
| [Eino 回调与 Trace](https://www.cloudwego.io/docs/eino/quick_start/chapter_06_callback_and_trace/) | 组件回调可收集模型生命周期、用量与耗时 | 保留现有运行事件采集；未来要覆盖所有内部调用，应另外设计统一模型调用账本 |

版本已通过 Go module 解析和本地源码确认；未采用 0.10 alpha。DeepSeek 原始字段当前不会自动进入 Eino 的标准缓存读取计数，因此在项目现有兼容 HTTP 层补齐，覆盖普通 JSON 和 SSE 流式响应。已有标准 `cached_tokens` 优先，未知字段使用 RawMessage 保存，避免浮点转换损失数字精度。

## 统计口径

- `promptTokens`：模型返回的全部输入。
- `completionTokens`：全部输出，包括供应商计入其中的推理。
- `totalTokens`：使用供应商的总量；缺省时由输入加输出补齐。
- `cachedTokens`：输入中的缓存读取部分。
- `cacheWriteTokens`：输入中的缓存写入部分。
- `reasoningTokens`：输出中的推理部分。
- 缓存读取、写入、推理都是细分，不能再加到总量。缓存读取占输入的比例为 `cachedTokens / promptTokens`；没有输入时显示破折号。
- 一个流式模型输出可能多次返回累计计数，沿用按字段取最大值的归并方式，整条输出结束后只加入一次运行汇总。部分输出发生错误时，仍可保留已返回用量。

DeepSeek 的未命中输入不等于 Claude 的缓存写入。不能根据文本长度推导账单级计数，也不能根据缺失字段断言供应商未使用缓存。旧记录的新增字段默认为 0，表示未记录该细分。

## 功能入口与数据流

点击首页「Token 用量」卡片进入「系统设置 → Token 用量」，也可直接从设置二级菜单打开。首页卡片显示近 7 天总用量，副标仅显示缓存命中率（缓存读取 Token / 输入 Token），调用次数和每日用量保留在详情页。支持当前项目范围、7/30/90 天切换、八项指标、最多 20 项运行模型排行、每日趋势与完整日明细、最多 20 项最近运行。详情页独立加载，支持手动刷新与请求取消，切换周期不会刷新整个仪表盘；刷新页面可保持该设置分区。

详情页采用四项核心指标（总量、输入、输出、缓存读取占输入）与四项次级指标（调用、缓存读取、缓存写入、推理）的层级布局。趋势可切换总 Token、模型调用与缓存命中率，鼠标悬停或键盘聚焦柱形可查看日期与精确数字；切换图表指标不再请求 API。缓存环形图拆分缓存读取与其余输入，不把缓存或推理再次叠加到总量。模型明细展示占比条，每日数字明细默认折叠，最近运行区分完整与部分记录。统计口径可展开查看。

每日图表沿用 API 返回的服务端日期，只补齐已返回日期之间无记录的空档，不根据浏览器时区猜测查询终点。小屏图表和宽表在自己的区域内横向滚动。没有记录和无法获取统计分别展示空状态与不可用状态。

### UI 调研依据（2026-10-12）

| 参考 | 采用的设计 |
| --- | --- |
| [DeepSeek Context Caching](https://api-docs.deepseek.com/guides/kv_cache/) | 使用其明确的缓存命中/未命中输入定义，设计缓存构成视图；比例按 Token 而非请求次数计算 |
| [LiteLLM Usage](https://docs.litellm.ai/docs/proxy/ui/chat_ui) | 周期选择、核心统计与使用日志分层；本项目保留已有数据范围，不新增无法准确计量的费用或成功率 |
| [Langfuse Custom Dashboards](https://langfuse.com/docs/metrics/features/custom-dashboards) | 将汇总、时间趋势、模型分布拆为独立可读区域，并让趋势指标可切换 |

DeepSeek 和 GLM 控制台的完整页面本轮未能公开读取，因此本轮视觉设计以能核实的开源面板为依据。没有声称复刻它们当前登录后的界面。项目也没有新增供应商账单、预算或节省金额推算。

运行事件 → 流式/非流式用量累加 → `eino_usage_summary` → `process_details` 与 `model_token_usage` → 现有 `/api/usage/tokens`、对话用量接口 → 首页与聊天摘要。

新增 `model_token_usage.cache_write_tokens INTEGER NOT NULL DEFAULT 0`。启动时现有声明式 SQLite schema reconciliation 自动补列；旧数据库无需手工执行迁移。现有事件 ID 幂等写入、外键删除关联和历史回填继续生效。日分组按服务本地时区显示，与首页「今日」边界一致。

面板采用文本节点渲染供应商模型名称，表格在手机上局部横向滚动，提供中文、英文、俄文标签。聊天轮次 tooltip、主对话与 WebShell 时间线均增加缓存写入字段。

## 当前边界与后续方向

统计基于已记录的 Agent 输出事件，包括已有采集链路中的子 Agent，**不是全站 API 请求账本**。摘要压缩、检索 embedding、隐藏的模型调用、没有返回 usage 的请求不能由当前链路完整覆盖；因请求取消丢失最终 usage 的消耗也无法还原。

模型排行按运行配置的模型名称归类，发生 failover 的运行可能混合多个模型，不能据此核算逐模型账单。近期列表每项是运行汇总，不是单次 HTTP 请求；模型调用次数也仅统计返回了非空 usage 的输出。

本次不展示费用或套餐剩余额度。自定义中转渠道和订阅套餐的价格、计费方式不同，token 乘固定单价无法可靠代表费用。下一步如需要完整成本管理，应先建设按请求记录的账本：调用来源、真实供应商与模型、成功/错误/取消、耗时、TTFT、usage 是否返回、价格版本和币种；统一覆盖主/子 Agent、压缩、重试、embedding，单独对接供应商额度接口。

## 验证

- `go test ./...`：全仓通过。
- `node --test web/static/js/*.test.cjs`：295 项通过。
- 针对缓存写入的保存/分组/近期记录、旧 schema 补列、用户可见范围、流式去重和总量回退添加回归测试。
- DeepSeek 普通 JSON/SSE 的缓存字段转换、标准字段优先、错误/终止块保持、超大数值精度验证通过。
- 浏览器使用合成数据验证桌面、390px 手机、深色模式、模型名称转义、空数据与接口不可用状态；没有读取真实运行数据库，也没有调用付费模型。
