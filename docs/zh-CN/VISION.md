# 视觉分析（analyze_image）

## 概述

- **工具名**：`analyze_image`（MCP 内置）
- **行为**：读取本地图片 → `imaging` 缩放/JPEG 压缩 → 调用独立 **Vision** 模型 → 返回**纯文本**给 Agent
- **上下文**：图片字节**不会**写入对话历史；仅路径与文字摘要进入 Agent 上下文

## 配置（`config.yaml` → `vision`）

```yaml
vision:
  enabled: true
  model: qwen-vl-max   # 必填
  api_key:             # 留空 → 默认 AI 通道 api_key
  base_url:            # 留空 → 默认 AI 通道 base_url
  provider:            # 留空 → 默认 AI 通道 provider
  max_image_bytes: 5242880
  max_dimension: 2048
  jpeg_quality: 82
  max_payload_bytes: 524288
  skip_preprocess_below_bytes: 2097152  # 低于 2MB 且长边<=max_dimension 时原图直传；0=始终 JPEG 压缩
  detail: low          # low | high | auto
  timeout_seconds: 60
```

`enabled: false` 时不注册工具。

## Web 设置

**系统设置 → 基本设置 → 视觉分析（analyze_image）** 可配置启用开关、视觉模型、API Key/Base URL（留空复用默认 AI 通道）、预处理参数；**保存并应用** 后写入 `config.yaml` 并重新注册 MCP 工具。

## 路径

`analyze_image` 可读取服务器上任意可读的图片文件路径（绝对路径或相对于进程工作目录的相对路径）。仍校验图片扩展名与常规文件类型。

## Agent 使用

系统提示已说明：遇图片调用 `analyze_image`，勿用 `read_file` 读二进制图。

`multi_agent.runtime.tool_search_always_visible_tools` 建议包含 `analyze_image`。

## 合规

启用后图片会发往 Vision API 配置的上游；敏感环境请使用可信网关或保持 `enabled: false`。
