package project

import "strings"

// VisionImageSectionMarker 图片分析 section 标题（与 AppendVisionImageAnalysisIfReady 注入一致）。
const VisionImageSectionMarker = "## Image analysis"

// VisionImageAnalysisSection 单/多代理共用的图片分析提示（analyze_image；上下文仅保留文字摘要）。
func VisionImageAnalysisSection() string {
	var b strings.Builder
	b.WriteString(VisionImageSectionMarker)
	b.WriteString("\n\n")
	b.WriteString("- When you encounter an image file (screenshot, captcha, login page, report illustration), if the analyze_image tool exists, pass the file path on the server for analysis.\n")
	b.WriteString("- Do not expect read_file to make sense of a binary image; a user message like \"📎 xxx.png: /path\" gives the path you can pass to analyze_image.\n")
	b.WriteString("- For captchas: if one has been saved as a local image from a page or API (e.g. captcha.png), use analyze_image and state in the question \"output only the captcha characters\"; if recognition fails, refresh the captcha, save it again and retry; for complex slider/behavior captchas do not expect a single image recognition to succeed.\n")
	b.WriteString("- When delegating to a sub agent, if the sub task involves captcha/screenshot reading, state the image path and the expected output format in the task description.\n")
	return b.String()
}

// AppendVisionImageAnalysisIfReady 仅在 vision.enabled 且 model 已配置时追加图片分析提示。
func AppendVisionImageAnalysisIfReady(base string, visionReady bool) string {
	if !visionReady {
		return base
	}
	return AppendSystemPromptBlock(base, VisionImageAnalysisSection())
}
