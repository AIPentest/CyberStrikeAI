# 工具执行治理

[返回中文文档](README.md)

本文说明 CyberStrikeAI 对长时间工具、MCP 阻塞、大输出、取消和恢复上下文的治理策略。目标是让 Agent 保持标准工具语义，同时避免工具卡死、上下文爆炸、数据库膨胀或恢复时重新注入历史大输出。

## 设计目标

- **Agent 不被工具绑死**：工具调用可以很慢，但当前 runner 只等待有限时间。
- **长任务可继续观察**：超时返回 `execution_id`，后续可用 `wait_tool_execution` 多轮等待。
- **用户和 Agent 都能取消**：当前会话结束或用户停止任务时，会取消仍在运行的工具。
- **数据库与 Agent 视图一致**：DB 保存的是 Agent 实际拿到的兜底后结果，不再保存另一份原始大输出。
- **恢复不会撑爆上下文**：续跑使用 model-facing trace；历史异常大 tool trace 恢复时也会再次裁剪。
- **外部 MCP 有隔离保护**：按 server 限并发、按全局限并发，并对连续失败的 server 熔断。

## 执行模型

普通工具调用仍然对 Eino/Agent 表现为一次标准 tool call，但底层执行分为两段：

```text
Agent 调用工具
  -> ExecutionService 创建 execution
  -> worker 执行真实 MCP/工具调用
  -> Agent bounded wait
       -> 完成：返回工具结果
       -> 未完成：返回 execution_id，worker 继续后台运行
```

这解决了 MCP server、`exec`、`sqlmap`、`nmap`、`nuclei` 等长任务阻塞当前 runner 的问题。

## 工具状态语义

| 状态 | 含义 |
|---|---|
| `queued` | execution 已创建，等待 worker 或并发槽位 |
| `running` | worker 正在执行 |
| `background_running` | 前端展示状态，表示本轮 Agent 已停止等待，但后台仍在跑 |
| `completed` | 本次 tool call 本身已完成 |
| `failed` | 工具真实失败 |
| `cancelled` | 用户、Agent 或会话清理主动取消 |
| `hard_timeout` | 超过硬超时，被系统终止 |
| `orphaned` | 重启/异常后发现 DB 中仍是 running，但运行时已无对应 worker |

注意：`wait_tool_execution` 到达 `timeout_seconds` 时，如果目标 execution 仍在运行，**这次 wait 调用本身是完成的观察动作**，不是工具执行失败。返回体会说明目标仍为 `running`，前端不应显示为红色失败。

## 控制工具

| 工具 | 用途 |
|---|---|
| `get_tool_execution` | 读取 execution 当前状态 |
| `wait_tool_execution` | 等待指定 execution 一段时间 |
| `cancel_tool_execution` | 主动取消指定 execution |

`get_tool_execution` 与 `wait_tool_execution` 支持返回运行中输出预览：

- `include_partial_output`：是否返回 partial output，默认 `true`。
- `partial_output_max_bytes`：本次返回的尾部预览上限，默认 `4096`，最大 `65536`。

partial output 是“已产生输出的有界预览”，不等同于最终 `result`。最终 `result` 仍只在工具结束时写入 canonical execution 记录；不支持流式输出的工具不会返回 partial 字段。

典型流程：

```text
1. 调用 exec/sqlmap/nmap 等长任务
2. 超过 tool_wait_timeout_seconds 后拿到 execution_id
3. Agent 可继续推理、改用其他工具，或调用 wait_tool_execution
4. 仍未完成时可继续等待，或调用 cancel_tool_execution
```

`tool_wait_timeout_seconds` 适用于内部 MCP、外部 MCP，以及 Eino filesystem 的流式 `execute`。Eino 的 `ls/read_file/write_file/edit_file/glob/grep` 等非流式 filesystem 工具会写入 execution 监控记录，但不作为后台 worker 做软等待续跑。

## 取消和会话清理

- 用户点击“停止任务”时，会取消当前会话仍在运行的工具。
- 会话正常结束后，会批量取消当前会话仍 `running` 的工具。
- “中断并继续”类流程不会做会话级批量取消，以免误杀后续需要等待的 worker。
- 取消只针对当前 conversation 绑定的 execution，不会误杀其他会话的工具。

## 外部 MCP 隔离

外部 MCP 可能因为远端 server 卡住、断连或返回异常而拖慢 Agent。系统提供三层保护：

| 能力 | 配置 | 说明 |
|---|---|---|
| 单 server 并发限制 | `external_mcp_max_concurrent_per_server` | 同一个外部 MCP server 同时运行的工具数 |
| 全局并发限制 | `external_mcp_max_concurrent_total` | 所有外部 MCP 工具总并发 |
| 熔断 | `external_mcp_circuit_failure_threshold` / `external_mcp_circuit_cooldown_seconds` | 单 server 连续失败后短期快速失败，避免反复打坏 server |

推荐默认：

```yaml
agent:
  external_mcp_max_concurrent_per_server: 2
  external_mcp_max_concurrent_total: 16
  external_mcp_circuit_failure_threshold: 3
  external_mcp_circuit_cooldown_seconds: 60
```

## 输出兜底

系统使用 `multi_agent.eino_middleware.reduction_max_length_for_trunc` 作为统一工具结果上限。当前示例配置为 50000 bytes。

```yaml
multi_agent:
  eino_middleware:
    reduction_enable: true
    reduction_max_length_for_trunc: 50000
```

兜底覆盖：

| 渠道 | 行为 |
|---|---|
| Agent 实际拿到的工具结果 | 使用兜底后的 canonical result |
| DB/监控存储 | 保存同一份 canonical result |
| `get_tool_execution` / `wait_tool_execution` | 读取同一份 canonical result |
| Eino `execute` / filesystem 监控记录 | 完成记录前统一兜底 |
| 非流式 `exec` stdout/stderr | 源头 bounded buffer |
| 流式 `exec` stdout/stderr | 推送给前端的累计输出也受上限控制 |
| PTY 执行路径 | 同样受上限控制 |
| 前端详情弹窗 | 额外有 UI 展示截断保护 |

触发上限后，完整输出先写入本地 trunc 文件，Agent 侧只保留计入预算的 `<persisted-output>` 预览（含绝对路径）。因此阈值为 50000 时，上下文文本不会超过该上限。

示例：

```text
<persisted-output>
Output too large (200000). Full output saved to: /path/to/tmp/reduction/conversations/<id>/trunc/<execution_id>
Use read_file with offset/limit to read parts of the file.
Preview (first …):
…

Preview (last …):
…

</persisted-output>
```

当前策略是「全文落盘 + 上下文预览」：超过 `reduction_max_length_for_trunc` 时，完整输出写入本地文件（默认 `tmp/reduction/conversations/<会话ID>/trunc/<execution_id>`），Agent/DB/监控拿到的是带绝对路径的 `<persisted-output>` 预览；可用 `read_file` 按 offset/limit 回读全文。

## DB 与恢复上下文

新执行结果的写入路径如下：

```text
工具完成
  -> NormalizeToolResultForStorage
  -> 写入内存 execution
  -> 写入 DB
  -> 返回给 Agent
```

因此正常情况下，DB 中保存的就是 Agent 拿到的结果。

续跑恢复时，系统使用 `LastAgentTraceInput` 中的 model-facing trace，也就是实际送入 ChatModel 的消息快照，而不是原始事件流累计。恢复入口还会对历史 tool 内容再次应用上限，防止以下情况撑爆上下文：

- 升级前 DB 已经存过原始大输出。
- 手工迁移或导入的数据绕过了当前写入路径。
- 配置从更大阈值改成 50000。
- 未来某条旁路写入漏掉 canonicalize。

## 关键配置建议

长任务场景推荐：

```yaml
agent:
  max_iterations: 800
  tool_timeout_minutes: 60
  tool_wait_timeout_seconds: 30
  external_mcp_max_concurrent_per_server: 2
  external_mcp_max_concurrent_total: 16
  external_mcp_circuit_failure_threshold: 3
  external_mcp_circuit_cooldown_seconds: 60
  shell_no_output_timeout_seconds: 1200

multi_agent:
  eino_middleware:
    reduction_enable: true
    reduction_max_length_for_trunc: 50000
```

参数说明：

| 参数 | 建议 | 说明 |
|---|---:|---|
| `max_iterations` | `300-1000` | 太大等于放弃循环保护 |
| `tool_timeout_minutes` | `60` | 单次工具硬超时，适合 sqlmap 等长任务 |
| `tool_wait_timeout_seconds` | `30-60` | Agent 本轮等待上限，到时返回 `execution_id` |
| `shell_no_output_timeout_seconds` | `600-1200` | 连续无输出时终止，防止静默挂死 |
| `reduction_max_length_for_trunc` | `50000` | 工具结果统一上限 |

不建议把 `tool_wait_timeout_seconds` 设置得很大。长任务应由 worker 后台跑，Agent 通过 `execution_id` 继续观察，而不是一轮等待数分钟。

## 测试建议

可以用以下对话测试长任务语义：

```text
调用 exec 执行 sleep 120；如果超过 10 秒还没完成，不要一直等，告诉我 execution_id，然后调用 wait_tool_execution 等 5 秒；如果仍未完成，再调用 cancel_tool_execution，最后说明状态。
```

可以用以下命令测试大输出兜底：

```text
调用 exec 执行：python3 - <<'PY'
print("A" * 200000)
PY
然后展示工具结果长度和是否包含截断提示。
```

预期：

- 初始长任务会返回 `execution_id`，状态为 `running` 或前端展示 `background_running`。
- `wait_tool_execution` 等待到上限但目标未完成时，本次 wait 调用不应显示为执行失败。
- 大输出结果不会超过 `reduction_max_length_for_trunc`。
- DB、监控详情、Agent 继续推理看到的是同一份兜底结果。

## 当前边界

- 外部 MCP 的远端 server 内部如何采集输出不由 CyberStrikeAI 控制；CyberStrikeAI 会在结果进入本系统后统一兜底、限并发和熔断。
- 超长工具输出会在截断前写入本地 `tmp/reduction/.../trunc/<id>`（或 `reduction_root_dir`），bounded result 中包含可 `read_file` 的绝对路径。

## 本轮任务的进程生命周期

每轮任务有独立 `runId`，本地进程与异步 MCP worker 都通过 context 保留归属。取消 context、工具返回、SSE 断线不等于资源已回收。

- `exec ... &` 与 Eino 后台执行立即返回，但仍属于本轮任务；无任务归属的后台启动被拒绝。前台工具退出时清理遗留子进程，跨工具调用运行的任务应使用显式后台入口。
- 完成、失败、超时收尾、用户停止、服务正常关闭都会封闭启动入口、取消 worker、终止进程并等待回收。“中断并继续”保留本轮归属。
- Unix 首先给进程组 3 秒退出宽限期，再强制终止并验证；内核隔离、守护进程退出与 worker 收尾各有有界等待。清理期间仍占用本轮任务；失败保留 `cleanup_failed`、进程组/worker ID，每 15 秒巡检重试。
- 工具 worker 的登记与任务关闭互斥；即使 MCP 使用 `WithoutCancel`，任务也会等待其退出。旧轮次的延迟回调无法接管新轮次的进程或提交新 worker。

### 操作系统隔离和崩溃回收

| 平台 | 实现 | 崩溃回收与限制 |
| --- | --- | --- |
| Linux，已配置 cgroup v2 | 每轮创建独立 cgroup，使用 `clone3(CLONE_INTO_CGROUP)` 在创建时归组；配置进程数、内存、CPU 限额 | 独立守护进程在主程序管道 EOF 后写 `cgroup.kill`，验证 `populated=0`；启动时独占委派根并回收遗留任务组。`setsid` 不会脱离 cgroup |
| Windows | 每轮 Job Object，守护进程先加入 Job；任务通过原子父进程属性继承 Job，避免启动后再分配的竞态 | `KILL_ON_JOB_CLOSE`、主动 `TerminateJobObject` 和活跃进程数验证；进程数、内存与可选 CPU 限额 |
| macOS／未配置 cgroup 的 Unix | 独立进程组和守护进程，子进程在登记确认前通过管道等待 | 主程序被强杀后，管道 EOF 触发整组清理；无法限制主动 `setsid` 逃逸，不能作为强隔离部署 |

守护进程自身异常退出时，仍存活的宿主会终止相应资源；IPC 有超时。Linux 的遗留回收依据独占目录与随机任务标识，不重放历史 PID。

这属于生命周期隔离，不是针对同权限恶意代码的完整安全沙箱。能够修改 cgroup、取得外部父进程句柄或杀死宿主与守护进程的程序仍需要容器/不同操作系统身份及权限策略限制。Linux 容器部署应使用 init 回收孤儿进程。`required` 模式在缺少相应内核能力或权限时拒绝运行，不会悄悄降级。

### 配置与上线

默认 `auto` 在 Windows 使用 Job Object；Unix 未指定 cgroup 根时使用进程组守护。Linux 生产部署需配置独立的 systemd 服务，设置 `Delegate=cpu memory pids`、`KillMode=control-group` 和 `Restart=on-failure`，将以下严格隔离配置 **合并到现有配置**：

```yaml
security:
  process_isolation:
    mode: required
    cgroup_root: auto
    max_processes: 256
    memory_max_bytes: 2147483648
    cpu_quota_micros: 200000
```

需要 Linux 5.14+、cgroup v2、允许 `clone3`，以及 `Delegate=cpu memory pids`。`cgroup_root: auto` 用于 systemd 的独立委派单元；也可指定受控的绝对路径，禁止使用整台主机的层级根。配置改变需要重启。Windows 留空 `cgroup_root`，可以使用 `required`；macOS 的 `required` 会明确报错。

服务启动时会真正创建并回收一个探测进程，验证权限和内核支持；也可在不启动 HTTP/MCP 服务的情况下单独运行：

```sh
./cyberstrike-ai --check-process-isolation -config config.yaml
```

输出 `cgroup_v2`、`windows_job` 或 `process_group_watchdog`。运行/历史任务 API 的 `isolationBackend` 字段提供每轮实际采用的后端。systemd 的 `KillMode=control-group` 和失败重启为整个服务额外兜底；它与每任务 cgroup 配合使用。

### 远端 MCP 取消

普通 MCP 取消通知不提供远端退出回执。适配器可实现 `ExternalCancellationConfirmer`，使用服务端任务状态、租约或取消回执确认结束。没有确认时，不再标成“已终止”：工具记录持久化为 `orphaned` 并说明原因；本地 worker 和进程清理完成后，任务历史保留 `cleanup_unconfirmed`，响应明确提示远端状态待确认。它不是自动重试成功，也不是远端零残留保证。仍未返回的本地 worker 则保持 `cleanup_failed` 并继续追踪。

### 回归验证

```sh
go test -race ./internal/processguard ./internal/mcp ./internal/handler ./internal/security
```

`Process isolation` GitHub Actions 工作流还会执行 Linux cgroup 集成测试，命令直接保存在 `.github/workflows/process-isolation.yml` 中。该测试使用一次性、私有 cgroup 命名空间的 Docker 容器，不挂载宿主 cgroup 或 Docker socket。测试覆盖宿主 `SIGKILL`、启动登记竞态、`setsid`、资源限额、启动时遗留回收、worker 取消和远端未确认状态。Windows Job Object 测试已纳入跨平台 CI；交叉编译不等于 Windows 实机测试。

参考：[Go os/exec](https://pkg.go.dev/os/exec)、[Linux cgroup v2](https://cdn.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)、[Windows Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)、[MCP cancellation](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation)。
