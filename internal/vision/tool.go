package vision

import (
	"context"
	"fmt"
	"os"
	"strings"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

// RegisterAnalyzeImageTool 在 vision.enabled 且 model 已配置时注册 MCP 工具 analyze_image。
func RegisterAnalyzeImageTool(mcpServer *mcp.Server, cfg *config.Config, logger *zap.Logger) {
	if mcpServer == nil || cfg == nil {
		return
	}
	if !cfg.Vision.Ready() {
		if cfg.Vision.Enabled && logger != nil {
			logger.Warn("vision.enabled is set but vision.model is empty; skipping analyze_image registration")
		}
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		if logger != nil {
			logger.Warn("vision: getwd failed, skip analyze_image", zap.Error(err))
		}
		return
	}

	preOpt := PreprocessOptions{
		MaxImageBytes:            cfg.Vision.MaxImageBytesEffective(),
		MaxDimension:             cfg.Vision.MaxDimensionEffective(),
		JPEGQuality:              cfg.Vision.JPEGQualityEffective(),
		MaxPayloadBytes:          cfg.Vision.MaxPayloadBytesEffective(),
		SkipPreprocessBelowBytes: cfg.Vision.SkipPreprocessBelowBytesEffective(),
	}
	client := NewClient(cfg.Vision, cfg.OpenAI)

	tool := mcp.Tool{
		Name: builtin.ToolAnalyzeImage,
		Description: "Analyze a local image on the server and return a textual description (CAPTCHA, UI elements, errors, architecture diagram highlights, etc.)." +
			"Input is a file path (e.g. a chat_uploads path from a user upload, or a tool screenshot path). " +
			"Output is text only, containing no image data. Do not use read_file on a binary image expecting to understand its content.",
		ShortDescription: "Analyze a local image and return a textual description (CAPTCHA/UI/errors, etc.)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute image path, or a path relative to the process working directory",
				},
				"question": map[string]interface{}{
					"type":        "string",
					"description": "Optional: the question you want the model to focus on answering. For CAPTCHA images we suggest: output only the CAPTCHA characters, with no spaces or explanation",
				},
			},
			"required": []string{"path"},
		},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		path, _ := args["path"].(string)
		question, _ := args["question"].(string)

		abs, err := ResolveImagePath(path, cwd)
		if err != nil {
			return textResult(fmt.Sprintf("Path validation failed: %v", err), true), nil
		}

		img, meta, err := PreprocessImageFile(abs, preOpt)
		if err != nil {
			return textResult(fmt.Sprintf("Image preprocessing failed: %v", err), true), nil
		}

		summary, err := client.Analyze(ctx, img, question)
		if err != nil {
			return textResult(fmt.Sprintf("Vision model call failed: %v", err), true), nil
		}

		body := formatAnalysisResult(abs, meta, summary)
		return textResult(body, false), nil
	}

	mcpServer.RegisterTool(tool, handler)
	if logger != nil {
		logger.Debug("vision: analyze_image tool registered", zap.String("model", cfg.Vision.Model))
	}
}

func textResult(text string, isError bool) *mcp.ToolResult {
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: text}},
		IsError: isError,
	}
}

func formatAnalysisResult(path string, meta PreprocessMeta, summary string) string {
	var b strings.Builder
	b.WriteString("## Image analysis\n")
	b.WriteString("- **path**: ")
	b.WriteString(path)
	b.WriteString("\n")
	switch meta.PreprocessMode {
	case "passthrough":
		b.WriteString(fmt.Sprintf("- **preprocess**: passthrough %dx%d, %s, %dKB (original %dKB)\n\n",
			meta.OutputWidth, meta.OutputHeight, meta.OutputMIMEType,
			(meta.OutputBytes+1023)/1024, (meta.OriginalBytes+1023)/1024))
	default:
		b.WriteString(fmt.Sprintf("- **preprocess**: %dx%d → %dx%d, jpeg q=%d, %dKB (original %dKB)\n\n",
			meta.OriginalWidth, meta.OriginalHeight,
			meta.OutputWidth, meta.OutputHeight,
			meta.JPEGQuality, (meta.OutputBytes+1023)/1024,
			(meta.OriginalBytes+1023)/1024))
	}
	b.WriteString("### Summary\n")
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n")
	return b.String()
}
