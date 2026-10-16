# CyberStrikeAI Documentation

[中文](#中文文档) | [English](#english-documentation)

CyberStrikeAI documentation is organized by user journey. Start with deployment, then move to the topic that matches your task.

## 中文文档

### 按目标开始

- **快速体验**：[部署指南](zh-CN/deployment.md) → [配置参考](zh-CN/configuration.md) → [排错指南](zh-CN/troubleshooting.md)
- **生产部署**：[配置画像](zh-CN/configuration-profiles.md) → [安全加固](zh-CN/security-hardening.md) → [运维 Runbooks](zh-CN/runbooks.md) → [审计与监控](zh-CN/audit-and-monitoring.md)
- **接入与自动化**：[API 参考](zh-CN/api-reference.md) → [API Recipes](zh-CN/api-recipes.md) → [MCP 联邦](zh-CN/mcp-federation.md)
- **参与开发**：[开发者指南](zh-CN/developer-guide.md) → [测试指南](zh-CN/testing.md) → [贡献规范](zh-CN/contributing-guide.md)

### 核心概念与编排

- [架构说明](zh-CN/architecture.md)
- [安全模型](zh-CN/security-model.md)
- [Agent 与角色](zh-CN/agent-and-role-guide.md)
- [Skills 指南](zh-CN/skills-guide.md)
- [Eino 多代理](zh-CN/MULTI_AGENT_EINO.md)
- [工作流](zh-CN/workflow-graph.md)
- [工具执行治理](zh-CN/tool-execution-governance.md)
- [人机协同最佳实践](zh-CN/hitl-best-practices.md)

### 功能指南

- [知识库](zh-CN/knowledge-base.md)
- [RBAC 权限管理](zh-CN/rbac.md)
- [机器人接入](zh-CN/robot.md)
- [视觉分析](zh-CN/VISION.md)
- [WebShell 管理](zh-CN/webshell.md)
- [C2 使用说明](zh-CN/c2.md)
- [Parallel Search MCP](zh-CN/parallel-search.md)

### 开发与发布

- [开发者指南](zh-CN/developer-guide.md)
- [插件开发](zh-CN/plugin-development.md)
- [前端国际化](zh-CN/frontend-i18n.md)
- [测试指南](zh-CN/testing.md)
- [贡献规范](zh-CN/contributing-guide.md)
- [发布流程](zh-CN/release-process.md)

## English Documentation

### Choose a path

- **Try locally**: [Deployment](en-US/deployment.md) → [Configuration](en-US/configuration.md) → [Troubleshooting](en-US/troubleshooting.md)
- **Run in production**: [Configuration Profiles](en-US/configuration-profiles.md) → [Security Hardening](en-US/security-hardening.md) → [Runbooks](en-US/runbooks.md) → [Audit and Monitoring](en-US/audit-and-monitoring.md)
- **Integrate and automate**: [API Reference](en-US/api-reference.md) → [API Recipes](en-US/api-recipes.md) → [MCP Federation](en-US/mcp-federation.md)
- **Contribute code**: [Developer Guide](en-US/developer-guide.md) → [Testing](en-US/testing.md) → [Contributing](en-US/contributing-guide.md)

### Concepts and orchestration

- [Architecture](en-US/architecture.md)
- [Security Model](en-US/security-model.md)
- [Agents and Roles](en-US/agent-and-role-guide.md)
- [Skills](en-US/skills-guide.md)
- [Eino Multi-Agent](en-US/MULTI_AGENT_EINO.md)
- [Workflows](en-US/workflow-graph.md)
- [Tool Execution Governance](en-US/tool-execution-governance.md)
- [HITL Best Practices](en-US/hitl-best-practices.md)

### Feature guides

- [Knowledge Base](en-US/knowledge-base.md)
- [RBAC Administration](en-US/rbac.md)
- [Robot / Chatbot](en-US/robot.md)
- [Vision Analysis](en-US/VISION.md)
- [WebShell Management](en-US/webshell.md)
- [C2 Guide](en-US/c2.md)
- [Parallel Search MCP](en-US/parallel-search.md)

### Development and release

- [Developer Guide](en-US/developer-guide.md)
- [Plugin Development](en-US/plugin-development.md)
- [Frontend i18n](en-US/frontend-i18n.md)
- [Testing](en-US/testing.md)
- [Contributing](en-US/contributing-guide.md)
- [Release Process](en-US/release-process.md)

## Documentation conventions

- Commands assume the repository root unless stated otherwise.
- Examples use placeholders; never commit real credentials or target systems without explicit authorization.
- Runtime behavior and configuration defaults are authoritative in `config.example.yaml` and the source code. If a document differs, report it as documentation drift.
