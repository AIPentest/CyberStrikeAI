# 排错指南

本文按现象列出常见问题。优先查看服务日志、浏览器控制台和 `/api-docs` 中的接口响应。

## 无法访问页面

检查：

- 服务是否启动。
- 端口是否被占用。
- 配置中是否启用 HTTPS。
- 访问协议是否正确。

默认配置常见地址：

```text
https://127.0.0.1:8080/
```

如果使用自签证书，浏览器会提示不受信任，需要手动继续访问。

## 登录失败

检查：

- RBAC 用户密码是否正确（默认 `admin`；首次启动密码见控制台输出）。
- 是否修改密码后旧会话已失效，需重新登录。
- 浏览器 Cookie 是否异常，可尝试无痕窗口。
- 审计日志中是否有登录失败节流。

### 忘记 `admin` 密码

如果仍有其他具备 `rbac:write` 权限的管理员账号，优先在 **平台权限 → 用户管理** 中重置密码。

如果没有可用的管理员会话，可在服务器上紧急重置内置 `admin` 账号。在项目根目录执行：

```bash
./run.sh --reset-admin-password
```

按提示输入并确认新密码。脚本会隐藏输入并写入 bcrypt 哈希。如果服务正在运行，完成后重新启动服务，使原有登录会话失效。

如果无法使用 `run.sh`，也可以手动执行以下命令，按提示输入并确认新密码：

```bash
HASH=$(htpasswd -nBC 10 '' | cut -d: -f2 | tr -d '\n') && sqlite3 data/conversations.db "UPDATE rbac_users SET password_hash='$HASH', updated_at=CURRENT_TIMESTAMP WHERE id='admin' AND username='admin' AND is_builtin=1; SELECT changes();"
```

输出 `1` 表示修改成功。该命令需要 `sqlite3` 和 `htpasswd`；如果 `config.yaml` 中的 `database.path` 不是默认值，请替换 `data/conversations.db`。密码输入不会显示，也不会写入 Shell 历史。

## 模型无响应

检查：

- 当前对话选择的 AI 通道是否存在；为空时会使用 `ai.default_channel`。
- `ai.channels.<id>.base_url` 是否包含正确路径，如 `/v1`。
- `ai.channels.<id>.api_key` 是否有效。
- `ai.channels.<id>.model` 是否存在。
- 服务商是否支持当前通道的 `reasoning` 字段。

可在系统设置中使用模型测试。若网关报 400，先尝试：

```yaml
ai:
  channels:
    your-channel:
      reasoning:
        mode: off
```

## 流式输出中断

常见原因：

- 反向代理缓冲 SSE。
- 模型网关超时。
- 浏览器网络断开。
- 上下文过大。

Nginx 需要：

```nginx
proxy_buffering off;
proxy_http_version 1.1;
```

## 工具执行失败

检查：

- 工具命令是否已安装到 PATH。
- `tools/*.yaml` 参数 schema 是否正确。
- 是否被 HITL 拒绝。
- 是否超过 `agent.tool_timeout_minutes`。
- Shell 长时间无输出是否触发 `shell_no_output_timeout_seconds`。

工具配置可参考 `tools/README_CN.md`（English: `tools/README.md`）。

## MCP 连不上

内置 MCP：

- 检查 `mcp.enabled`。
- 检查 `mcp.port`。
- 检查 `auth_header` 和 `auth_header_value`。

外部 MCP：

- stdio：检查命令路径、工作目录、环境变量。
- HTTP/SSE：检查 URL、认证、网络连通性。
- 查看 `/api/external-mcp/stats`。

## 知识库不可用

检查：

- `knowledge.enabled: true`。
- embedding 配置是否正确。
- 是否已经扫描并重建索引。
- `data/knowledge.db` 是否可写。
- 嵌入服务是否 429 或超时。

如果索引大量失败，降低：

```yaml
knowledge:
  indexing:
    batch_size: 5
    rate_limit_delay_ms: 600
```

## 机器人没有回复

检查：

- 对应平台 `robots.<platform>.enabled`。
- 平台回调 URL 是否指向 `/api/robot/...`。
- Token、secret、verify_token 是否一致。
- 服务器是否可被平台访问。
- 群聊是否需要 @ 机器人。

详细步骤见 [机器人使用说明](robot.md)。

## C2 监听器启动失败

检查：

- `c2.enabled`。
- 端口是否被占用。
- 防火墙或安全组。
- 是否需要管理员权限绑定低端口。

关闭 C2 后 `/api/c2/*` 返回 503 是预期行为。

## WebShell 命令乱码

处理：

- 确认目标系统编码。
- 尝试更短命令。
- 使用 base64 包装输出。
- Windows 目标检查代码页。

## 数据库锁或写入失败

检查：

- `data/` 是否可写。
- 是否多个实例共用同一个 SQLite 文件。
- 磁盘是否满。
- 是否异常复制了 WAL/SHM 文件。

生产环境不要让多个进程同时写同一份 SQLite 数据库。

## 前端页面异常

检查：

- 浏览器控制台错误。
- 静态资源是否加载成功。
- 修改前端后是否刷新缓存。
- i18n key 是否缺失。

接口异常时打开 `/api-docs` 对照请求体。

## 诊断顺序

遇到问题时不要直接改配置，先定位层级：

1. 进程：服务是否还在，日志是否有 panic。
2. 网络：端口、HTTPS、反向代理、浏览器控制台。
3. 认证：`/api/auth/validate` 是否 200。
4. 配置：`/api/config` 是否能读，应用后是否报错。
5. 模型：模型测试是否通过。
6. 工具：工具列表和单个 schema 是否正常。
7. 数据库：`data/` 是否可写，有无锁。
8. 业务模块：知识库、MCP、C2、WebShell 分别测最小动作。

先定位层级，再改参数。否则容易把一个代理问题误判成模型问题。

## 最小诊断命令

```bash
# 进程和端口
lsof -i :8080

# 本机 HTTPS 是否通
curl -k -I https://127.0.0.1:8080/

# 静态资源是否通
curl -k -I https://127.0.0.1:8080/static/logo.png

# 查看数据库文件
ls -lh data/
```

如果经过 Nginx，再分别测代理地址和回源地址，确认问题在哪一层。

## 常见误判

- “模型坏了”：实际是 HITL 挂起等待审批。
- “工具没加载”：实际是 tool_search 隐藏了大部分工具。
- “知识库没效果”：实际是索引没重建或 risk_type 过滤过窄。
- “C2 接口坏了”：实际是 `c2.enabled: false`，返回 503 是正常保护。
- “配置保存了但没生效”：实际是监听端口/TLS 需要重启。
- “机器人不回复”：实际是平台侧没有正确配置回调 URL 或验签参数。

## 故障报告模板

提交问题时建议附：

```text
版本/提交：
启动方式：
访问方式：http/https/反向代理：
相关配置段：
复现步骤：
预期结果：
实际结果：
服务端日志：
浏览器控制台：
相关接口响应：
```

有了这些信息，定位速度通常会快很多。
