# 仓库与分支说明 / Repository and branch guide

2026-11-11：个人仓库已由 `CyberStrikeAI-personal` 改名为 `CyberStrikeAI`；原同名上游 fork 已改名为 `CyberStrikeAI-upstream-fork`。日常发布继续进入个人独立仓库，官方贡献仍走 fork。

## 仓库职责

| 仓库 | 职责 |
| --- | --- |
| [Sycun/CyberStrikeAI](https://github.com/Sycun/CyberStrikeAI) | 个人维护主线。日常修复、优化和文档先在这里保存、使用与验证。 |
| [Sycun/csai-decouple-refactor](https://github.com/Sycun/csai-decouple-refactor) | 独立架构实验线，探索分层解耦与插件化；完成度以该仓库代码和验证记录为准。 |
| [Sycun/CyberStrikeAI-upstream-fork](https://github.com/Sycun/CyberStrikeAI-upstream-fork) | 官方贡献 fork。仅将选定改动送往上游，保留已有 PR 的来源分支。 |
| [AIPentest/CyberStrikeAI](https://github.com/AIPentest/CyberStrikeAI) | 官方上游。个人维护线与重构线均保留原始历史、许可证与版权说明。 |

个人主线与架构实验线分别演进，不把全部分支自动合入一个版本。向官方贡献时，应从官方基线组织可独立审查的改动。

## 分支状态（2026-11-03 整理时核对）

| 分支 | 当前定位 | 上游状态与后续处理 |
| --- | --- | --- |
| `main` | 个人维护默认分支 | 初始快照来自 `codex/deep-assessment`，后续日常改动以本分支为基线。 |
| `codex/deep-assessment` | 深度测评功能与官方 PR 来源 | [上游 PR #383](https://github.com/AIPentest/CyberStrikeAI/pull/383) 已于 2026-11-13 关闭；个人版已移除该模块，来源分支仅保留历史。 |
| `codex/issue-365-word-reports` | 单条漏洞 Markdown / Word 下载的历史分支 | [上游 PR #378](https://github.com/AIPentest/CyberStrikeAI/pull/378) 已合并，分支尖端已包含在个人主线历史中；保留存档。 |
| `codex/issue-357-proxy-pool` | 代理池功能开发分支 | 已整合进个人主线；[上游 PR #381](https://github.com/AIPentest/CyberStrikeAI/pull/381) 待合并，整理时存在合并冲突。个人版已完成兼容整合；官方 PR 的冲突另行处理。 |
| `feat/local-model-context-profile` | 本地模型上下文优化分支 | 已整合进个人主线；[上游 PR #367](https://github.com/AIPentest/CyberStrikeAI/pull/367) 待合并，整理时存在合并冲突。个人版已完成兼容整合；官方 PR 的冲突另行处理。 |
| `archive/local-main-20261103` | 调整个人维护流程前的本地 `main` | 历史存档，不作为日常开发或部署来源。 |

上表是带日期的状态快照，PR 后续状态以对应 GitHub 页面为准。保留分支与标签用于追溯，不据此宣称功能已集成或验收通过。

## 本次个人版整合（2026-11-03）

代理池与本地模型上下文分支已合入个人版 `main`，并保留各自提交历史。当时合并保留自定义请求头、深度测评与 Word 下载功能（深度测评已于 2026-11-13 移除）；上下文探测复用渠道鉴权头，禁止跟随重定向。此记录表示代码整合，真实模型与真实目标的运行验收仍单独记录。

## 验证与安装

- 任务管理已恢复 v1.8.0 的批量队列逻辑；入口、兼容性与验证边界见 [任务管理](zh-CN/task-management.md)。
- 现有自动化与夹具验证不代表完成真实模型、真实目标的长时验收，也不构成漏洞发现率或成本优势的证明。
- 安装个人维护版时使用本仓库 README 的克隆地址。官方仓库与个人仓库的更新来源应明确选择；采用个人仓库默认分支更新时，应先评估对应提交与运行数据迁移。
- 仓库整理与默认分支调整不等于部署。已有安装的更新配置和运行状态由各自环境管理。

## Development workflow (English)

`CyberStrikeAI/main` is the personal maintenance baseline. `csai-decouple-refactor` is a separate architecture experiment. `Sycun/CyberStrikeAI-upstream-fork` remains the contribution fork for selected upstream PRs.

The dated table above distinguishes integrated work, pending branches, and historical snapshots. Proxy-pool and local-model changes are integrated into the personal main line; upstream PR integration is tracked separately. Keep existing PR branches and archival refs. Automated checks and fixtures do not establish real-model or real-target acceptance; installation and deployment sources must be selected explicitly.
