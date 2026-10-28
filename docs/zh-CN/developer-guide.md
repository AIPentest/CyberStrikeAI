# 开发者指南

本文面向二次开发者，说明项目结构、启动方式、主要扩展点和开发习惯。

## 项目结构

```text
cmd/server/              Web 服务入口
internal/app/            应用组装、路由注册、MCP 工具注册
internal/handler/        HTTP Handler
internal/database/       SQLite 数据访问
internal/security/       认证、限流、Shell 执行
internal/mcp/            MCP Server、外部 MCP 管理
internal/multiagent/     Eino 单代理、多代理、中间件
internal/workflow/       工作流运行时
internal/knowledge/      知识库索引与检索
internal/c2/             内置 C2
internal/project/        项目事实黑板
web/static/              前端 JS/CSS/资源
web/templates/           HTML 模板
tools/                   命令工具 YAML
roles/                   角色 YAML
agents/                  多代理 Markdown 定义
skills/                  Agent Skills
docs/                    项目文档
```

## 启动开发环境

```bash
go run ./cmd/server --config config.yaml
```

前端是静态页面，模板在 `web/templates/`，JS/CSS 在 `web/static/`。修改后刷新浏览器即可验证，多数场景不需要单独前端构建。

## 路由

路由集中在 `internal/app/app.go` 的 `registerRoutes` 中。新增业务接口通常需要：

1. 在 `internal/handler/` 增加 Handler。
2. 在 `internal/database/` 增加必要的数据访问。
3. 在 `internal/app/app.go` 构造并注册路由。
4. 如需对外文档，更新 `internal/handler/openapi.go`。
5. 如需前端调用，更新 `web/static/js/`。

## 一键更新：让安装目录更新它自己的源码

部署现场的日常是"我的 fork 又前进了几十个提交 → 把这套安装带上去 → 重新编译 → 换二进制 → 重启"。
这件事以前只有 `upgrade.sh`，而它写死了别人的仓库：对跑 fork 的人来说"升级"等于"用别人的代码覆盖自己"。
现在这个能力做进了平台自己（`internal/update`），页面、REST 与 CLI 三条入口共用同一份实现，
所以它们给出的拒绝理由不可能各说各话。

### 三种入口

| 入口 | 怎么用 |
|---|---|
| 控制台 | 「系统设置 → 一键更新」（`#system-update` 老深链仍可用）。打开这一区只读本机状态、不联网；点「检查更新」才去 fetch；「一键更新」发起任务并轮询进度；"更新完成后退出进程"在检测到守护进程（launchd/systemd 的启动标记）时默认勾选，重启期间页面守着重连、新进程一应答就自动刷新（会话在内存里，刷新后需重新登录）；另有回滚按钮 |
| REST | `GET /api/system/update`（磁盘现状，不联网）、`POST /api/system/update/check`（fetch 后报告差集）、`POST /api/system/update/apply`（`202` 返回 `job_id`，用 `GET /api/system/update/job` 轮询）、`POST /api/system/update/rollback` |
| CLI | `./cyberstrike-ai -check-update`、`./cyberstrike-ai -update`、`./cyberstrike-ai -update-rollback`。安装目录 = `--config` 所在目录，未给 `--config` 时是当前目录 |

权限是 `update:read`（GET）与 `update:apply`（四个写接口）。`update:apply` 还要求会话是 global（`all` scope）：
一台机器只有一份源码，`assigned`/`own` scope 的账号不该能移动别人正在跑的代码。

### 它从哪里取代码

从**更新源仓库**：一个仓库地址，默认就是官方仓库 `https://github.com/AIPentest/CyberStrikeAI.git`；
更新跟随该仓库**自己的默认分支**（不用配置分支，也不写死 main），所以对一套没做过任何配置的安装，
「从哪更新」永远有一个明确答案。`upgrade.sh` 现在只是这条实现的薄壳：本目录是 git 工作树时它直接调
`./cyberstrike-ai -update`。

### 更换更新源（config.yaml 的 update 段）

更新源只有一个字段：仓库地址。留空 = 官方仓库；想跟随自己的二开、镜像（GitHub 访问不畅时）或别人的仓库，
只改这一处：

```yaml
update:
  repo: https://github.com/AIPentest/CyberStrikeAI.git  # 一个仓库地址；留空/不写 = 官方仓库
```

控制台的「一键更新 → 更新源」区块可以直接编辑并保存这个地址（留空保存 = 回到官方仓库）。
服务端保存时校验地址：只允许 https/http/ssh/git/file:// 与本机绝对路径——git 的 `ext::` 传输会执行命令，一律拒绝。

### 接入非 git 安装（解压/打包装的那类）

一开始用 Release 包解压安装、目录里没有 `.git` 的，可在同一页执行「预览并接入」（不配置也能用默认的官方仓库）：
预览在临时仓库里 fetch，先列出**会被目标版本替换的本机文件**与**会保留的运维者内容**；确认后目录接入
成为 git 工作树（`git init` + 添加 origin + 落地目标分支），被替换的文件全部留底在
`.update-backup/<唯一备份目录>/overwritten/`，运维者内容照旧先暂存再放回，随后重编译二进制。
接入后它就是正常安装，一键更新与回滚都可用了；注意接入前没有 git 历史，因此「接入」本身没有可回滚的
上一提交，第一个回滚点由接入后的第一次更新写下。

### 版本号随代码走

仓库里的 `config.example.yaml` 载着这份代码对应的发布版本（上游随 release 提交一起改），而你的
`config.yaml` 是运维数据、任何一次更新都不会碰它。所以更新在合并完成后会把新代码的版本号写回
`config.yaml` 的 `version` 字段（只动这一行、临时文件 + 原子替换；进度里会有一行「版本号已随新代码
更新：v1.7.20 → v1.7.21」）。重启后页头的版本徽标与静态资源的 `?v=` 都跟代码对上——升级后前端缓存
也因此自动失效。回滚会把版本号一起还原（前提是这期间没人手改过它）；解压安装「接入」时同样同步一次。
若 `version` 的写法无法行级替换（如 JSON 单行、多行标量），写入会安全跳过并提示，而不是产出打不开的
配置——读取走 YAML 解析（认 `version` / `"version"` / `'version'` 三种写法），写入的行匹配与之口径一致。

### 四种拒绝场景

| 场景 | reason | 含义与处理 |
|---|---|---|
| 本地改过**产品源码**（不在 protected 清单里的路径） | `local_source_edits` | 一键更新是"下载"，不是"替你决定冲突"。点名文件，先提交或还原再重试 |
| 分支与远端**分叉**（本地既领先又落后） | `diverged` | 那是合并决策，给出 ahead/behind 数字；手工处理后再点 |
| 本机没有 Go 工具链 | `no_toolchain` | **源码照样更新**，只有二进制没换；欠编译记入 `.update-build-pending`（状态里 `buildPending`，页面照常开放「一键更新」），装好 `go` 再点一次会真的补编译 |
| 这个目录不是 git 工作树 | `not_a_repo`（状态里 `installed: false`） | tarball 安装的目录没有可快进的远端，按发布包方式更新 |

`apply` 是异步的，所以它总是先回 `202`，上表的拒绝出现在轮询到的 `job.failure`（`reason`/`message`/`items`）里；
`rollback` 是同步的，拒绝直接落到状态码（`local_source_edits`、`diverged`、`no_state`、`moved_since_update`、
`no_binary` → 409，其余 → 400）。其他失败（`merge_failed`、`build_failed`、`swap_failed`）带着 git/go 的原始输出；
编译失败时二进制保持原版本，而源码已移动、状态文件已写好，因此仍然可以回滚。

检查、更新、接入、回滚和重启共用安装目录的系统文件锁（`.update-lock`）。遇到
`update_busy` 时等待另一操作完成；进程退出后锁由系统释放，不要删除锁文件来绕过互斥。

### 运维者的内容如何被保留

`roles/ skills/ tools/ agents/ knowledge_base/ data/ log/ venv/ config.yaml .env` 是**运维者的内容**，
判定在 `update.Protected`。合并前，只有本次更新**确实会写到**的那些路径被复制到
`.update-backup/<唯一备份目录>/` 并从工作树移开（你多出来的、上游没动的目录连碰都不碰），合并后再原样放回，
于是"更新代码、留下我的工作"成立。结果里的 `keptContent` 逐个点名被保留的文件——绝不静默丢弃：
当上游与你在同一个文件上都有改动，落地的是你的版本，而这件事写在结果里。
**回滚遵循同一条规则**：`reset --hard` 会把受保护文件上现存的本地改动先暂存、重置后放回（更新前改的
和更新后改的都算，本地删除的保持删除），`keptContent` 同样点名——回滚撤的是代码，不是你的内容。

### 回滚

`./cyberstrike-ai -update-rollback` 或页面按钮：`git reset --hard` 回到那次更新前的提交，
换过二进制的那次会把 `cyberstrike-ai.prev` 放回原位，随后删除 `.update-state.json`。
编译失败的那次更新从没换过二进制，所以它的回滚只搬源码——磁盘上的二进制本来就属于要回去的那个提交。
它会拒绝的情形：没有更新记录（`no_state`）、需要换回却没有留下 `cyberstrike-ai.prev`（`no_binary`）、
记录里的提交在本仓库不存在（`bad_state`）、磁盘上的二进制不是记录里与该提交配对的那一个（`binary_changed`）、
**HEAD 自那次更新之后又动过**（`moved_since_update`：回滚只该撤到更新前，不该顺手抹掉之后的工作）、
以及存在本地源码改动（`local_source_edits`）。

回滚开始前会写入 `.update-rollback-pending.json`。中断后再次执行回滚会继续恢复；页面的
`rollbackPending` 提示会暂停新更新。保留恢复记录、备份目录和 `.prev`，不要手动删除它们。
如果记录中的二进制哈希无法与现场对应，会拒绝覆盖并保留现场。

### 重启的两种情形

进程只有在请求显式带 `restart: true`、且启动时装配了重启钩子时才会优雅退出（`Shutdown` 后退出码 0）。
能不能被拉起来取决于外部守护（systemd、`run.sh`），页面如实这么写，而不是承诺一次可能不发生的启动；
没装配钩子时要求重启是 `400`，而不是先停掉服务再自称重启了。

**待生效状态是持久的、可补重启。** 状态接口对比"启动时记下的二进制身份（大小 + 纳秒 mtime）"与磁盘
现值：更新、回滚或 CLI 换过二进制而没重启时，`needsRestart` 为真、`binaryBuiltAt` 给出构建时间，
控制台顶部出现常驻横幅与「立即重启服务」（`POST /api/system/update/restart`；没有待生效版本时
`409 nothing_pending`、有任务在跑 `409`、没钩子 `400`）。`supervised` 字段来自环境标记（launchd 的
`XPC_SERVICE_NAME`、systemd 的 `INVOCATION_ID`/`JOURNAL_STREAM`），只用来决定勾选框的默认值。

**重启后页面自己回来。** 重启期间控制台换成自恢复视图，每 2 秒探一次状态接口：旧进程还在应答（200）
就继续等；连不上说明正在退出；新进程接客但对旧会话只回 401——这就是"重启已完成"的判据，页面随即
`location.replace` 回 `#system-update` 整页刷新。离开控制台则静默停表，不把已经走开的用户拽回来。

## 数据库

默认 SQLite。新增表或字段时：

- 将迁移逻辑放到数据库初始化或对应模块迁移函数。
- 保持向后兼容，避免破坏已有 `data/conversations.db`。
- 添加针对迁移和核心查询的单测。

## 新增工具

命令工具优先通过 `tools/*.yaml` 增加，不必改 Go 代码。需要 Go 内置工具时：

- 在合适模块注册 MCP Tool。
- 定义清晰 `InputSchema`。
- 处理超时、错误、审计和 HITL 上下文。
- 避免把高风险操作默认免审批。

工具 YAML 规则见 `tools/README_CN.md`（English: `tools/README.md`）。

## 新增角色

角色通过 `roles/*.yaml` 管理。常见字段包括名称、描述、系统提示词和工具列表。角色应遵循最小工具集原则，不要把所有工具默认交给专用角色。

## 新增子代理

多代理子 Agent 放在 `agents/*.md`。Front matter 示例：

```yaml
---
name: Vulnerability Triage
id: vulnerability-triage
description: 对漏洞线索进行验证、定级和修复建议整理
tools:
  - nmap
  - nuclei
bind_role: 综合漏洞扫描
max_iterations: 200
---
```

正文是系统提示词。主代理可使用固定文件名或 `kind: orchestrator`。

## 新增 Skill

Skill 放在 `skills/<name>/SKILL.md`。用于提供专题能力、流程说明或附属资料。详见 [Skills 指南](skills-guide.md)。

## 前端开发

前端代码按功能拆分在 `web/static/js/`。新增页面或模块时：

- 复用现有 `apiFetch`、modal、通知、i18n 工具。
- 同步更新 `web/static/i18n/zh-CN.json` 和 `en-US.json`。
- 避免把敏感 Key 放到前端。
- 高风险按钮要有确认和清晰状态反馈。

i18n 规范见 [前端国际化方案](frontend-i18n.md)。

## OpenAPI

`internal/handler/openapi.go` 维护内置 OpenAPI 输出。新增公开接口后建议同步补：

- path
- method
- summary/description
- requestBody
- responses
- security

这样 `/api-docs` 才能反映最新接口。

## 开发习惯

- 优先保持现有模块边界。
- 大模型、外部 API、文件系统、Shell 相关改动必须考虑超时和错误路径。
- 高风险能力要接入 HITL 或至少有清晰审计。
- 代码变更后运行相关包单测。

## 新增业务模块的完整配方

不要只加一个 Handler。完整模块通常要考虑：

1. 数据模型：是否需要 SQLite 表和迁移。
2. Handler：HTTP 参数、错误码、分页、过滤。
3. Audit：管理动作是否要审计。
4. Monitor：如果会执行长任务，是否要记录执行状态。
5. MCP：是否要暴露给 Agent。
6. HITL：MCP 工具是否有审批边界。
7. OpenAPI：是否更新 `/api/openapi/spec`。
8. Frontend：是否需要 i18n、状态、空态、错误提示。
9. Tests：数据库、handler、边界条件。
10. Docs：配置、使用、排错和安全影响。

少做其中一项，后面通常会以“用户看不懂”“Agent 调错”“接口没人会用”的形式返工。

## Handler 错误设计

建议错误响应保持：

```json
{
  "error": "machine_readable_code",
  "message": "给用户看的说明"
}
```

不要只返回 Go error 字符串。前端需要稳定字段，用户需要可操作建议，日志需要详细错误。

## 长任务设计

扫描、索引、批量任务、C2 等都可能长时间运行。设计时要回答：

- 是否能取消？
- 是否能查询进度？
- 失败后能否重试？
- 结果写在哪里？
- 页面刷新后状态是否还在？
- 是否会阻塞 HTTP 请求？

如果答案是否定的，应考虑接入任务表、事件流或监控模块。

## 测试优先级

最值得补测试的地方：

- 配置热应用。
- HITL 审批分支。
- Shell 超时和无输出。
- 外部 MCP 失败恢复。
- 知识库索引和检索后处理。
- WebShell 编码和系统识别。
- SQLite 迁移兼容。

这些地方比普通 getter/setter 更容易出现真实用户故障。

### 恢复证据与 MCP 编辑启停状态

自更新恢复在改写文件前验证 JSON 字段名称/类型、重复键、提交与哈希配对及实际二进制文件。旧版纯文本待编译标记仍受支持，但关联 State 也必须通过验证。记录不完整、矛盾或格式不受支持时会拒绝恢复并保留文件与恢复证据。不要仅为绕过报错而删除标记，应先核对源码提交、当前二进制和旧二进制/备份的配对。

编辑已有外部 MCP 时，省略启停字段会保留当前启用/停止状态。显式 `disabled` 或 `external_mcp_enable` 可改变启停状态，`disabled: true` 优先；新建 MCP 默认启用。启动/停止按钮同时持久化对应的 disabled 状态，避免编辑或应用重启悄悄启用已停止的服务。
