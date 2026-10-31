# HTTP 测试工具代理池

[English](../en-US/http-proxy-pool.md) | [文档首页](README.md)

`http-framework-test` 支持用户提供的代理列表及代理供应商接口。连接失败或响应命中指定状态码时，在次数上限内切换代理；成功代理继续复用连接。默认关闭，原有 `proxy` 参数及无代理调用方式继续可用。

## 使用

更新工具文件后，在系统设置中应用配置或重启服务以重新加载工具参数。代理池通过每次工具调用的参数启用，不需要修改 `config.yaml`。

调用 `http-framework-test` 时传入以下参数（MCP 工具参数示例）：

```json
{
  "url": "https://your-authorized-target.example/health",
  "method": "GET",
  "proxy_pool": "[\"http://127.0.0.1:8081\",\"http://127.0.0.1:8082\"]",
  "proxy_max_attempts": 3,
  "proxy_cooldown": "60",
  "proxy_fail_status": "403,429",
  "response_max_bytes": 8192
}
```

`proxy_pool` 也接受每行一个代理的字符串。支持 `http://host:port`、`https://host:port`、`socks5://host:port` 和省略协议的 `host:port`（按 HTTP 解析）；端口必须显式指定。支持 IPv6 方括号及 `user:password@host:port` 认证，凭证中的特殊字符须做 URL 编码。SOCKS 依赖包含在 `requirements.txt` 的 `httpx[http2,socks]` 中。

使用供应商接口自动获取代理时，增加或单独使用：

```json
{
  "url": "https://your-authorized-target.example/health",
  "proxy_source": "https://your-proxy-provider.example/api/proxies"
}
```

接口返回 UTF-8 文本，每行一个代理，或以下 JSON 格式之一：

```json
["http://127.0.0.1:8081", "127.0.0.1:8082"]
```

```json
{"proxies": ["http://127.0.0.1:8081"]}
```

也支持 `{"data": ["http://127.0.0.1:8081"]}`。列表元素还可为 `{"host":"127.0.0.1","port":8081,"scheme":"http","username":"user","password":"pass"}`（`ip` 可代替 `host`），或 `{"url":"http://127.0.0.1:8081"}`。`scheme` 默认 HTTP，认证字段可省略；结构化凭证自动做 URL 编码。需要其他供应商字段格式时，应由供应商或自建适配接口转换为上述格式。项目不内置公共免费代理来源。

供应商接口在调用开始时拉取一次；每个请求或重定向跳的可用代理耗尽时，最多再拉取一次。新地址与现有地址去重合并，已有冷却记录保留。连接/读取超时为 10 秒，不跟随重定向，响应上限 1 MiB，代理总数上限 256。首次拉取失败时，有静态列表则发出提示并继续使用静态代理；仅配置供应商时结束调用；切换过程中拉取失败则保留最后的 HTTP 失败响应，若还未收到响应则报错。

## 切换与结果

| 参数 | 默认值 | 行为 |
| --- | --- | --- |
| `proxy_pool` | 空 | JSON 字符串数组或换行列表 |
| `proxy_strategy` | `"sticky"` | `sticky` 成功后复用，`round_robin` 轮询并跳过冷却代理 |
| `proxy_source` | 空 | 用户配置的 HTTP(S) 供应商接口，可与列表合用 |
| `proxy_max_attempts` | `3` | 每个请求/重定向跳最多尝试 1–10 个不同代理 |
| `proxy_cooldown` | `"60"` | 失败代理冷却 0–86400 秒 |
| `proxy_fail_status` | `"403,429"` | 逗号分隔的 400–599 状态码，传 `"none"` 禁用状态码切换 |

具体规则：

- 默认保持成功代理；设置 `proxy_strategy="round_robin"` 后，每个请求/重定向跳轮询可用代理；失败后冷却，在剩余可用代理中切换。每个请求或重定向跳内，同一代理最多尝试一次。
- 仅 GET、HEAD、OPTIONS 自动重放。POST、PUT、DELETE 等方法发生错误或命中状态码时保留首次结果；用户显式设置的 `repeat` 仍表示额外执行次数。
- 连接、代理、超时等传输错误可触发上述切换；响应体开始读取后的中断直接报错，不重放部分响应。
- 429 若包含有效且尚未到期的 `Retry-After`，停止自动重放并提示稍后重试。同一工具调用中，对该目标源的后续请求也会被阻止至等待期结束，等待值最多记为 24 小时。
- 最后一次尝试若收到 HTTP 失败响应，保留该响应并返回非零退出码；网络故障或所有代理处于冷却时报告错误。不会回退到直连，也不会运行额外的目标 DNS/TCP/TLS 直连探针。
- stderr 记录尝试编号、代理编号及失败类型；stdout 保留最终响应及最终代理编号。代理池自身诊断不输出代理凭证或供应商 URL。工具调用参数仍沿用平台既有的轨迹、审计与日志规则，带凭证的参数应按敏感配置管理。

## 调研结论与覆盖范围

[#357](https://github.com/AIPentest/CyberStrikeAI/issues/357) 仅提出自动获取代理与被封后切换，未定义供应商、识别方式或工具范围。当前实现从能读取 HTTP 响应的 `http-framework-test` 接入，使 HTTP 和 HTTPS 的切换行为都可验证。

HTTP CONNECT 隧道中的 HTTPS 响应需要由发起请求的客户端解密才能读取，因此单独增加全局转发代理不足以识别全部工具的 HTTPS 封禁响应。HTTPX 的代理与自定义传输接口适合在本工具内完成切换，见 [HTTPX 代理文档](https://www.python-httpx.org/advanced/proxies/)与[传输文档](https://www.python-httpx.org/advanced/transports/)。

403 也可能是业务权限错误，429 也可能按账号或其他维度限流；默认状态码只是可配置的失败策略，不宣称已确认 IP 被封。不匹配状态码的验证码页、HTTP 200 拦截页目前保持原样。重放和等待规则参考 [RFC 9110 §9.2.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.2) 和 [RFC 6585 §4](https://www.rfc-editor.org/rfc/rfc6585.html#section-4)。

池状态只在单次工具调用内保留，不跨会话共享。其他扫描工具、`exec`、Eino `execute`、外部 MCP、LLM API 连接暂不接入本池；未新增全局代理设置页。供应商拉取使用独立客户端，开启 TLS 校验，不继承目标请求的 Cookie、Authorization、环境代理或 `allow_insecure`；生产供应商接口建议使用 HTTPS。

## 排错与源码

| 提示 | 处理 |
| --- | --- |
| `Proxy pool exhausted` | 检查代理可达性，等待冷却或更换列表/供应商返回值 |
| `failure policy still matched` | 查看保留的响应；确认是 IP 限制还是正常的权限/限流结果，按需调整 `proxy_fail_status` |
| `Retry-After is still active` | 等待目标指定的时间后再发起调用 |
| `Could not fetch proxy source` | 检查供应商接口网络、认证和 TLS；目标的 `allow_insecure` 不影响供应商连接 |
| `SOCKS support requires ...` | 在工具使用的 Python 环境安装 `httpx[socks]` 或更新 `requirements.txt` 依赖 |

执行代码与 MCP 参数定义位于 [tools/http-framework-test.yaml](../../tools/http-framework-test.yaml)，回归场景位于 [scripts/test_http_proxy_pool.py](../../scripts/test_http_proxy_pool.py)。

## 本地验证

```sh
python3 -m pip install 'httpx[http2,socks]>=0.27.0' PyYAML
python3 -m unittest discover -s scripts -p 'test_http_proxy_pool.py' -v
```

测试从 YAML 读取实际执行代码，使用本地 HTTP 代理、CONNECT 隧道及 TLS 服务验证切换、路径保真、重放限制、等待期、供应商刷新与凭证隔离；不访问外部目标或真实供应商。HTTPS 测试需要 `openssl`。
