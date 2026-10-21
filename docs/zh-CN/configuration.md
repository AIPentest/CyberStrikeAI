# 配置参考

CyberStrikeAI 的主配置文件是 `config.yaml`。大多数配置也可以在 Web 的“系统设置”中修改，保存后再应用。生产环境中，建议把敏感值放在受控配置系统中，并限制 `config.yaml` 的文件权限。

## 基础配置

```yaml
version: "vX.Y.Z" # 占位符；请使用 config.example.yaml 中当前发布版本的值
server:
  host: 0.0.0.0
  port: 8080
  tls_enabled: true
  tls_auto_self_sign: true
  # 可选：其他可信 Web 集成；Chromium 浏览器插件无需配置
  # cors_allowed_origins:
  #   - https://trusted-integration.example
auth:
  session_duration_hours: 12
log:
  level: info
  output: stdout
```

- `version`：前端展示版本。
- `server.host/port`：Web 服务监听地址和端口。
- `server.tls_*`：HTTPS 配置。生产环境建议使用 `tls_cert_path` 和 `tls_key_path`。
- Chromium 浏览器插件的合法 `chrome-extension://<32位插件ID>` Origin 会被自动识别，无需配置。插件仍需按域授权，并使用密码登录与 Bearer Token 调用 API。
- `server.cors_allowed_origins`：仅供其他可信 Web 集成使用的额外 Origin 精确白名单；不支持 `*`，修改后需重启服务。
- `auth.session_duration_hours`：登录会话有效期（小时）。登录密码由 RBAC 用户管理，首次启动时在控制台输出 `admin` 初始密码。
- `log.output`：可以是 `stdout`、`stderr` 或文件路径，由 `log.level` 控制级别。
- 额外诊断日志默认开启，仅记录 `warn` 及以上（包括重试、连接异常和错误），独立于 `log.level`，不保存普通 `info` / `debug` 日志。保留原有结构化字段、时间、代码位置和 Error 及以上堆栈，不额外采集请求正文或工具输出。
- `log.diagnostic_dir`：默认 `log`，相对于进程工作目录，文件名为 `diagnostic-YYYY-MM-DD.log`（JSON Lines，按服务器本地日期拆分）。只有出现诊断日志时才创建目录和文件；跨天后首次写入切换文件。
- `log.diagnostic_retention_days`：默认 14 天（含当天）；省略或小于等于 0 时使用默认值。每天首次写入时清理此目录内过期的 `diagnostic-日期.log`，不删除其他文件；没有新诊断日志时不执行清理。
- `log.diagnostic_disabled: true`：关闭额外诊断落盘。以上日志配置修改后需重启；目录无法写入时保留原输出，并由 Zap 向 stderr 报告写入失败。

## AI 通道与模型配置

```yaml
ai:
  default_channel: openai-main
  channels:
    openai-main:
      name: OpenAI Main
      provider: openai_compatible
      base_url: https://api.openai.com/v1
      api_key: sk-...
      model: gpt-4.1
      max_total_tokens: 120000
      max_completion_tokens: 16384
      reasoning:
        mode: on
        effort: high
        allow_client_reasoning: true
        profile: openai_compat
    claude-main:
      name: Claude Main
      provider: claude
      base_url: https://api.anthropic.com/v1
      api_key: sk-ant-...
      model: claude-sonnet-4-5
```

`ai` 是推荐的模型配置入口。系统设置页对应路径是 **系统设置 → 基本设置 → AI 通道配置**，保存后写入 `ai.default_channel` 和 `ai.channels`。旧版 `openai` 字段仍保留为兼容运行时字段；加载配置时会确保至少有一个默认通道，并把 `ai.default_channel` 解析后的配置同步到运行时 `openai`。

通道字段：

| 字段 | 说明 |
| --- | --- |
| `ai.default_channel` | 默认通道 ID。新对话、机器人、批量任务和未显式选择通道的请求使用它。 |
| `ai.channels.<id>` | 通道配置。ID 会归一化为小写、数字和短横线，例如 `Qwen_Max` 会变成 `qwen-max`。 |
| `name` | Web UI 展示名。留空时使用通道 ID。 |
| `provider` | `openai_compatible` 或 `claude`。`openai_compatible` 会在运行时映射为 `openai`；`claude` 使用 Eino 原生 Anthropic Messages API。 |
| `base_url/api_key/model` | 必填。Base URL 通常需要包含版本路径，如 OpenAI/兼容网关的 `/v1`。 |
| `max_total_tokens` | 上下文压缩、攻击链构建、多代理摘要等共用的总预算。已知 `context_window` 时会收紧到“窗口 − 输出预留”。 |
| `max_completion_tokens` | 单次模型输出上限；未填时使用默认值，并会被压到已知/假定窗口的一半以内。 |
| `context_window` | 模型原生上下文窗口（token）。填写后运行时会按窗口收紧 `max_total_tokens`；0/省略表示未知。 |
| `context_profile` | `auto`（默认）或 `local`。`local` 面向 32K–256K 本地模型：预算折到窗口的 60%（上限 128K），收紧摘要预留、工具输出截断/清理、用户消息与账本上限，并启用 `tool_search` 与精简常驻工具。数值参数只收紧、不放大，同时按需启用 tool_search/reduction。 |
| `reasoning` | 该通道的默认推理扩展字段。不同网关支持差异较大，异常时先尝试 `mode: off`。 |

### 本地模型（32K–256K）上下文要点

本地模型的有效上下文通常明显小于标称窗口，且不同后端的“模型上限”和“运行时窗口”可能不一致。建议：

1. **先确认运行时窗口**：在 **系统设置 → 基本设置 → AI 通道配置 → 模型上下文窗口** 右侧点击「自动检测」。探测顺序为 Ollama `/api/ps`、`/api/show`，llama.cpp `/props`，LM Studio `/api/v0/models`，vLLM/OpenAI 兼容 `/v1/models`；同时支持反向代理子路径（如 `https://host/ollama/v1`）。检测结果会显示按当前档位折算后的有效预算。检测不到时请手动填写服务端实际配置。
2. **启用本地档位**：把 `context_profile` 设为 `local`。预算按窗口 60% 计算，同时不超过“窗口 − 输出预留”（例如 32K 窗口、默认输出上限 16K 时预算为 16K）；单次输出上限也会被压到窗口的一半以内。摘要更早触发；工具输出截断阈值约为窗口/6（8–32 KB），历史工具结果清理阈值约为预算的 35%；常驻工具收敛到核心集合，内置工具也进入 `tool_search` 动态池，其余工具按需解锁。token 估算会额外放大 10%，抵消本地 tokenizer 与 tiktoken 在中英文上的差异。该档位还会把输出上限字段切换为更通用的 `max_tokens`（Ollama/llama.cpp 等常不识别 `max_completion_tokens`），攻击链、FOFA 语法生成、工作流草稿、HITL 审计等旁路调用同样生效。
3. **不要按标称 1M 规划**：Qwen3.5 等模型原生 262,144，可用 YaRN 扩到 1M，但本地显存与长上下文性能衰减（context rot）会让实际效果明显下降；带工具的 agent 建议把有效预算控制在 64K–128K。
4. **Ollama 必须设置 `num_ctx`**：Ollama 的 `/v1` 兼容端点会忽略请求里的 `num_ctx`，并按显存选择默认窗口（约 4K/32K/256K）；超长时从开头截断，系统提示词和工具定义会先丢失。请用 `OLLAMA_CONTEXT_LENGTH=65536 ollama serve` 或 Modelfile `PARAMETER num_ctx 65536`，重启后用 `ollama ps` 确认 `CONTEXT`。
5. **KV cache 与前缀缓存**：
   - llama.cpp：`--ctx-size 65536 --cache-type-k q8_0 --cache-type-v q8_0 -fa`；`--cache-prompt` 默认开启。`--parallel` 会让每个槽位分摊上下文；检测结果会返回 `context_slots` 并在大于 1 时提示，请按实际 KV 模式（如 `--kv-unified`）确认单请求可用窗口。
   - vLLM：`--max-model-len 65536 --enable-prefix-caching --kv-cache-dtype fp8`；用 `--max-num-seqs` 控制并发 KV 占用。
   - Ollama：`OLLAMA_KV_CACHE_TYPE=q8_0`、`OLLAMA_NUM_PARALLEL=1`；窗口越大 KV cache 越大。
   - LM Studio：在模型设置中显式设置 Context Length，并开启可用的 KV cache 量化。
6. **思考模型**：Qwen3.5 等默认开启 thinking，会在工具循环中消耗大量上下文。可通过通道的 `reasoning.extra_request_fields.chat_template_kwargs.enable_thinking: false` 关闭（vLLM、llama.cpp `--jinja` 支持）；`reasoning.mode: off` 只清理 OpenAI/DeepSeek 推理字段，不一定能关闭本地 chat template 的 thinking。
7. **建议窗口下限**：带 90+ 工具的 agent 在 16K 以下很难稳定运行；32K 需要依赖 `tool_search` 和 `context_profile: local`；64K 以上更接近云端模型的使用体验。
8. **故障转移通道不会重新预算**：`multi_agent.eino_middleware.model_failover_channels` 只切换模型端点，预算仍按主通道计算；请让 failover 通道的可用窗口不小于主通道，否则可能依赖超窗压缩/重试。

对话页的“AI 通道”下拉框会读取已保存通道。请求体中的 `aiChannelId` 非空时仅对本次/本会话运行配置生效，不会把 API Key 发送给模型；为空时跟随 `ai.default_channel`。

常用操作：

- 新增：点击左侧 `+`，填写必填字段后保存。
- 设默认：选中通道后点击“设为默认”，保存并应用后新请求生效。
- 复制：以当前表单内容创建副本，适合为同一服务商配置不同模型。
- 删除：默认通道不能作为批量删除目标；删除后需保留至少一个通道。
- 探活：单通道“测试连接”或左侧“批量探活”会调用模型测试接口，适合验证 Key、Base URL 和模型名。

## Agent

```yaml
agent:
  max_iterations: 12000
  tool_timeout_minutes: 60
  shell_no_output_timeout_seconds: 1200
  workspace_root_dir: ""
  system_prompt_path: ""
```

- `max_iterations`：单代理、多代理主执行器和子代理的默认迭代上限。
- `tool_timeout_minutes`：单次工具最长运行时间。
- `shell_no_output_timeout_seconds`：Shell 长时间无输出时终止。
- `workspace_root_dir`：会话工作区根目录，建议不要设置到系统 `/tmp`。
- `system_prompt_path`：单代理系统提示词覆盖文件。

## HITL

```yaml
hitl:
  default_reviewer: audit_agent
  audit_backend: openai
  retention_days: 90
  tool_whitelist: [read_file, list_dir, glob, grep, tool_search]
  audit_model:
    provider: ""
    base_url: ""
    api_key: ""
    model: ""
```

- `default_reviewer`：`human` 或 `audit_agent`。
- `audit_backend`：`openai`（默认，兼容协议聊天模型）或 `typesafe`（TypeSafe Jev）。
- `tool_whitelist`：全局免审批工具列表，会与会话白名单合并。
- `audit_model`：openai 后端留空复用主模型；typesafe 后端需填写 TypeSafe API Key，不继承主模型密钥。
- `audit_agent_prompt` / `audit_agent_prompt_review_edit`：openai 后端作为聊天提示词；typesafe 后端作为 Jev 的组织策略（`operatorPolicy`）。内置默认提示词与 Jev 问题重复，不会再复制进 state。

更多策略见 [人机协同最佳实践](hitl-best-practices.md)。

## 多代理

```yaml
multi_agent:
  enabled: true
  robot_default_agent_mode: eino_single
  batch_use_multi_agent: false
  eino_skills:
    disable: false
    filesystem_tools: true
    skill_tool_name: skill
```

支持模式：

- `eino_single`：Eino 单代理。
- `deep`：DeepAgent 风格多代理。
- `plan_execute`：规划、执行、重规划。
- `supervisor`：主管代理转交子代理。

`agents_dir` 指向 Markdown 子代理目录。单个代理可在 front matter 中设置 `tools`、`bind_role`、`max_iterations`。

## 工具与 MCP

```yaml
security:
  tools_dir: tools
  tool_description_mode: full
mcp:
  enabled: false
  host: 0.0.0.0
  port: 8081
  auth_header: "X-MCP-Token"
  auth_header_value: ""
external_mcp:
  servers: {}
```

- `security.tools_dir`：内置工具 YAML 目录。
- `tool_description_mode`：`short` 更省 token，`full` 更完整。
- `mcp.enabled`：是否启动独立 HTTP MCP 服务。
- `mcp.auth_header_value`：外部调用 MCP 时的共享密钥，生产环境必须设置。
- `external_mcp.servers`：外部 MCP 联邦配置。

工具 YAML 规则见 `tools/README.md`。

## 知识库

```yaml
knowledge:
  enabled: false
  base_path: knowledge_base
  embedding:
    provider: openai
    model: text-embedding-v4
    base_url: ""
    api_key: ""
  retrieval:
    top_k: 5
    similarity_threshold: 0.4
  indexing:
    chunk_size: 512
    chunk_overlap: 50
    batch_size: 10
```

启用后会注册知识库检索工具，并开放管理接口。详细说明见 [知识库](knowledge-base.md)。

## 数据库

```yaml
database:
  path: data/conversations.db
  knowledge_db_path: data/knowledge.db
```

默认使用 SQLite。`knowledge_db_path` 为空时可复用会话数据库；独立文件更便于迁移知识库。

## 审计与监控

```yaml
audit:
  enabled: true
  retention_days: 15
  max_detail_bytes: 8192
monitor:
  retention_days: 90
```

- `audit` 记录平台操作，不记录对话正文和每次工具调用正文。
- `monitor` 管理工具执行记录保留时间。

## 存储清理

```yaml
storage:
  auto_clean: false
  interval_minutes: 60
  orphan_grace_days: 1
  active_grace_hours: 24
  categories:
    workspace:
      enabled: true
      retention_days: 30
```

清理运行期间产生的磁盘垃圾，入口在「系统设置 → 存储清理」，也可用 `POST /api/storage/cleanup` 触发。

- `auto_clean` 默认关闭：升级后不会在管理员不知情的情况下删除既有数据；关闭时仍可手动预览与清理。
- `interval_minutes` 后台清理间隔，最小 5；`orphan_grace_days` 是会话/项目已删除但目录残留时的最小保留天数；`active_grace_hours` 内的活动会话一律跳过。
- 各类别 `retention_days` 为 0 表示不按保留期清理，但**孤儿目录仍会回收**（会话已删除的目录没有保留价值）。
- 删除不可逆。API 层要求真实删除必须同时传 `dry_run=false` 与 `confirm=true`，省略 `dry_run` 按预览处理。

| 类别 | 默认保留 | 目标目录 |
| --- | --- | --- |
| `workspace` | 30 天 | Agent 工作区 `tmp/workspace` |
| `reduction` | 7 天 | 超长工具输出落盘 `tmp/reduction` |
| `conversation_artifacts` | 30 天 | 摘要与超长输入台账 `data/conversation_artifacts` |
| `plantask` | 30 天 | 多代理计划看板 `skills/.eino/plantask` |
| `c2_artifacts` | 30 天 | C2 回传/上传/下发/payload `tmp/c2`（清理 payload 后下载链接失效） |
| `chat_uploads` | 90 天 | 对话上传附件 `chat_uploads` |
| `workflow_checkpoints` | 7 天 | 工作流断点 `data/workflow-checkpoints` |
| `diagnostic_logs` | 14 天 | 诊断日志 `log/diagnostic-*.log` |

## C2、WebShell、项目

```yaml
c2:
  enabled: true
project:
  enabled: true
  context_checkpoints: true
  # 续接补充包含在事实索引预算内；工作文件候选清单另有最多 4000 字符预算
  fact_index_max_runes: 65000
```

- `c2.enabled`：关闭后不启动 C2 监听器，也不注册 C2 MCP 工具。
- WebShell 连接配置存 SQLite，没有单独的主配置开关。
- `project` 控制跨对话事实黑板注入预算。[项目续聊与成果复用](project-resume.md) 说明脚本登记、测试结果与续接流程。
- `project.context_checkpoints` 默认开启：已绑定项目的会话成功压缩时，将续接摘要自动保存为 `note/context-*`（tentative），按会话及主/子代理分别更新，不覆盖已确认事实。完整原始记录保存为不可变快照，后续可通过 `get_project_fact` 与 `read_file` 找回。关闭此项仅停止自动黑板记录，仍保留原始上下文快照。详见 [长上下文续接留存](blackboard-context-memory.md)。

## 机器人

`robots` 支持个人微信 iLink、企业微信、钉钉、飞书、Telegram、Slack、Discord、QQ。详细配置步骤见 [机器人使用说明](robot.md)。

## 配置修改建议

- 先在测试环境验证模型、MCP、知识库和高风险工具。
- 改动 `tools_dir`、`roles_dir`、`skills_dir`、`agents_dir` 后，检查 Web 页面是否能列出对应资源。
- 生产环境避免开启不需要的 C2、WebShell、终端和外部 MCP。
- 修改敏感配置后，检查审计页面是否有异常登录或配置变更记录。

## 配置应用机制

配置不是所有字段都同等“热更新”。`/api/config/apply` 会做一组协调动作：更新模型配置、工具描述模式、重新注册部分 MCP 工具、初始化或更新知识库、重启机器人连接、按配置启停 C2。这个逻辑在 `internal/handler/config.go` 中由 `ConfigHandler` 协调。

实务判断：

| 配置段 | 应用后通常立即生效 | 需要额外动作 |
| --- | --- | --- |
| `ai.default_channel` / `ai.channels` | 新请求使用解析后的默认或选定通道 | 旧的流式请求不会被强制切换；前端通道列表需要重新读取配置 |
| `openai` | 兼容字段；通常由默认 AI 通道同步 | 新配置优先维护 `ai.channels` |
| `agent.max_iterations` | 新 Agent 任务生效 | 已运行任务按启动时状态继续 |
| `security.tool_description_mode` | 工具重新暴露时生效 | 模型已有上下文不会回滚 |
| `hitl.tool_whitelist` | 新工具调用审批判断生效 | 已挂起审批不自动重判 |
| `knowledge.enabled` | 会尝试初始化/更新组件 | 启用后仍需扫描和索引 |
| `knowledge.embedding` | 检索器/索引器配置更新 | 已有向量通常需要重建索引 |
| `robots` | 会触发连接重启 | 平台回调配置仍需在平台侧正确 |
| `c2.enabled` | 会协调 C2 runtime | 已暴露端口和会话要人工确认 |
| `server.port/tls` | 通常需要重启进程 | 监听地址不是普通热更新 |

## 配置优先级和派生关系

几个字段有“留空复用”的关系：

- `vision.api_key/base_url/provider` 留空时复用 `openai`。
- `hitl.audit_model` 在 `audit_backend=openai` 时留空复用默认 AI 通道；typesafe 后端不继承主模型密钥。
- `knowledge.embedding.base_url/api_key` 留空时复用主模型或 embedding 默认配置。
- `knowledge.retrieval.rerank.base_url/api_key` 留空时复用 embedding/openai。
- `database.knowledge_db_path` 留空时可以复用主会话数据库，但独立文件更利于备份。

这类配置排障时不要只看子配置段，也要看它会回落到哪个上级配置。

## 参数取值建议

| 参数 | 保守值 | 激进值 | 判断依据 |
| --- | --- | --- | --- |
| `agent.tool_timeout_minutes` | 10-30 | 60+ | 扫描工具是否常跑长任务 |
| `shell_no_output_timeout_seconds` | 300-600 | 1200+ | 工具是否长时间静默 |
| `knowledge.indexing.batch_size` | 5-10 | 20+ | embedding 服务批量限制 |
| `knowledge.indexing.rate_limit_delay_ms` | 300-800 | 0-100 | 服务商 RPM 和 429 情况 |
| `retrieval.top_k` | 3-5 | 8-12 | 内容质量和上下文预算 |
| `similarity_threshold` | 0.35-0.45 | 0.5+ | 召回优先还是精度优先 |
| `audit.retention_days` | 15-30 | 90+ | 合规要求和磁盘空间 |
| `monitor.retention_days` | 30-90 | 180+ | 是否需要长周期复盘 |

## 变更前后验证模板

修改配置前记录：

```text
变更目的：
涉及配置段：
预期影响：
回滚方式：
验证接口：
```

修改后验证：

```bash
curl -k https://127.0.0.1:8080/api/auth/validate \
  -H "Authorization: Bearer <token>"
```

再按配置类型验证模型、工具、知识库、C2 或机器人。不要只看 Web 保存成功提示。

## 源码锚点

- 配置结构：`internal/config/config.go`
- 环境变量展开：`internal/config/envexpand.go`
- Web 配置接口：`internal/handler/config.go`
- 路由注册：`internal/app/app.go`
- C2 配置协调：`internal/app/c2_lifecycle.go`
