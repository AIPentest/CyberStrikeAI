# SQLite 启动结构补齐实现计划

- Branch: codex/startup-schema-reconcile
- 输入：requirements.md（Confirmed）、design.md
- Workflow Gate：P7 后端实现；上游契约与设计齐全，允许实施。

1. 提取现有初始化后的最终 SQL 到 internal/database/schemas，验证其覆盖所有表及索引。
2. 新增 schema_reconcile.go，通过内存参考库与事务实现通用补齐；DDL 分词用于提取准确的列定义。
3. 数据库入口改为薄委托，删除启动对纯字段迁移的依赖；保留数据回填，既有漏洞外键迁移保存额外列。
4. 添加数据库级回归测试，运行 go test ./internal/database ./internal/knowledge ./internal/handler ./internal/app 与 git diff --check。
5. 更新行为文档及 CHANGELOG，记录验证和提交，推送功能分支；本任务不合并或发布。

回滚：恢复代码版本；新增表和字段保留，不自动删除；升级前的历史数据不改写。验证使用临时 SQLite 文件，不操作用户实际数据库。
