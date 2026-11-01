# Changelog

## Unreleased

### Added

- 支持通过 `agent.task_timeout_minutes` 配置单代理、多代理、工作流及异步任务执行超时：未配置默认 600 分钟，`0` 表示不限时，负数拒绝加载；续跑保留重新计时行为，单次工具超时仍独立生效。

- 新增单代理与多代理异步对话 API：接收后返回任务回执，客户端断线不停止执行；支持按用户隔离的幂等提交、持久状态与结果查询、显式取消以及服务重启后的中断状态。同步接口保持兼容。

- At startup, all project management tables are checked automatically against the unified latest SQLite schema: missing tables are created and missing columns added, with no per-column migration list to maintain; existing data and extra tables/columns are preserved; a failed structural repair rolls back and blocks startup. HITL and notification tables are included in the unified initialization, and the historical vulnerability foreign-key migration preserves extra columns, indexes, triggers, and dependent records.

### Fixed

- 修复 WebShell 批量探活因请求构建异常一直停留在“探活中”的问题；使用连接自身 ID 读取已保存凭据，并在探活失败后恢复按钮、继续处理其他连接。

- 修复审计 Agent 向不支持 thinking 字段的兼容网关发起请求失败的问题；支持独立兼容 profile，并按模型和地址识别默认行为。

- 修复已知支持 `none` 的官方 OpenAI 模型在关闭推理后仍使用服务端默认推理的问题（#366）：按提供商、端点和模型发送并保留显式关闭参数，覆盖 Planner 和强制工具选择；保留 DeepSeek 与第三方兼容网关行为，并明确提示需要 Responses 的工具调用限制。

- Prevented the built-in admin from losing privileges: deactivating the built-in `admin` or removing its system administrator role is rejected, and on failure the original profile, permissions, and sessions are preserved; the UI disables the corresponding actions accordingly.

- Fixed asset-management intelligence collection failing because the default FOFA request does not support the `province` return field; `region` is now used uniformly; legacy form/API field aliases remain compatible, imported assets keep their province, and the intelligence field default is corrected to `asn/org`.

- Fixed single-agent, multi-agent, role workflow, and bulk tasks mistaking a lower-layer call timeout for a whole-task timeout: only expiry of the task context's own deadline marks a task timed out; lower-layer failures keep their original diagnostics; overall timeout and existing retry policies are unchanged.

- C2 Profile and WebShell deletes now uniformly return 404 for missing objects and a generic 500 message on database failures, without exposing SQL errors; vulnerability and bulk-task lists return `[]` when no records match, with added HTTP contract regression verification.

- Fixed stale drafts overwriting others' edits when multiple people edit call-interception rules: only one save per version succeeds; on conflict the draft is kept and the user is prompted to refresh; API saves must carry the ETag/If-Match from the read.

- Fixed platform role updates/deletes logging every user out: only the sessions of members of the affected role are revoked; sessions stay valid when the name, description, or equivalent permission set changes.

- Fixed Russian and other long strings overflowing dashboard status cards, risk badges, and queue labels; side bars and risk names now wrap properly, and the layout adapts to the card's available width.

- Self-update recovery now strictly verifies the backup record, State, commit, and binary pairing before overwriting files; missing, duplicate, or contradictory evidence is rejected, leaving a recoverable state intact.
- Editing a stopped external MCP no longer inadvertently starts a connection; start/stop, config edits, and application restarts keep a consistent started/stopped state.

- The login password reveal/hide button is now an eye icon inside the input field, with unified input width and height, keyboard focus support, a 44px touch area, accessible labels in Chinese, English, and Russian, and light/dark theme support.
