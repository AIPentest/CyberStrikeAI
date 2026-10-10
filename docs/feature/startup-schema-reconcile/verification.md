# SQLite 启动结构补齐验证

- Phase: F5 complete
- Date: 2026-10-10
- Implementation commit: 7f8dab4e

## 自动化结果

- `go test ./internal/database ./internal/knowledge ./internal/handler`：通过。
- `go test ./internal/app`：通过。
- SQL 定义格式整理后 `go test ./internal/database`：通过。
- `git diff --check`：通过。

## 回归证据

schema_reconcile_test.go 覆盖：

- 新表/新列直接来自定义，无需旧迁移列表；默认值、CHECK、逗号与括号、带引号的标识符及虚拟生成列。
- 创建缺表后外键实际生效。
- 补字段或创建唯一索引失败时整个结构事务回滚，原行不变。
- NewDB 重开 SQLite 文件时补齐缺表以及原迁移列表未覆盖的 model_token_usage.cached_tokens、rbac_users.display_name、HITL 字段；逐表比较所有定义的列。
- NewKnowledgeDB 使用相同机制重开文件补结构。
- 连续启动两次，数据、额外表及字段保留。
- 历史漏洞外键迁移保留额外字段、触发器、索引和依赖记录，外键开关恢复，删除会话后漏洞保留。
- 既有批量任务审批测试切换到通用结构检查，依旧验证缺字段恢复和幂等。

所有数据库验证使用临时文件/内存，不修改用户实际数据库。未运行生产部署或全仓库测试；未合并功能分支。
