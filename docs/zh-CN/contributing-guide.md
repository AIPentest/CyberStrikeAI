# 贡献规范

[English](../en-US/contributing-guide.md)

本文定义向 CyberStrikeAI 增加功能、接口、工具、前端页面或文档时的基本要求。

## 总原则

- 新功能要有文档入口。
- 新 API 要更新 OpenAPI。
- 新前端文案要补中英文 i18n。
- 新配置要说明是否支持热应用。
- 新高风险工具要说明 HITL 策略。
- 新数据库字段要兼容旧库。
- 新长任务要有状态、取消或恢复策略。

## 新增 API Checklist

- Handler 参数校验明确。
- 错误响应包含稳定 `error` 和可读 `message`。
- 接口受认证保护，除非明确是平台回调。
- 修改类接口写审计。
- 长任务写监控或任务状态。
- 更新 `internal/handler/openapi.go`。
- 更新 API 文档或 Recipe。
- 增加 Handler 测试。

## 新增配置 Checklist

- `config.Config` 结构体有字段。
- `config.yaml` 示例有注释。
- 省略字段时有安全默认值。
- 旧配置能启动。
- 说明是否热应用。
- Web 设置页不会误删未知字段。
- 如影响高风险能力，更新安全文档。

## 新增工具 Checklist

适用于 YAML 工具和 Go 内置 MCP 工具。

- 工具名稳定、具体、避免重名。
- `short_description` 能被 `tool_search` 搜到。
- 输入 schema 明确，不用裸 `cmd` 包所有行为。
- 输出可读且结构稳定。
- 超时和错误路径可控。
- 高风险操作不进全局白名单。
- 文档说明使用场景和风险。

## 新增前端页面 Checklist

- 复用现有 `apiFetch`、modal、通知和状态样式。
- 所有可见文案补 `zh-CN.json` 和 `en-US.json`。
- 有 loading、empty、error 状态。
- 删除/高风险操作有确认。
- 长文本和英文按钮不溢出。
- 浏览器控制台无错误。

## 新增数据库变更 Checklist

- 迁移幂等。
- 旧库可升级。
- 字段默认值合理。
- 大表索引谨慎。
- 测试空库和旧库。
- 发布说明提醒备份。

## 新增高风险能力 Checklist

高风险包括：Shell、WebShell、C2、外部 MCP 写入/执行、凭证访问、批量扫描。

必须回答：

- 谁能调用？
- 是否需要 HITL？
- 审计记录什么？
- 如何取消？
- 如何清理？
- 如何禁用？
- 默认是否关闭？

## 文档要求

每个重要功能至少补：

- 用途。
- 配置。
- 操作流程。
- 风险边界。
- 排错。
- 源码锚点。

中英文文档要保持文件名一致，分别放在：

```text
docs/zh-CN/
docs/en-US/
```

更新导航：

- `docs/README.md`
- `docs/zh-CN/README.md`
- `docs/en-US/README.md`

提交文档变更前，请检查本地链接、代码块闭合、中英文文件名对齐、语言导航覆盖率，以及根 README 中的 Go 版本是否与 `go.mod` 一致。版本化示例应尽量从 `go.mod`、`config.example.yaml` 等权威文件派生，避免手工同步。

## Review 关注点

代码评审优先看：

- 行为回归。
- 安全边界。
- 旧数据兼容。
- 错误处理。
- 测试缺口。
- 文档和 OpenAPI 是否同步。
