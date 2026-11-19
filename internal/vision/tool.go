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
			logger.Warn("vision.enabled 但 vision.model 为空，跳过注册 analyze_image")
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
		Description: "分析服务器上的本地图片并返回文字描述（验证码、UI 元素、报错、架构图要点等）。" +
			"输入为文件路径（如用户上传的 chat_uploads 路径或工具截图路径）。" +
			"输出仅为文本，不含图片数据。不要对二进制图片使用 read_file 指望理解内容。",
		ShortDescription: "分析本地图片并返回文字描述（验证码/UI/报错等）",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "服务器本地图片的绝对路径或相对进程工作目录的路径，文件名须使用实际图片格式对应的扩展名，例如 .png 或 .jpg",
				},
				"question": map[string]interface{}{
					"type":        "string",
					"description": "可选：希望模型重点回答的问题。验证码图建议：只输出验证码字符，不要空格和解释",
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
			return textResult(fmt.Sprintf("路径校验失败: %v", err), true), nil
		}

		img, meta, err := PreprocessImageFile(abs, preOpt)
		if err != nil {
			return textResult(fmt.Sprintf("图片预处理失败: %v", err), true), nil
		}

		summary, err := client.Analyze(ctx, img, question)
		if err != nil {
			return textResult(fmt.Sprintf("视觉模型调用失败: %v", err), true), nil
		}

		body := formatAnalysisResult(abs, meta, summary)
		return textResult(body, false), nil
	}

	mcpServer.RegisterTool(tool, handler)
	if logger != nil {
		logger.Debug("vision: analyze_image 工具已注册", zap.String("model", cfg.Vision.Model))
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
