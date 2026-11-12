# Vision Analysis

[Chinese](../zh-CN/VISION.md)

Vision analysis registers the `analyze_image` MCP tool when enabled. It is intended for screenshots, captchas, UI states, and image evidence in authorized workflows.

## Config

```yaml
vision:
  enabled: true
  model: qwen-vl
  api_key: ""
  base_url: ""
  provider: ""
  max_image_bytes: 5242880
  max_dimension: 2048
  jpeg_quality: 82
  max_payload_bytes: 524288
  detail: auto
  timeout_seconds: 60
```

Empty `api_key`, `base_url`, or `provider` inherits from the resolved default AI channel.

## Data Handling

Image bytes are sent only to the vision model call. Agent history keeps text summaries, not raw image bytes. This reduces context size and accidental image propagation.

## Preprocessing

The runtime can resize and recompress large images based on:

- maximum file size;
- maximum dimension;
- JPEG quality;
- encoded payload size.

If small images are already under limits, preprocessing may be skipped.

## Usage Guidance

Use vision for:

- UI screenshots;
- visual vulnerability evidence;
- captcha or image-based prompts in authorized tests;
- interpreting tool screenshots.

Do not use it for:

- unrelated personal images;
- sensitive screenshots without authorization;
- long-term storage of raw evidence when a text summary is enough.

## Source Anchors

- Tool registration: `internal/app/vision_tools.go`
- Client: `internal/vision/client.go`
- Preprocess: `internal/vision/preprocess.go`
- Config: `internal/config/vision.go`
