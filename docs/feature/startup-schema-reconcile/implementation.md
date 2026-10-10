# SQLite 启动结构补齐实现记录

- Phase: F4 complete
- Branch: codex/startup-schema-reconcile
- 结构来源：internal/database/schemas/*.sql，嵌入二进制。
- 自动对比：内存 SQLite 解析最新定义，真实连接事务内创建缺表、补字段、创建缺索引。
- 启动入口：NewDB / NewKnowledgeDB，检查失败关闭连接并返回错误。
- 处理器中 HITL/通知表定义收敛到统一定义；请求不再临时创建通知表。
- 删除 database/RBAC/workflow/knowledge/HITL 的纯字段迁移列表。时间、模型用量、安全拦截和审批等数据回填保留。
- 原数据库定义文件从 1,822 行缩减到 270 行；结构与历史语义迁移各自独立。
- 原漏洞外键迁移改为从最新定义创建目标表，动态保存额外列及其值，保留索引、触发器和依赖记录；它不是缺字段检查的依据。
- 不删除额外表/字段，不自动修改已存在字段类型或约束。不可安全 ADD COLUMN 的定义导致补齐事务回滚，启动失败，附表/字段及原始错误。

实现验证：go test ./internal/database ./internal/knowledge ./internal/handler 通过；go test ./internal/app 通过；格式整理后 go test ./internal/database 再次通过。
