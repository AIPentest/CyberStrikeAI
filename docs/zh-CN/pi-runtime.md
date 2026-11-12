# Pi 原生运行时

CyberStrikeAI 默认使用 Pi 1.5.2（`@earendil-works/pi-coding-agent`、`pi-ai`），Go 保留业务、权限、工具执行和证据管理。

## 构建与使用

需要 Go（版本见 go.mod）和 Node.js >=22.19。

```sh
bash scripts/build-pi-runtime.sh
go build -o cyberstrike-ai ./cmd/server
```

`run.sh` 和服务自更新的构建步骤都会准备 Pi。构建使用锁文件和 `npm ci --ignore-scripts`；发布到 `runtime/pi/.builds/<源码指纹>/` 后才会被选用。正在运行的 Worker 使用原来的依赖目录；源码回滚时选择对应旧指纹的包。缓存属于构建产物，不提交到 Git。部署时需保留运行时源码和已构建的匹配包，或在目标机器重新构建；只复制 Go 二进制不足以运行 Pi。

```yaml
agent_runtime:
  engine: pi
  node_path: node
  worker_path: ""
  session_dir: data/pi-sessions
```

设置页可选择运行时。`worker_path` 是管理员的显式覆盖，适合开发或独立打包；留空使用源码指纹校验后的构建包。未填写 engine 的新代码默认使用 Pi；现有部署的运行数据和模型配置不会因此自动迁移或部署。

## 执行边界

- 聊天、机器人、批量任务、工作流 Agent 节点通过统一调度入口进入 Pi。HTTP 路由与 `eino_single` 等历史模式值暂保留兼容，模式与运行时分别选择。
- Go 管理每次调用的 Worker，支持流式事件、工具回调、补充指令与取消；同一进程中的相同会话存储串行访问，不同会话独立运行。
- Worker 使用显式模型、内存凭据和资源加载器，不自动发现个人 Pi 配置、全局插件或默认文件／Shell 工具。
- 工具较多时使用 Pi 原生 tool_search 按需加载，减少模型一次接收的工具定义。TaskCreate／Update／Get／List 和 write_todos 继续写入现有对话任务板。
- MCP、文件和 Shell 工具回到 Go，经角色过滤、HITL 和现有执行监控处理。子代理工具集合受主代理集合约束。
- Deep 使用任务委派；Supervisor 提供专家委派和 exit；Plan-Execute 使用计划记录与执行子代理。最终文本仍需通过既有证据验收。
- Pi 原生重试及虚拟模型路由承接聊天备用渠道。认证／配置错误不能用无界“继续”消除。
- 上下文窗口和输出上限沿用渠道设置；输入预算用于 Pi 压缩阈值。压缩后的模型上下文和完整会话记录分别保留。

## 数据与兼容

- Pi Session 是模型会话及压缩记录的权威来源；SQLite 保存业务状态、工具记录和交付证据。两者通过会话和执行 ID 关联。
- 同一个 Worker 收到重复工具调用 ID 时复用已完成结果；不同参数复用相同 ID 会被拒绝。
- 进程中断后，未提交结果的调用标记为结果不确定。系统不把它自动当成成功，也不直接重放它；继续前须核对执行记录。这不是跨所有外部系统的“恰好一次”保证。
- Eino **Agent** 回退仍可通过 `engine: eino` 显式选择，旧依赖尚未全量删除。
- Workflow 已使用原生 DAG 和 JSON 检查点。旧 Eino Workflow 二进制检查点不能由新版直接恢复；应先完成旧工作流，或使用原版本完成恢复。新检查点记录图指纹和执行中的节点，拒绝用已修改的图或结果不确定的节点自动续跑。
- 知识库使用自身 Document、切分、Embedding HTTP、向量检索、查询扩展和重排流程。向量与表结构保持兼容，新索引准备成功后在事务中替换旧行。
- 本地测试和源码集成不等于真实模型、真实任务的长时验收。当前尚无拒答率、发现率或成本改进的实测结论。

## 回归

```sh
cd runtime/pi
npm ci --ignore-scripts
npm run build
npm test
cd ../..
go test -race ./internal/agentruntime ./internal/multiagent ./internal/workflow ./internal/knowledge
go test ./...
node --test web/static/js/*.test.cjs
```

SDK 集成测试使用本地模拟模型和无副作用的工具夹具，覆盖 OpenAI／Claude 动作建议、审批编辑、子代理工具边界、流式输出、取消、进程中断及备用模型切换。
