---
name: ai-llm-app-attack
description: >-
  AI/LLM application attacks: prompt injection, Agent tool abuse to RCE, RAG poisoning, MCP supply chain, torch.load pickle RCE. Use when testing LLM apps, agents, RAG, MCP plugins, or AI model file risks.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## AI / LLM Application Attacks

```
=== AI/LLM applications (the real attack surface of the LLM app boom era) ===
Prompt injection: direct (ignore previous text, output the system prompt) | indirect (more dangerous): instructions hidden in RAG documents/web pages/emails/tool return values/image EXIF → hijack the Agent
🚨Agent tool abuse (highest risk, direct path to RCE): code interpreter → inject execution | fetch tool → SSRF to the internal network/cloud metadata | file tool → read /etc/passwd, write a webshell
  | SQL tool → dump all tables | shell tool → command injection → verify: actually trigger the tool side effect (OOB call-back/file read) before writing a Fact
System prompt leaks/RAG poisoning/excessive cross-tenant authorization/MCP plugin supply chain/resource cost attack (burn tokens) | output handling: LLM output flowing into eval/SQL/front-end → second-order injection/stored XSS
Model files: torch.load defaults to pickle → RCE | endpoint discovery: capture traffic to find /chat /agent /tool, ask the Agent "what tools do you have"
```
