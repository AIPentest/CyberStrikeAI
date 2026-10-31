# HTTP test tool proxy pool

[中文](../zh-CN/http-proxy-pool.md) | [Documentation home](README.md)

`http-framework-test` accepts a user-supplied proxy list and/or provider endpoint. It switches proxies within a bounded attempt budget on transport failures or configured HTTP status codes, and reuses a successful proxy. Settings → Proxy settings can manage egress for this HTTP tool; the default remains per-call arguments.

## Usage

After updating the tool file, apply configuration in settings or restart the service to reload its parameters. The proxy submenu also supports persistent settings that apply to subsequent invocations without restarting active processes.

Example MCP tool arguments:

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

`proxy_pool` also accepts newline-separated endpoints. Supported forms are `http://host:port`, `https://host:port`, `socks5://host:port`, and bare `host:port` (HTTP). An explicit port is required. IPv6 brackets and URL-encoded `user:password@host:port` credentials are supported. SOCKS support is included in the `httpx[http2,socks]` requirement.

To fetch proxies automatically, add or use `proxy_source` alone:

```json
{
  "url": "https://your-authorized-target.example/health",
  "proxy_source": "https://your-proxy-provider.example/api/proxies"
}
```

The endpoint must return UTF-8 newline-separated endpoints, a JSON array of endpoint strings, or an object containing a `proxies` or `data` array:

```json
{"proxies": ["http://127.0.0.1:8081", "127.0.0.1:8082"]}
```

Entries may also be objects such as `{"host":"127.0.0.1","port":8081,"scheme":"http","username":"user","password":"pass"}` (`ip` aliases `host`) or `{"url":"http://127.0.0.1:8081"}`. Scheme defaults to HTTP, credentials are optional and automatically URL-encoded. Other provider formats require an adapter. No public free-proxy sources are built in. The source is fetched at startup and at most once more per request/redirect hop when available candidates run out. New endpoints are deduplicated and merged without clearing cooldowns. Fetches have 10-second connect/read timeouts, do not follow redirects, accept at most 1 MiB, and the pool holds at most 256 endpoints. An initial fetch failure warns and uses static endpoints when available; source-only calls fail. A refresh failure preserves the last HTTP failure response, or reports an error if no response exists.

## Behavior

| Parameter | Default | Meaning |
| --- | --- | --- |
| `proxy_pool` | Empty | JSON string array or newline-separated list |
| `proxy_strategy` | `"sticky"` | `sticky` reuses successful proxies; `round_robin` rotates and skips cooldowns |
| `proxy_source` | Empty | User-configured HTTP(S) provider endpoint, optionally combined with a list |
| `proxy_max_attempts` | `3` | 1–10 distinct proxy attempts per request/redirect hop |
| `proxy_cooldown` | `"60"` | 0–86400 seconds before a failed proxy is eligible again |
| `proxy_fail_status` | `"403,429"` | Comma-separated status codes from 400–599; `"none"` disables status-based switching |

- Successful proxies stay selected by default. With `proxy_strategy="round_robin"`, each request/redirect hop rotates through eligible proxies. Failed proxies enter cooldown. Each proxy is tried at most once per request/redirect hop.
- Only GET, HEAD and OPTIONS are replayed automatically. POST, PUT, DELETE and other methods retain the first result. An explicit `repeat` still requests additional executions.
- Transport failures before response headers can trigger switching. Failures while reading a response body are reported without replaying a partial response.
- A 429 with a valid future `Retry-After` stops automatic replay and reports that the caller should retry later. Further requests to that origin within the same tool invocation are blocked until the wait expires; waits are recorded for at most 24 hours.
- If the final attempt returns an HTTP failure response, it is preserved with a nonzero exit code. A network failure or a pool with all endpoints cooling down reports an error. There is no direct fallback or extra direct DNS/TCP/TLS target probe.
- Pool diagnostics show attempt numbers, proxy numbers and failure types. They omit proxy credentials and provider URLs. The platform's existing argument traces, audit records and logs still apply; treat arguments containing credentials as sensitive configuration.

## Scope and design

[#357](https://github.com/AIPentest/CyberStrikeAI/issues/357) requests automatic proxy acquisition and switching after a ban, without specifying providers, detection or tool scope. This implementation starts with `http-framework-test`, where both HTTP and decrypted HTTPS responses can be inspected. A standalone forwarding proxy cannot read HTTPS responses inside a CONNECT tunnel. See the [HTTPX proxy](https://www.python-httpx.org/advanced/proxies/) and [transport](https://www.python-httpx.org/advanced/transports/) documentation.

A 403 can be an application authorization failure; a 429 can apply to an account or other scope. Status matching is a configurable policy, not proof of an IP ban. CAPTCHA or HTTP 200 block pages remain unchanged. Replay and waiting rules are informed by [RFC 9110 §9.2.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.2) and [RFC 6585 §4](https://www.rfc-editor.org/rfc/rfc6585.html#section-4).

Pool state lasts for one tool invocation. Other scanners, `exec`, Eino `execute`, external MCP servers and LLM API connections are outside this pool; the settings submenu manages only this HTTP tool. Provider fetching uses a separate client with TLS verification and no target cookies, target Authorization, environment proxies or inherited `allow_insecure`. Use HTTPS for production provider endpoints.

## Troubleshooting and source

| Message | Action |
| --- | --- |
| `Proxy pool exhausted` | Check endpoint connectivity, wait for cooldown, or replace the list/provider results |
| `failure policy still matched` | Inspect the preserved response and distinguish IP restrictions from normal authorization/rate limits; adjust `proxy_fail_status` if needed |
| `Retry-After is still active` | Wait for the target's requested delay before another call |
| `Could not fetch proxy source` | Check provider connectivity, authentication and TLS; target `allow_insecure` does not affect provider connections |
| `SOCKS support requires ...` | Install `httpx[socks]` in the tool's Python environment or update its requirements |

The implementation and MCP parameter definitions are in [tools/http-framework-test.yaml](../../tools/http-framework-test.yaml); regression scenarios are in [scripts/test_http_proxy_pool.py](../../scripts/test_http_proxy_pool.py).

## Local verification

```sh
python3 -m pip install 'httpx[http2,socks]>=0.27.0' PyYAML
python3 -m unittest discover -s scripts -p 'test_http_proxy_pool.py' -v
```

Tests execute the code read from the tool YAML against local HTTP proxies, CONNECT tunnels and TLS servers. No external targets or real providers are contacted. The HTTPS fixture requires `openssl`.

## Managed settings and stable egress

Settings → Proxy settings offers manual arguments, a fixed proxy, or a pool. Fixed mode keeps the configured endpoint across calls and does not fail over. Pool mode offers sticky or round-robin selection with a minimum switch interval (60 seconds in the settings form). The interval also prevents early failover: return the failure response/error instead. Timing is invocation-local; use fixed mode and a provider with fixed egress for stability across calls. A fixed URL alone cannot guarantee the provider's public IP.

`PUT /api/config` persists the `proxy` object and applies it to future HTTP tool invocations. GET masks `proxy_url`, `proxy_pool`, and `proxy_source`; preserve `********` to keep a saved value or send an empty string to clear it. The settings list accepts one URL per line; provider responses may still use structured entries. See `config.example.yaml`. Managed values override model-supplied proxy arguments through a process-local environment snapshot, never global HTTP_PROXY or command arguments. This is not a network sandbox for arbitrary shell commands.

Design references: [Zyte sessions](https://docs.zyte.com/zyte-api/usage/features.html#sessions) motivate stable IP/cookie conditions; [HTTPX clients](https://www.python-httpx.org/advanced/clients/) motivate connection reuse; [Nuclei](https://docs.projectdiscovery.io/opensource/nuclei/running) exposes distinct proxy/rate controls; [Nmap](https://nmap.org/book/man-bypass-firewalls-ids.html) documents limits on which scan phases support proxies. Other Kali tools retain native routing, proxy and rate settings. No output-based ban guessing or process restarts are introduced.

LLM API transports, streams and retry/backoff remain separate from the scan pool. Account/token rate limits are handled by existing provider retry policies. Stable public-IP requirements depend on the provider; this feature never rotates model API egress.
