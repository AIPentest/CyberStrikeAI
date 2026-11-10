package project

import "strings"

// VisionImageSectionMarker The image-analysis section title (consistent with the AppendVisionImageAnalysisIfReady injection).
const VisionImageSectionMarker = "## Image analysis"

// VisionImageAnalysisSection The image-analysis prompt shared by single-/multi-agent (analyze_image; only a text summary is kept in context).
func VisionImageAnalysisSection() string {
	var b strings.Builder
	b.WriteString(VisionImageSectionMarker)
	b.WriteString("\n\n")
	b.WriteString("- When you encounter an image file (screenshot, CAPTCHA, login page, report figure), if the analyze_image tool exists, pass the file path on the server for analysis.\n")
	b.WriteString("- Do not use read_file on a binary image expecting to understand its content; in the user's message'📎 xxx.png: /path'is the path that can be passed to analyze_image.\n")
	b.WriteString("- CAPTCHA type: if already saved from a page or API as a local image (e.g. captcha.png), use analyze_image with question stating 'output only the CAPTCHA characters'; on failure, refresh the CAPTCHA, save it again, and re-recognize; do not expect a complex slider/behavioral CAPTCHA to succeed on a single recognition.\n")
	b.WriteString("- When delegating to a sub-agent, if the subtask involves reading a CAPTCHA/screenshot, state the image path and expected output format in the task description.\n")
	return b.String()
}

// AppendVisionImageAnalysisIfReady Appends the image-analysis prompt only when vision.enabled is true and model is configured.
func AppendVisionImageAnalysisIfReady(base string, visionReady bool) string {
	if !visionReady {
		return base
	}
	return AppendSystemPromptBlock(base, VisionImageAnalysisSection())
}
