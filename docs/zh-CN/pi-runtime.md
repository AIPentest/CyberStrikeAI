# Pi 原生运行时

CyberStrikeAI 仅使用 Pi 1.5.2（`@earendil-works/pi-coding-agent`、`pi-ai`），Go 保留业务、权限、工具执行和证据管理。

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

`worker_path` 是管理员的显式覆盖，适合开发或独立打包。留空使用源码指纹校验后的构建包。

源码安装拉取 Worker 修复后，请在该安装目录执行 `bash scripts/build-pi-runtime.sh` 再重启服务。如果设置了 `agent_runtime.worker_path`，请确认它指向新构建。保留原有会话文件：Worker 会在发送 OpenAI 兼容请求时整理被插话打断的工具批次。

模型请求使用 HTTP/1.1，遵循服务进程环境里的 `HTTP_PROXY`、`HTTPS_PROXY` 和 `NO_PROXY`。重定向只允许在原站点内跳转，避免模型凭据外传。

## 执行边界

- 聊天、机器人、批量任务、工作流 Agent 节点通过统一调度入口进入 Pi。正式单代理路由为 `/api/agent`、模式值为 `single`；旧路由与模式只做输入兼容，执行始终使用 Pi。
- Go 管理每次调用的 Worker，支持流式事件、工具回调、补充指令与取消；同一进程中的相同会话存储串行访问，不同会话独立运行。
- Worker 使用显式模型、内存凭据和资源加载器，不自动发现个人 Pi 配置、全局插件或默认文件／Shell 工具。
- 工具较多时使用 Pi 原生 tool_search 按需加载，减少模型一次接收的工具定义。TaskCreate／Update／Get／List 和 write_todos 继续写入现有对话任务板。
- MCP、文件和 Shell 工具回到 Go，经角色过滤、HITL 和现有执行监控处理。子代理工具集合受主代理集合约束。
- Deep 使用任务委派；Supervisor 提供专家委派和 exit；Plan-Execute 使用计划记录与执行子代理。最终文本仍需通过既有证据验收。
- Pi 原生重试及虚拟模型路由承接聊天备用渠道。认证／配置错误不能用无界“继续”消除。
- 上下文窗口和输出上限沿用渠道设置，包括 local profile；输入预算用于 Pi 压缩阈值。压缩后的模型上下文和完整会话记录分别保留。

## 数据与兼容

- Pi Session 是模型会话及压缩记录的权威来源；SQLite 保存业务状态、工具记录和交付证据。两者通过会话和执行 ID 关联。
- 每次实际调用都有独立的运行期传输 ID；模型复用自身的 tool-call ID 不会吞掉新调用。同一个传输 ID 重发时复用已完成结果；不同参数复用同一个传输 ID 会被拒绝。
- 进程中断后，未提交结果的调用标记为结果不确定。系统不把它自动当成成功，也不直接重放它；继续前须核对执行记录。这不是跨所有外部系统的“恰好一次”保证。
- Eino 执行内核、模型适配、桥接和依赖已全部删除。`engine` 只接受 `pi`；旧 `eino_skills`／`eino_middleware` 配置可迁移读取，保存使用 `skills`／`runtime`，旧 callbacks 设置删除。
- Workflow 已使用原生 DAG 和 JSON 检查点。旧 Eino Workflow 二进制检查点不能由新版直接恢复；升级前应完成旧工作流，或导出材料后建立新的 Pi 任务。新检查点记录图指纹和执行中的节点，拒绝用已修改的图或结果不确定的节点自动续跑。
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

2026-11-11 已用现有默认模型完成一次隔离回声工具闭环：模型调用 echo，Go 执行后返回 PONG，Pi 正常结束。该验证只使用合成消息和回声工具，未执行安全测试或更改生产服务；不代表长任务、全部模型渠道或效果指标已验收。
