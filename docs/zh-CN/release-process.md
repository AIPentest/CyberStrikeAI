# 发布流程

本文用于维护者或部署者发布、升级和回滚 CyberStrikeAI。

## 版本准备

发布前检查：

- `README.md` 和 `README_CN.md` 的功能说明是否更新。
- `docs/` 是否补充新功能文档。
- `config.example.yaml` 示例是否包含新增配置。
- OpenAPI 是否包含新增接口。
- 中英文 i18n 是否同步。
- 高风险功能是否有安全说明。

## 测试

至少运行：

```bash
go test ./internal/...
```

如果修改了入口、构建或命令：

```bash
go test ./cmd/...
bash scripts/build-pi-runtime.sh
go build -o cyberstrike-ai ./cmd/server
```

如果修改前端，手动验证：

- 登录。
- 对话流式输出。
- 设置保存和应用。
- 工具列表。
- 相关页面无控制台错误。

## 构建

```bash
bash scripts/build-pi-runtime.sh
go build -o cyberstrike-ai ./cmd/server
```

发布包应包含：

- `cyberstrike-ai`
- `web/templates/`
- `web/static/`
- `config.example.yaml` 示例。
- `tools/`
- `roles/`
- `skills/`
- `agents/`
- `docs/`
- `README.md` / `README_CN.md`
- `LICENSE`

不要把本地 `data/`、真实 `config.yaml` 密钥、上传附件和日志打进公开发布包。

## 升级检查清单

升级前：

- 停服务。
- 备份 `config.yaml`。
- 备份 `data/`。
- 备份自定义 `tools/roles/skills/agents/knowledge_base`。
- 记录当前版本和启动方式。

升级后：

- 启动服务。
- 登录。
- 测试模型。
- 检查工具列表。
- 检查知识库状态。
- 检查外部 MCP。
- 检查 C2/WebShell 是否按预期启用或关闭。
- 查看日志和审计。

## 回滚

触发回滚的常见情况：

- 服务无法启动。
- 数据库迁移失败。
- 核心对话功能不可用。
- 高风险功能行为异常。

回滚步骤：

1. 停止新版本。
2. 恢复旧二进制或旧代码。
3. 恢复升级前 `config.yaml`。
4. 恢复升级前 `data/`。
5. 启动旧版本并验证。

如果新版已修改数据库结构，必须恢复数据库备份，不能只替换二进制。

## Changelog 建议

每个版本记录：

- 新增功能。
- 行为变更。
- 配置变更。
- 数据库变更。
- 安全修复。
- 兼容性说明。
- 升级注意事项。

高风险模块的变更要单独标出，例如 C2、WebShell、终端、外部 MCP、HITL。

## 发布风险分级

| 改动 | 风险 | 必测 |
| --- | --- | --- |
| 文档、图片 | 低 | 链接和渲染 |
| 前端页面 | 中 | 登录、页面状态、API 错误 |
| Handler/API | 中 | OpenAPI、权限、错误码 |
| 配置结构 | 高 | 旧配置兼容、ApplyConfig |
| 数据库结构 | 高 | 旧库迁移、回滚策略 |
| Agent/MCP/HITL | 高 | 工具调用、审批、流式中断 |
| C2/WebShell/Terminal | 极高 | 授权环境、审计、禁用开关 |

发布说明里要按风险级别提示用户，而不是只列功能点。

## 配置兼容策略

新增配置字段要遵循：

- 省略时有安全默认值。
- 旧配置能启动。
- 示例 `config.yaml` 有注释。
- Web 设置页不会把未知字段误删。
- 热应用和重启两种路径都验证。

如果新增字段默认开启高风险功能，应重新考虑默认值。

## 数据库变更策略

SQLite 迁移要考虑：

- 用户可能从很老版本直接升级。
- 迁移中断后再次启动是否幂等。
- 新字段是否允许空值。
- 索引是否会锁表太久。
- 是否需要数据回填。

发布说明必须写清楚“升级前备份 data/”。

## Release 验收脚本思路

最小自动化：

```bash
go test ./internal/...
go test ./cmd/...
bash scripts/build-pi-runtime.sh
go build -o cyberstrike-ai ./cmd/server
```

手动冒烟：

```text
登录 -> 模型测试 -> 新建对话 -> 工具列表 -> HITL -> 知识库 -> 外部 MCP -> 关闭/开启 C2
```

对高风险模块，宁可多做一个授权靶场测试，也不要只靠单测放行。
