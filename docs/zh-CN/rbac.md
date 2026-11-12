# CyberStrikeAI RBAC 使用与管理指南

[English](../en-US/rbac.md)

CyberStrikeAI 是可执行 Agent、MCP、WebShell、C2 和批量任务的安全自动化平台。RBAC 不仅控制页面是否可见，还会贯穿 HTTP API、资源查询、Agent 上下文、内置/外部 MCP 工具、后台任务和机器人执行链路。

---

## 一、先区分两种“角色”

| 概念 | 管理入口 | 作用 |
|------|----------|------|
| **平台角色（RBAC Role）** | 左侧 **平台权限** | 决定用户能调用哪些功能、能访问哪些资源 |
| **AI 测试角色（Agent Role）** | 左侧 **角色** / `roles/*.yaml` | 决定 Agent 的提示词、测试方法和可选工具集合 |

AI 测试角色不是安全授权边界。即使选择了“渗透测试”角色，用户仍必须拥有对应的平台权限；反过来，RBAC 有权限也不会自动改变 Agent 提示词。

---

## 二、授权模型

一次访问同时满足以下条件才会放行：

```text
有效账号
  + 路由/工具所需 permission
  + 该 permission 对应的 scope
  + 目标资源 owner / 显式授权 / 父资源继承
  + 全局操作的额外限制
```

处理链路：

1. 登录后签发 Bearer Token，会话包含用户、角色、权限和逐权限 Scope。
2. HTTP 中间件把路由映射为权限，例如 `GET /api/projects` → `project:read`。
3. 对带资源 ID 的请求继续校验 owner、显式资源授权或父资源继承。
4. Agent 启动时把不可变 Principal 写入 `context.Context`。
5. 内置 MCP 工具根据工具和参数再次检查权限与资源；外部 MCP 也有独立限制。
6. 拒绝事件写入 RBAC/审计日志。

前端隐藏按钮只用于改善体验，不构成安全边界；真正的拒绝发生在服务端。

---

## 三、内置平台角色

| 角色 | Scope | 默认能力 |
|------|-------|----------|
| **管理员 `admin`** | `all` | 所有已知权限，包括 RBAC、配置、终端、审计删除和全局定义管理 |
| **操作员 `operator`** | `assigned` | 日常读写与执行能力；不含 RBAC、核心配置、终端、审计管理、外部 MCP 执行和部分全局定义写权限 |
| **审计员 `auditor`** | `all` | 各模块只读权限和 `audit:read`，不执行写操作 |
| **只读用户 `viewer`** | `assigned` | 各模块只读权限，仅查看被授权范围 |

系统角色不可修改或删除，升级时会按当前版本的权限目录重新构建授权，避免旧版本残留权限。需要不同组合时创建自定义角色。

内置管理员账号 `admin` 是保底管理入口，不能停用、删除或移除其系统管理员角色。通过界面或 API 提交此类更新都会被拒绝，原账号资料、权限和有效会话保持不变；仍可修改显示名称、密码以及保留 `admin` 角色的其他角色配置。普通账号的启用状态和角色仍可按需调整。此保护不自动修复历史上已丢失的角色绑定。

没有分配任何角色的账号仍可登录，但基本没有业务权限；不要依赖“无角色”作为完整岗位配置。

---

## 四、权限命名与目录

权限使用 `模块:动作` 命名。常见动作：

- `read`：查看、列表、查询、导出。
- `write`：创建、更新、执行或管理。
- `delete`：删除。
- `execute`：执行 Agent、终端、工作流或特定能力。

当前权限按模块分组如下；运行版本的权威目录以“平台权限”页面或 `GET /api/rbac/metadata` 为准。

| 模块 | 权限 |
|------|------|
| 账号 | `auth:self` |
| 仪表盘 | `dashboard:read` |
| 对话 | `chat:read`、`chat:write`、`chat:delete` |
| Agent | `agent:execute`、`agent:local-execute` |
| HITL | `hitl:read`、`hitl:write` |
| 任务 | `tasks:read`、`tasks:write`、`tasks:delete` |
| 项目 | `project:read`、`project:write`、`project:delete` |
| 漏洞 | `vulnerability:read`、`vulnerability:write`、`vulnerability:delete` |
| WebShell | `webshell:read`、`webshell:write`、`webshell:delete` |
| C2 | `c2:read`、`c2:write`、`c2:delete` |
| MCP | `mcp:read`、`mcp:execute`、`mcp:write`、`mcp:external:execute` |
| 一键更新（本机源码） | `update:read`、`update:apply` |
| 知识库 | `knowledge:read`、`knowledge:write`、`knowledge:delete` |
| Skills | `skills:read`、`skills:write`、`skills:delete` |
| Markdown Agents | `agents:read`、`agents:write`、`agents:delete` |
| AI 测试角色 | `roles:read`、`roles:write`、`roles:delete` |
| 工作流 | `workflow:read`、`workflow:execute`、`workflow:write`、`workflow:delete` |
| 系统配置 | `config:read`、`config:write` |
| 终端 | `terminal:execute` |
| 审计 | `audit:read`、`audit:delete` |
| RBAC | `rbac:read`、`rbac:write` |
| 通知 | `notification:read`、`notification:write` |
| 机器人 | `robot:read`、`robot:write` |
| 文件 | `files:read`、`files:write`、`files:delete` |
| 攻击链 | `attackchain:read`、`attackchain:write` |
| 网络空间测绘 / 信息收集 | `fofa:execute` |
| OpenAPI | `openapi:read` |
| 执行监控 | `monitor:read`、`monitor:write`、`monitor:delete` |

特殊权限说明：

- `fofa:execute` 为兼容旧版本保留权限名，现在保护 **信息收集** 页中的 FOFA、ZoomEye、Quake、Shodan 查询。

- `agent:execute` 允许运行 Agent，但不自动允许本地文件系统、Shell 或任意配置命令。
- `agent:local-execute` 是本地执行兜底权限，应仅授予可信操作员。
- `mcp:execute` 用于访问认证后的 MCP HTTP 入口。
- `mcp:external:execute` 用于 Agent 调用外部 MCP 工具，当前还要求该权限的 Scope 为 `all`。
- 管理外部 MCP 配置使用 `mcp:write`，与执行外部工具是两项权限。
- 一键更新是**两项**：`update:read` 看本机安装的版本与更新源差集；`update:apply` 拉取更新源仓库（默认官方仓库）
  的默认分支、重编译并换掉平台二进制。写侧还要求会话是 `all` scope——一台机器一份源码，`assigned`/`own` 不该能移动别人正在跑的代码。
- `robot:write` 管理机器人配置和测试入口；机器人聊天本身使用绑定用户或服务账号的业务权限。

---

## 五、资源 Scope

每个角色包含一个 Scope：

| Scope | 含义 | 适合场景 |
|-------|------|----------|
| `all` | 访问该权限覆盖的所有资源 | 管理员、全局审计员 |
| `assigned` | 访问管理员指定的资源及系统支持的父资源继承范围 | 项目成员、指定资产操作员 |
| `own` | 以本人创建/归属资源为主；部分资源仍可通过显式授权或父资源关系访问 | 个人工作区、机器人独立身份 |

权限和 Scope 是绑定在一起计算的。一个用户可拥有多个角色，权限取并集；**同一个权限**的 Scope 取最宽值：

```text
all > assigned > own
```

示例：

- “全局审计”角色：`project:read` + `all`
- “个人项目编辑”角色：`project:write` + `own`

最终结果是：

```text
project:read  → all
project:write → own
```

全局读取不会把无关的写权限扩大为全局写入。服务端授权必须使用 `ScopeFor(permission)`，不能使用用户的最宽总 Scope。

### 全局对象限制

部分对象是进程级共享定义，没有 owner。即使用户拥有 `write`，若该权限 Scope 不是 `all`，服务端仍拒绝修改。例如：

- AI 测试角色、Skills、Markdown Agents。
- 外部 MCP 配置。
- 机器人配置。
- 工作流定义。
- 知识库写操作（搜索除外）。
- HITL 全局白名单、默认审核方和审计策略。
- C2 Profile 写操作。
- 一键更新的写操作（`check`/`apply`/`rollback`，即 `update:apply`）。
- 部分全局监控统计。

---

## 六、资源归属、显式授权与继承

可在“平台权限 → 成员详情 → 资源授权”中给用户分配资源。当前可直接选择的主要类型：

- 项目 `project`
- 对话 `conversation`
- 漏洞 `vulnerability`
- WebShell `webshell`
- 批量任务队列 `batch_task`
- C2 Listener `c2_listener`

一次批量授权最多 100 个资源。重复授权会跳过，不会创建重复记录。

部分子资源会继承父资源访问能力：

| 子资源 | 可继承的父资源 |
|--------|----------------|
| 对话 | 所属项目 |
| 漏洞 | 所属项目或关联对话 |
| 消息、过程详情、攻击链 | 所属对话 |
| C2 Session | Listener |
| C2 Task / 文件 /事件 | Session、Task 或 Listener 链路 |

因此，给用户授权一个项目，通常不需要再逐个授权该项目中的每条对话和漏洞。仍应以具体页面/API 的服务端检查结果为准。

---

## 七、Web 管理流程

### 7.1 创建用户

1. 使用管理员进入左侧 **平台权限**。
2. 创建平台用户，设置用户名、显示名称、至少 8 位密码和启用状态。
3. 分配一个或多个平台角色。
4. 若角色 Scope 为 `assigned`，继续配置资源授权。
5. 让用户重新登录并在右上角用户菜单确认角色、权限数量和 Scope。

### 7.2 创建自定义角色

1. 新建平台角色并填写清晰的岗位名称与说明。
2. 选择 `all`、`assigned` 或 `own`。
3. 只勾选岗位实际需要的权限。
4. 先用测试账号验证列表、详情、写操作、删除和 Agent 工具调用。
5. 再批量分配给正式用户。

系统角色不可编辑；复制其思路创建自定义角色即可。

### 7.3 权限变更何时生效

- 更新用户、密码、启用状态或角色后，该用户现有会话会被撤销，需要重新登录。
- 修改自定义角色的权限或 Scope、或删除自定义角色后，仅该角色成员的现有会话会被撤销，需要重新登录；无关用户保持在线。仅修改名称、描述或调整权限顺序不会撤销会话；删除零成员角色也不影响现有会话。
- 机器人每条消息实时解析绑定用户/服务账号权限；用户禁用或角色调整会立即影响下一条消息。
- 后台批量任务会根据任务 owner 重新解析 Principal，不应依赖创建任务时的前端状态。

---

## 八、推荐角色模板

以下是起点，不是固定策略。

### 只读项目成员

```text
Scope: assigned
dashboard:read
chat:read
project:read
vulnerability:read
files:read
attackchain:read
```

### 日常安全操作员

```text
Scope: assigned
agent:execute
chat:read / chat:write
project:read / project:write
vulnerability:read / vulnerability:write
tasks:read / tasks:write
files:read / files:write
hitl:read / hitl:write
```

只有确实需要本机命令时才增加 `agent:local-execute` 或 `terminal:execute`；需要删除时再增加对应 `:delete`。

### 机器人专用账号

```text
Scope: own（独立工作区）或 assigned（指定项目）
agent:execute
chat:read / chat:write
按需增加 project、vulnerability、knowledge 等权限
```

也可使用 `admin` 作为机器人服务账号，但发送者仍需精确白名单；白名单内每个人都会获得完整权限并共享 admin 数据。详见[机器人指南](robot.md)。

---

## 九、Agent、MCP 与机器人边界

### Agent

HTTP 登录用户会被转换为不可变 Principal，传入单 Agent、多 Agent、工作流和工具执行上下文。长任务脱离 SSE 连接后仍保留身份，但不会因为前端按钮可见而绕过服务端权限。

### 内置 MCP

每个内置工具必须有显式授权策略。例如 WebShell 工具会同时检查 `webshell:read/write/delete` 和 `connection_id` 的资源访问；漏洞、项目、任务与 C2 工具也会检查参数指向的资源。

未登记授权策略的内置工具默认拒绝。普通本地/配置工具需要 `agent:local-execute`。

### 外部 MCP

Agent 调用外部 MCP 工具需要 `mcp:external:execute`，且当前要求 Scope 为 `all`。这是因为外部服务的资源模型通常不受本地 owner/assignment 约束。

### 机器人

- `user_binding`：平台发送者绑定自己的 RBAC 用户。
- `service_account`：精确白名单发送者统一使用一个 RBAC 用户。
- 平台验签只做来源认证，不代替业务授权。
- 发送 `身份` / `whoami` 可检查实际 Principal。

---

## 十、RBAC API

所有接口使用：

```http
Authorization: Bearer <token>
```

管理接口需要 `rbac:read` 或 `rbac:write`，资源选择器需要 `rbac:write`。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/rbac/me` | 当前用户、角色、权限、总 Scope 与逐权限 Scope |
| GET | `/api/rbac/metadata` | 权限目录、角色、角色权限和 Scope 列表 |
| GET/POST | `/api/rbac/users` | 列出/创建用户 |
| PUT/DELETE | `/api/rbac/users/:id` | 更新/删除用户 |
| GET/POST | `/api/rbac/roles` | 列出/创建角色 |
| PUT/DELETE | `/api/rbac/roles/:id` | 更新/删除自定义角色 |
| GET | `/api/rbac/resources?type=project&q=...` | 分页搜索可授权资源 |
| GET/POST | `/api/rbac/resource-assignments` | 列出/创建资源授权 |
| DELETE | `/api/rbac/resource-assignments/:id` | 撤销资源授权 |

创建用户示例：

```bash
curl -X POST http://localhost:8080/api/rbac/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "operator01",
    "display_name": "安全操作员 01",
    "password": "change-me-123",
    "enabled": true,
    "roles": ["operator"]
  }'
```

创建自定义角色示例：

```bash
curl -X POST http://localhost:8080/api/rbac/roles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "项目审计员",
    "description": "只读查看指定项目",
    "scope": "assigned",
    "permissions": ["chat:read", "project:read", "vulnerability:read"]
  }'
```

批量授权项目示例：

```bash
curl -X POST http://localhost:8080/api/rbac/resource-assignments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "USER_ID",
    "resource_type": "project",
    "resource_ids": ["PROJECT_ID_1", "PROJECT_ID_2"]
  }'
```

---

## 十一、审计与运维建议

- 使用个人账号管理平台，避免多人共享管理员密码。
- 自定义角色按岗位命名，描述中写明用途和负责人。
- 高风险权限单独审批：`terminal:execute`、`agent:local-execute`、`c2:write/delete`、`webshell:write/delete`、`rbac:write`、`config:write`。
- 定期检查 `all` Scope 角色、服务账号、机器人白名单和长期未使用用户。
- 用户离职时先禁用账号，再撤销机器人绑定、资源授权和会话。
- 在日志审计中关注 `rbac/access_denied`、角色/用户变更、资源授权、机器人服务账号执行。
- 配合 HITL 控制高风险工具；RBAC 允许调用不等于可以跳过审批。

---

## 十二、常见问题

### 页面按钮看不到

检查用户是否有对应权限；前端会根据 `/api/rbac/me` 隐藏无权操作。直接调用 API 仍会由服务端拒绝。

### 有权限但返回“无权访问该资源”

检查该权限的 Scope，而不是只看用户总 Scope；再检查资源 owner、显式授权和父资源授权。

### 角色改了但用户仍是旧权限

角色权限或 Scope 变更会撤销该角色成员的会话。让受影响用户重新登录；机器人下一条消息会重新解析权限。

### 忘记了内置 `admin` 密码

优先使用其他具备 `rbac:write` 权限的管理员账号重置。若没有可用的管理员会话，请按[排错指南中的管理员密码恢复流程](troubleshooting.md#忘记-admin-密码)在服务器上紧急重置。

### `write` 权限存在但全局配置仍被拒绝

全局对象写操作要求对应权限的 Scope 为 `all`。创建一个 `all` Scope 的专用管理角色，而不是扩大无关权限。

### Agent 能对话但不能运行命令

`agent:execute` 与 `agent:local-execute` 分离。按需授予本地执行权限，并结合 HITL、工具白名单和审计。

### 外部 MCP 提示需要 global scope

除 `mcp:external:execute` 外，该权限的 Scope 还必须为 `all`。外部 MCP 的数据边界不由本地资源授权自动保护。
