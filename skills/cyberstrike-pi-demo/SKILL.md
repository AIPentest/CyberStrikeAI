---
name: cyberstrike-pi-demo
description: 满配示例技能包：SKILL.md + scripts/、references/、assets/ 等可选目录；验证 Pi skill 与 HTTP 包内路径（仅授权安全测试与教学）。
---

# CyberStrike × Pi 满配技能演示

本包与 [Agent Skills](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview) 一致：**`SKILL.md` 为清单 + 主说明**（无单独 `SKILL.yaml`）。同目录可有 **`scripts/`**、**`references/`**、**`assets/`** 等任意子目录（只要路径安全、未触达包深度/文件数上限），由 **`ListPackageFiles` / `resource_path`** 与 Pi 本机工具读取。补充说明见 `FORMS.md`、`REFERENCE.md`。

## 概述

用于一次性验证：

- HTTP `GET /api/skills` 列表（`script_count`、`file_count`、`progressive` 等为推导/扫描结果）
- `GET /api/skills/cyberstrike-pi-demo?depth=summary|full`
- `section=` 对应 `SKILL.md` 中 **`##` 标题**或 ASCII 标题的短 id（例如 `## Payload 样例` 常对应 `section=payload`）
- 多代理内原生 **`skill`** 工具（及可选本机文件工具）读取包内相对路径资源
- 原生 `skill` 工具对技能正文、脚本与参考资料的读取

**硬性要求**：任何测试须取得书面授权，并限定在约定范围与时间窗口内。

## 授权测试工作流

1. **范围确认**：域名 / IP、接口列表、禁止动作（DoS、数据拖库等）。
2. **基线记录**：对约定资产做只读探测，保存时间戳与原始请求/响应摘要。
3. **分类测试**：按漏洞类型拆分任务；高风险操作前二次确认授权边界。
4. **证据与报告**：每个发现附带复现步骤、影响、修复建议；敏感数据脱敏。
5. **收尾**：删除临时账号、清理测试数据、移交报告。

## Payload 样例

以下为 **教学占位**，实际测试需替换为目标上下文且不得用于未授权系统：

- SQLi 探测（错误型）：`"'`（观察是否触发数据库错误信息泄露）
- XSS 反射型（无害化）：`<script>alert(1)</script>` → 在靶场中应被编码或 CSP 拦截
- 路径穿越（只读验证）：`....//....//etc/passwd`（仅在授权文件读取场景）

详细列表见 `scripts/payloads.txt`。

## references/ 与 assets/

用于验证非 `scripts/` 的子目录是否被同等对待：

| 路径 | 用途 |
|------|------|
| `references/citations.md` | 引用与 HTTP `resource_path` 测试说明 |
| `assets/README.txt` | 占位资源（可换成真实二进制做读文件上限测试） |

## 推荐工具链

| 阶段 | 工具示例 |
|------|-----------|
| 代理与重放 | Burp Suite、mitmproxy |
| 扫描与目录 | ffuf、nuclei（需调低并发遵守授权） |
| 漏洞验证 | 自写 PoC、官方 CLI（sqlmap 等）仅在授权范围内 |
| 记录 | Markdown + JSON 片段模板（见 `scripts/report-snippet.json`） |

## 清单与验证

- [ ] 已保存书面授权与测试窗口
- [ ] `scripts/` 下文件与正文引用一致
- [ ] Web 或 `GET /api/skills?...` 可核对索引；多代理会话内用 **`skill`** 工具按包加载以节省 token
- [ ] 需要细节时通过 **`skill`** 拉全文，或 HTTP `depth=full`、`section=<标题或短 id>`
- [ ] 需要脚本原文时通过本机文件工具或 HTTP `resource_path=scripts/check-env.sh`
- [ ] `resource_path=references/citations.md` 与 `resource_path=assets/README.txt` 可读取
