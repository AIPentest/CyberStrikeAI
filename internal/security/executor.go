package security

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/tooloutput"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ToolOutputCallback 用于在工具执行过程中把 stdout/stderr 增量推给上层（SSE）。
// 通过 context 传递，避免修改 MCP ToolHandler 签名导致的“写死工具”问题。
type ToolOutputCallback func(chunk string)

type toolOutputCallbackCtxKey struct{}

// ToolOutputCallbackCtxKey 是 context 中的 key，供 Agent 写入回调，Executor 读取并流式回调。
var ToolOutputCallbackCtxKey = toolOutputCallbackCtxKey{}

// Executor 安全工具执行器
type Executor struct {
	config                  *config.SecurityConfig
	toolIndex               map[string]*config.ToolConfig // 工具索引，用于 O(1) 查找
	mcpServer               *mcp.Server
	logger                  *zap.Logger
	shellNoOutputTimeoutSec int // execute/exec 无新输出空闲秒数；0=默认 300；-1=关闭（见 SetShellNoOutputTimeoutSeconds）
	toolOutputMaxBytes      int
	spillRootDir            string
}

// NewExecutor 创建新的执行器
func NewExecutor(cfg *config.SecurityConfig, mcpServer *mcp.Server, logger *zap.Logger) *Executor {
	executor := &Executor{
		config:    cfg,
		toolIndex: make(map[string]*config.ToolConfig),
		mcpServer: mcpServer,
		logger:    logger,
	}
	// 构建工具索引
	executor.buildToolIndex()
	return executor
}

// SetShellNoOutputTimeoutSeconds 配置 exec 工具无输出空闲终止（与 agent.shell_no_output_timeout_seconds 一致）。
func (e *Executor) SetShellNoOutputTimeoutSeconds(sec int) {
	e.shellNoOutputTimeoutSec = sec
}

// SetToolOutputMaxBytes limits stdout/stderr retained and streamed by exec-like
// tools. It should stay aligned with MCP result normalization so every channel
// sees the same bounded payload. Oversized full output is spilled to disk first.
func (e *Executor) SetToolOutputMaxBytes(maxBytes int) {
	e.toolOutputMaxBytes = maxBytes
}

// SetToolOutputSpillRoot sets the reduction-compatible root for spilling full
// exec stdout/stderr when the in-memory bound is exceeded (empty → tmp/reduction).
func (e *Executor) SetToolOutputSpillRoot(rootDir string) {
	e.spillRootDir = strings.TrimSpace(rootDir)
}

func (e *Executor) wrapToolOutputCallback(ctx context.Context, cb ToolOutputCallback) ToolOutputCallback {
	executionID := mcp.MCPExecutionIDFromContext(ctx)
	if e == nil || e.mcpServer == nil || strings.TrimSpace(executionID) == "" {
		return cb
	}
	return func(chunk string) {
		if chunk != "" {
			e.mcpServer.AppendToolExecutionPartialOutput(executionID, chunk)
		}
		if cb != nil {
			cb(chunk)
		}
	}
}

func (e *Executor) spillOptsFromContext(ctx context.Context) tooloutput.SpillOpts {
	root := ""
	if e != nil {
		root = e.spillRootDir
	}
	opts := tooloutput.SpillOpts{RootDir: root}
	if ctx != nil {
		opts.ConversationID = mcp.MCPConversationIDFromContext(ctx)
		opts.ProjectID = mcp.MCPProjectIDFromContext(ctx)
		opts.ExecutionID = mcp.MCPExecutionIDFromContext(ctx)
	}
	if opts.ExecutionID == "" {
		opts.ExecutionID = uuid.NewString()
	}
	return opts
}

// buildToolIndex 构建工具索引，将 O(n) 查找优化为 O(1)
func (e *Executor) buildToolIndex() {
	e.toolIndex = make(map[string]*config.ToolConfig)
	for i := range e.config.Tools {
		if e.config.Tools[i].Enabled {
			e.toolIndex[e.config.Tools[i].Name] = &e.config.Tools[i]
		}
	}
	e.logger.Debug("工具索引构建完成",
		zap.Int("totalTools", len(e.config.Tools)),
		zap.Int("enabledTools", len(e.toolIndex)),
	)
}

// ExecuteTool 执行安全工具
func (e *Executor) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*mcp.ToolResult, error) {
	e.logger.Debug("ExecuteTool被调用",
		zap.String("toolName", toolName),
		zap.Any("args", args),
	)

	// 特殊处理：exec工具直接执行系统命令
	if toolName == "exec" {
		e.logger.Debug("执行exec工具")
		return e.executeSystemCommand(ctx, args)
	}

	// 使用索引查找工具配置（O(1) 查找）
	toolConfig, exists := e.toolIndex[toolName]
	if !exists {
		e.logger.Error("工具未找到或未启用",
			zap.String("toolName", toolName),
			zap.Int("totalTools", len(e.config.Tools)),
			zap.Int("enabledTools", len(e.toolIndex)),
		)
		return nil, fmt.Errorf("工具 %s 未找到或未启用", toolName)
	}

	e.logger.Debug("找到工具配置",
		zap.String("toolName", toolName),
		zap.String("command", toolConfig.Command),
		zap.Strings("args", toolConfig.Args),
	)

	// 特殊处理：内部工具（command 以 "internal:" 开头）
	if strings.HasPrefix(toolConfig.Command, "internal:") {
		e.logger.Debug("执行内部工具",
			zap.String("toolName", toolName),
			zap.String("command", toolConfig.Command),
		)
		return e.executeInternalTool(ctx, toolName, toolConfig.Command, args)
	}

	// 构建命令 - 根据工具类型使用不同的参数格式
	cmdArgs := e.buildCommandArgs(toolName, toolConfig, args)

	e.logger.Debug("构建命令参数完成",
		zap.String("toolName", toolName),
		zap.Strings("cmdArgs", cmdArgs),
		zap.Int("argsCount", len(cmdArgs)),
	)

	// 验证命令参数
	if len(cmdArgs) == 0 {
		e.logger.Warn("命令参数为空",
			zap.String("toolName", toolName),
			zap.Any("inputArgs", args),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("错误: 工具 %s 缺少必需的参数。接收到的参数: %v", toolName, args),
				},
			},
			IsError: true,
		}, nil
	}

	// 执行命令
	cmd := exec.CommandContext(ctx, toolConfig.Command, cmdArgs...)
	applyDefaultTerminalEnv(cmd)
	attachNonInteractiveStdin(cmd)
	_ = prepareShellCmdSession(cmd)

	e.logger.Debug("执行安全工具",
		zap.String("tool", toolName),
		zap.Strings("args", cmdArgs),
	)

	var output string
	var err error
	spill := e.spillOptsFromContext(ctx)
	// 如果上层提供了 stdout/stderr 增量回调，或当前处于 MCP execution 中，则边执行边读取并回调。
	if cb, ok := ctx.Value(ToolOutputCallbackCtxKey).(ToolOutputCallback); (ok && cb != nil) || mcp.MCPExecutionIDFromContext(ctx) != "" {
		cb = e.wrapToolOutputCallback(ctx, cb)
		output, err = streamCommandOutput(ctx, cmd, cb, ResolveShellNoOutputTimeoutSeconds(e.shellNoOutputTimeoutSec), e.toolOutputMaxBytes, spill)
		if err != nil && shouldRetryWithPTY(output) {
			e.logger.Info("检测到工具需要 TTY，使用 PTY 重试",
				zap.String("tool", toolName),
			)
			cmd2 := exec.CommandContext(ctx, toolConfig.Command, cmdArgs...)
			applyDefaultTerminalEnv(cmd2)
			_ = prepareShellCmdSession(cmd2)
			output, err = runCommandWithPTY(ctx, cmd2, cb, e.toolOutputMaxBytes, spill)
		}
	} else {
		// 非流式：内存缓冲 + ctx 取消杀进程组；行为对齐原 CombinedOutput，避免双流管道 fan-in 死锁。
		output, err = combinedOutputCancellableWithLimit(ctx, cmd, e.toolOutputMaxBytes, spill)
		if err != nil && shouldRetryWithPTY(output) {
			e.logger.Info("检测到工具需要 TTY，使用 PTY 重试",
				zap.String("tool", toolName),
			)
			cmd2 := exec.CommandContext(ctx, toolConfig.Command, cmdArgs...)
			applyDefaultTerminalEnv(cmd2)
			_ = prepareShellCmdSession(cmd2)
			output, err = runCommandWithPTY(ctx, cmd2, nil, e.toolOutputMaxBytes, spill)
		}
	}
	if err != nil {
		// 检查退出码是否在允许列表中
		exitCode := getExitCode(err)
		if exitCode != nil && toolConfig.AllowedExitCodes != nil {
			for _, allowedCode := range toolConfig.AllowedExitCodes {
				if *exitCode == allowedCode {
					e.logger.Debug("工具执行完成（退出码在允许列表中）",
						zap.String("tool", toolName),
						zap.Int("exitCode", *exitCode),
						zap.String("output", string(output)),
					)
					return &mcp.ToolResult{
						Content: []mcp.Content{
							{
								Type: "text",
								Text: string(output),
							},
						},
						IsError: false,
					}, nil
				}
			}
		}

		e.logger.Error("工具执行失败",
			zap.String("tool", toolName),
			zap.Error(err),
			zap.Int("exitCode", getExitCodeValue(err)),
			zap.String("output", string(output)),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("工具执行失败: %v\n输出: %s", err, string(output)),
				},
			},
			IsError: true,
		}, nil
	}

	e.logger.Debug("工具执行成功",
		zap.String("tool", toolName),
		zap.String("output", string(output)),
	)

	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: string(output),
			},
		},
		IsError: false,
	}, nil
}

// RegisterTools 注册工具到MCP服务器
func (e *Executor) RegisterTools(mcpServer *mcp.Server) {
	e.logger.Debug("开始注册工具",
		zap.Int("totalTools", len(e.config.Tools)),
		zap.Int("enabledTools", len(e.toolIndex)),
	)

	// 重新构建索引（以防配置更新）
	e.buildToolIndex()

	for i, toolConfig := range e.config.Tools {
		if !toolConfig.Enabled {
			e.logger.Debug("跳过未启用的工具",
				zap.String("tool", toolConfig.Name),
			)
			continue
		}

		// 创建工具配置的副本，避免闭包问题
		toolName := toolConfig.Name
		toolConfigCopy := toolConfig

		// 根据配置决定暴露给 AI/API 的描述：short_description 或 description
		useFullDescription := strings.TrimSpace(strings.ToLower(e.config.ToolDescriptionMode)) == "full"
		shortDesc := toolConfigCopy.ShortDescription
		if shortDesc == "" {
			// 如果没有简短描述，从详细描述中提取第一行或前10000个字符
			desc := toolConfigCopy.Description
			if len(desc) > 10000 {
				if idx := strings.Index(desc, "\n"); idx > 0 && idx < 10000 {
					shortDesc = strings.TrimSpace(desc[:idx])
				} else {
					shortDesc = desc[:10000] + "..."
				}
			} else {
				shortDesc = desc
			}
		}
		if useFullDescription {
			shortDesc = "" // 使用 description 时清空 ShortDescription，下游会回退到 Description
		}

		tool := mcp.Tool{
			Name:             toolConfigCopy.Name,
			Description:      toolConfigCopy.Description,
			ShortDescription: shortDesc,
			InputSchema:      e.buildInputSchema(&toolConfigCopy),
		}

		handler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
			e.logger.Debug("工具handler被调用",
				zap.String("toolName", toolName),
				zap.Any("args", args),
			)
			return e.ExecuteTool(ctx, toolName, args)
		}

		mcpServer.RegisterTool(tool, handler)
		e.logger.Debug("注册安全工具成功",
			zap.String("tool", toolConfigCopy.Name),
			zap.String("command", toolConfigCopy.Command),
			zap.Int("index", i),
		)
	}

	e.logger.Debug("工具注册完成",
		zap.Int("registeredCount", len(e.config.Tools)),
	)
}

// buildCommandArgs 构建命令参数
func (e *Executor) buildCommandArgs(toolName string, toolConfig *config.ToolConfig, args map[string]interface{}) []string {
	cmdArgs := make([]string, 0)

	// 如果配置中定义了参数映射，使用配置中的映射规则
	if len(toolConfig.Parameters) > 0 {
		// 检查是否有 scan_type 参数，如果有则替换默认的扫描类型参数
		hasScanType := false
		var scanTypeValue string
		if scanType, ok := args["scan_type"].(string); ok && scanType != "" {
			hasScanType = true
			scanTypeValue = scanType
		}

		// 添加固定参数（如果指定了 scan_type，可能需要过滤掉默认的扫描类型参数）
		if hasScanType && toolName == "nmap" {
			// 对于 nmap，如果指定了 scan_type，跳过默认的 -sT -sV -sC
			// 这些参数会被 scan_type 参数替换
		} else {
			cmdArgs = append(cmdArgs, toolConfig.Args...)
		}

		// 按位置参数排序
		positionalParams := make([]config.ParameterConfig, 0)
		flagParams := make([]config.ParameterConfig, 0)

		for _, param := range toolConfig.Parameters {
			if param.Position != nil {
				positionalParams = append(positionalParams, param)
			} else {
				flagParams = append(flagParams, param)
			}
		}

		// 对于需要子命令的工具（如 gobuster dir），position 0 必须紧跟在命令名后、所有 flag 之前
		for _, param := range positionalParams {
			if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
				continue
			}
			if param.Position != nil && *param.Position == 0 {
				value := e.getParamValue(args, param)
				if value == nil && param.Default != nil {
					value = param.Default
				}
				if value != nil {
					cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
				}
				break
			}
		}

		// 处理标志参数
		for _, param := range flagParams {
			// 跳过特殊参数，它们会在后面单独处理
			// action 参数仅用于工具内部逻辑，不传递给命令
			if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
				continue
			}

			value := e.getParamValue(args, param)
			if value == nil {
				if param.Required {
					// 必需参数缺失，返回空数组让上层处理错误
					e.logger.Warn("缺少必需的标志参数",
						zap.String("tool", toolName),
						zap.String("param", param.Name),
					)
					return []string{}
				}
				continue
			}

			// 布尔值特殊处理：如果为 false，跳过；如果为 true，只添加标志
			if param.Type == "bool" {
				var boolVal bool
				var ok bool

				// 尝试多种类型转换
				if boolVal, ok = value.(bool); ok {
					// 已经是布尔值
				} else if numVal, ok := value.(float64); ok {
					// JSON 数字类型（float64）
					boolVal = numVal != 0
					ok = true
				} else if numVal, ok := value.(int); ok {
					// int 类型
					boolVal = numVal != 0
					ok = true
				} else if strVal, ok := value.(string); ok {
					// 字符串类型
					boolVal = strVal == "true" || strVal == "1" || strVal == "yes"
					ok = true
				}

				if ok {
					if !boolVal {
						continue // false 时不添加任何参数
					}
					// true 时只添加标志，不添加值
					if param.Flag != "" {
						cmdArgs = append(cmdArgs, param.Flag)
					}
					continue
				}
			}

			formattedValue := e.formatParamValue(param, value)
			if strings.TrimSpace(formattedValue) == "" {
				if param.Required {
					e.logger.Warn("必需参数为空",
						zap.String("tool", toolName),
						zap.String("param", param.Name),
					)
					return []string{}
				}
				continue
			}

			format := param.Format
			if format == "" {
				format = "flag" // 默认格式
			}

			switch format {
			case "flag":
				// --flag value 或 -f value
				if param.Flag != "" {
					cmdArgs = append(cmdArgs, param.Flag)
				}
				cmdArgs = append(cmdArgs, formattedValue)
			case "combined":
				// --flag=value 或 -f=value
				if param.Flag != "" {
					cmdArgs = append(cmdArgs, fmt.Sprintf("%s=%s", param.Flag, formattedValue))
				} else {
					cmdArgs = append(cmdArgs, formattedValue)
				}
			case "template":
				// 使用模板字符串
				if param.Template != "" {
					template := param.Template
					template = strings.ReplaceAll(template, "{flag}", param.Flag)
					template = strings.ReplaceAll(template, "{value}", formattedValue)
					template = strings.ReplaceAll(template, "{name}", param.Name)
					cmdArgs = append(cmdArgs, strings.Fields(template)...)
				} else {
					// 如果没有模板，使用默认格式
					if param.Flag != "" {
						cmdArgs = append(cmdArgs, param.Flag)
					}
					cmdArgs = append(cmdArgs, formattedValue)
				}
			case "positional":
				// 位置参数（已在上面处理）
				cmdArgs = append(cmdArgs, formattedValue)
			default:
				// 默认：直接添加值
				cmdArgs = append(cmdArgs, formattedValue)
			}
		}

		// 然后处理位置参数（位置参数通常在标志参数之后）
		// 对位置参数按位置排序
		// 首先找到最大的位置值，确定需要处理多少个位置
		maxPosition := -1
		for _, param := range positionalParams {
			if param.Position != nil && *param.Position > maxPosition {
				maxPosition = *param.Position
			}
		}

		// 按位置顺序处理参数，确保即使某些位置没有参数或使用默认值，也能正确传递
		// position 0 已在前面插入（子命令优先），此处从 1 开始
		for i := 0; i <= maxPosition; i++ {
			if i == 0 {
				continue
			}
			for _, param := range positionalParams {
				// 跳过特殊参数，它们会在后面单独处理
				// action 参数仅用于工具内部逻辑，不传递给命令
				if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
					continue
				}

				if param.Position != nil && *param.Position == i {
					value := e.getParamValue(args, param)
					if value == nil {
						if param.Required {
							// 必需参数缺失，返回空数组让上层处理错误
							e.logger.Warn("缺少必需的位置参数",
								zap.String("tool", toolName),
								zap.String("param", param.Name),
								zap.Int("position", *param.Position),
							)
							return []string{}
						}
						// 对于非必需参数，如果值为 nil，尝试使用默认值
						if param.Default != nil {
							value = param.Default
						} else {
							// 如果没有默认值，跳过这个位置，继续处理下一个位置
							break
						}
					}
					// 只有当值不为 nil 时才添加到命令参数中
					if value != nil {
						cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
					}
					break
				}
			}
			// 如果某个位置没有找到对应的参数，继续处理下一个位置
			// 这样可以确保位置参数的顺序正确
		}

		// 特殊处理：additional_args 参数（需要按空格分割成多个参数）
		if additionalArgs, ok := args["additional_args"].(string); ok && additionalArgs != "" {
			// 按空格分割，但保留引号内的内容
			additionalArgsList := e.parseAdditionalArgs(additionalArgs)
			cmdArgs = append(cmdArgs, additionalArgsList...)
		}

		// 特殊处理：scan_type 参数（需要按空格分割并插入到合适位置）
		if hasScanType {
			scanTypeArgs := e.parseAdditionalArgs(scanTypeValue)
			if len(scanTypeArgs) > 0 {
				// 对于 nmap，scan_type 应该替换默认的扫描类型参数
				// 由于我们已经跳过了默认的 args，现在需要将 scan_type 插入到合适位置
				// 找到 target 参数的位置（通常是最后一个位置参数）
				insertPos := len(cmdArgs)
				for i := len(cmdArgs) - 1; i >= 0; i-- {
					// target 通常是最后一个非标志参数
					if !strings.HasPrefix(cmdArgs[i], "-") {
						insertPos = i
						break
					}
				}
				// 在 target 之前插入 scan_type 参数
				newArgs := make([]string, 0, len(cmdArgs)+len(scanTypeArgs))
				newArgs = append(newArgs, cmdArgs[:insertPos]...)
				newArgs = append(newArgs, scanTypeArgs...)
				newArgs = append(newArgs, cmdArgs[insertPos:]...)
				cmdArgs = newArgs
			}
		}

		return cmdArgs
	}

	// 如果没有定义参数配置，使用固定参数和通用处理
	// 添加固定参数
	cmdArgs = append(cmdArgs, toolConfig.Args...)

	// 通用处理：将参数转换为命令行参数
	for key, value := range args {
		if key == "_tool_name" {
			continue
		}
		// 使用 --key value 格式
		cmdArgs = append(cmdArgs, fmt.Sprintf("--%s", key))
		if strValue, ok := value.(string); ok {
			cmdArgs = append(cmdArgs, strValue)
		} else {
			cmdArgs = append(cmdArgs, fmt.Sprintf("%v", value))
		}
	}

	return cmdArgs
}

// parseAdditionalArgs 解析 additional_args 字符串，按空格分割但保留引号内的内容
func (e *Executor) parseAdditionalArgs(argsStr string) []string {
	if argsStr == "" {
		return []string{}
	}

	result := make([]string, 0)
	var current strings.Builder
	inQuotes := false
	var quoteChar rune
	escapeNext := false

	runes := []rune(argsStr)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' {
			// 检查下一个字符是否是引号
			if i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\'') {
				// 转义的引号：跳过反斜杠，将引号作为普通字符写入
				i++
				current.WriteRune(runes[i])
			} else {
				// 其他转义字符：写入反斜杠，下一个字符会在下次迭代处理
				escapeNext = true
				current.WriteRune(r)
			}
			continue
		}

		if !inQuotes && (r == '"' || r == '\'') {
			inQuotes = true
			quoteChar = r
			continue
		}

		if inQuotes && r == quoteChar {
			inQuotes = false
			quoteChar = 0
			continue
		}

		if !inQuotes && (r == ' ' || r == '\t' || r == '\n') {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	// 处理最后一个参数（如果存在）
	if current.Len() > 0 {
		result = append(result, current.String())
	}

	// 如果解析结果为空，使用简单的空格分割作为降级方案
	if len(result) == 0 {
		result = strings.Fields(argsStr)
	}

	return result
}

// getParamValue 获取参数值，支持默认值
func (e *Executor) getParamValue(args map[string]interface{}, param config.ParameterConfig) interface{} {
	// 从参数中获取值
	if value, ok := args[param.Name]; ok && value != nil {
		return value
	}

	// 如果参数是必需的但没有提供，返回 nil（让上层处理错误）
	if param.Required {
		return nil
	}

	// 返回默认值
	return param.Default
}

// formatParamValue 格式化参数值
func (e *Executor) formatParamValue(param config.ParameterConfig, value interface{}) string {
	switch param.Type {
	case "bool":
		// 布尔值应该在上层处理，这里不应该被调用
		if boolVal, ok := value.(bool); ok {
			return fmt.Sprintf("%v", boolVal)
		}
		return "false"
	case "array":
		// 数组：转换为逗号分隔的字符串
		if arr, ok := value.([]interface{}); ok {
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				strs = append(strs, fmt.Sprintf("%v", item))
			}
			return strings.Join(strs, ",")
		}
		return fmt.Sprintf("%v", value)
	case "object":
		// 对象/字典：序列化为 JSON 字符串
		if jsonBytes, err := json.Marshal(value); err == nil {
			return string(jsonBytes)
		}
		// 如果 JSON 序列化失败，回退到默认格式化
		return fmt.Sprintf("%v", value)
	default:
		formattedValue := fmt.Sprintf("%v", value)
		// 特殊处理：对于 ports 参数（通常是 nmap 等工具的端口参数），清理空格
		// nmap 不接受端口列表中有空格，例如 "80,443, 22" 应该变成 "80,443,22"
		if param.Name == "ports" {
			// 移除所有空格，但保留逗号和其他字符
			formattedValue = strings.ReplaceAll(formattedValue, " ", "")
		}
		return formattedValue
	}
}

// IsBackgroundShellCommand 检测命令是否为完全后台命令（末尾有独立 &，且不在引号内）。
// command1 & command2 不算完全后台（command2 仍在前台执行）。
func IsBackgroundShellCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	positions := findStandaloneAmpersandPositions(command)
	if len(positions) == 0 {
		return false
	}
	last := positions[len(positions)-1]
	afterAmpersand := strings.TrimSpace(command[last+1:])
	if afterAmpersand != "" {
		return false
	}
	beforeAmpersand := strings.TrimSpace(command[:last])
	return beforeAmpersand != ""
}

// executeSystemCommand 执行系统命令
func (e *Executor) executeSystemCommand(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
	// 获取命令
	command, ok := args["command"].(string)
	if !ok {
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: "错误: 缺少command参数",
				},
			},
			IsError: true,
		}, nil
	}

	if command == "" {
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: "错误: command参数不能为空",
				},
			},
			IsError: true,
		}, nil
	}

	// 安全检查：记录执行的命令
	e.logger.Warn("执行系统命令",
		zap.String("command", command),
	)

	command = PrepareShellCommandForExecute(command)

	// 获取shell类型（可选，默认为sh）
	shell := "sh"
	if s, ok := args["shell"].(string); ok && s != "" {
		shell = s
	}

	// 获取工作目录（可选）
	workDir := ""
	if wd, ok := args["workdir"].(string); ok && wd != "" {
		workDir = wd
	}

	// 检测是否为后台命令（包含 & 符号，但不在引号内）
	isBackground := IsBackgroundShellCommand(command)

	// 构建命令
	var cmd *exec.Cmd
	if workDir != "" {
		cmd = exec.CommandContext(ctx, shell, "-c", command)
		cmd.Dir = workDir
	} else {
		cmd = exec.CommandContext(ctx, shell, "-c", command)
	}
	ConfigureShellCmdForAgentExecute(cmd)

	// 执行命令
	e.logger.Info("执行系统命令",
		zap.String("command", command),
		zap.String("shell", shell),
		zap.String("workdir", workDir),
		zap.Bool("isBackground", isBackground),
	)

	if isBackground {
		job := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(command), "&"))
		session, err := StartManagedBackground(ctx, shell, job, workDir)
		if err != nil {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf("后台命令启动失败: %v", err)}}, IsError: true}, nil
		}
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf("后台命令已启动\n命令: %s\n进程组ID: %d\n\n后台进程由本轮任务托管，任务结束时自动清理。", command, session.rootPID)}}}, nil
	}

	// 非后台命令：等待输出
	var output string
	var err error
	spill := e.spillOptsFromContext(ctx)
	// 若上层提供工具输出增量回调，或当前处于 MCP execution 中，则边执行边流式读取。
	if cb, ok := ctx.Value(ToolOutputCallbackCtxKey).(ToolOutputCallback); (ok && cb != nil) || mcp.MCPExecutionIDFromContext(ctx) != "" {
		cb = e.wrapToolOutputCallback(ctx, cb)
		output, err = streamCommandOutput(ctx, cmd, cb, ResolveShellNoOutputTimeoutSeconds(e.shellNoOutputTimeoutSec), e.toolOutputMaxBytes, spill)
		if err != nil && shouldRetryWithPTY(output) {
			e.logger.Info("检测到系统命令需要 TTY，使用 PTY 重试")
			cmd2 := exec.CommandContext(ctx, shell, "-c", command)
			if workDir != "" {
				cmd2.Dir = workDir
			}
			ConfigureShellCmdForAgentExecute(cmd2)
			output, err = runCommandWithPTY(ctx, cmd2, cb, e.toolOutputMaxBytes, spill)
		}
	} else {
		output, err = combinedOutputCancellableWithLimit(ctx, cmd, e.toolOutputMaxBytes, spill)
		if err != nil && shouldRetryWithPTY(output) {
			e.logger.Info("检测到系统命令需要 TTY，使用 PTY 重试")
			cmd2 := exec.CommandContext(ctx, shell, "-c", command)
			if workDir != "" {
				cmd2.Dir = workDir
			}
			ConfigureShellCmdForAgentExecute(cmd2)
			output, err = runCommandWithPTY(ctx, cmd2, nil, e.toolOutputMaxBytes, spill)
		}
	}
	if err != nil {
		e.logger.Error("系统命令执行失败",
			zap.String("command", command),
			zap.Error(err),
			zap.String("output", string(output)),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: FormatCommandFailureFromErr(err, output),
				},
			},
			IsError: true,
		}, nil
	}

	e.logger.Info("系统命令执行成功",
		zap.String("command", command),
		zap.String("output_length", fmt.Sprintf("%d", len(output))),
	)

	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: string(output),
			},
		},
		IsError: false,
	}, nil
}

// combinedOutputCancellable 行为对齐 cmd.CombinedOutput（stdout/stderr 写入内存缓冲），
// 但在 ctx 取消时 terminateCmdTree 终止整棵进程树。
// 非流式路径不使用双流管道 fan-in，避免 stderr 撑满管道缓冲区时与 stdout 互相阻塞导致死锁。
// 无输出空闲检测由上层 agent.tool_timeout_minutes 兜底，不改变原 CombinedOutput 语义。
func combinedOutputCancellable(ctx context.Context, cmd *exec.Cmd) (string, error) {
	return combinedOutputCancellableWithLimit(ctx, cmd, 0, tooloutput.SpillOpts{})
}

func combinedOutputCancellableWithLimit(ctx context.Context, cmd *exec.Cmd, maxBytes int, spill tooloutput.SpillOpts) (string, error) {
	var tee *tooloutput.Tee
	if maxBytes > 0 {
		tee = tooloutput.NewTee(spill)
		defer func() { _ = tee.Close() }()
	}
	stdoutBuf := newBoundedOutputCollector(maxBytes, tee)
	stderrBuf := newBoundedOutputCollector(maxBytes, tee)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	session, err := StartShellSessionContext(ctx, cmd)
	if err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			TerminateShellCmdSession(session)
		case <-stopWatch:
		}
	}()
	defer close(stopWatch)

	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		waitErr = <-done
		return finalizeJoinedBoundedOutputs(stdoutBuf, stderrBuf, maxBytes, tee), ctx.Err()
	}
	return finalizeJoinedBoundedOutputs(stdoutBuf, stderrBuf, maxBytes, tee), waitErr
}

func joinCommandOutput(stdout, stderr string) string {
	if stderr == "" {
		return stdout
	}
	if stdout == "" {
		return stderr
	}
	return stdout + stderr
}

type boundedOutputCollector struct {
	builder   strings.Builder
	maxBytes  int
	seenBytes int
	truncated bool
	tee       *tooloutput.Tee
}

func newBoundedOutputCollector(maxBytes int, tee *tooloutput.Tee) *boundedOutputCollector {
	return &boundedOutputCollector{maxBytes: maxBytes, tee: tee}
}

func (b *boundedOutputCollector) Write(p []byte) (int, error) {
	b.WriteStringLimited(string(p))
	return len(p), nil
}

func (b *boundedOutputCollector) WriteStringLimited(s string) string {
	if b == nil {
		return ""
	}
	if b.tee != nil {
		_, _ = b.tee.Write([]byte(s))
	}
	if b.maxBytes <= 0 {
		b.seenBytes += len(s)
		b.builder.WriteString(s)
		return s
	}
	b.seenBytes += len(s)
	if b.builder.Len() >= b.maxBytes {
		b.truncated = true
		return ""
	}
	remaining := b.maxBytes - b.builder.Len()
	if len(s) <= remaining {
		b.builder.WriteString(s)
		return s
	}
	kept := truncateStringBytes(s, remaining)
	b.builder.WriteString(kept)
	b.truncated = true
	return kept
}

func (b *boundedOutputCollector) String() string {
	if b == nil {
		return ""
	}
	return b.builder.String()
}

func finalizeJoinedBoundedOutputs(stdout, stderr *boundedOutputCollector, maxBytes int, tee *tooloutput.Tee) string {
	if tee != nil {
		_ = tee.Close()
	}
	truncated := (stdout != nil && stdout.truncated) || (stderr != nil && stderr.truncated)
	seen := 0
	if stdout != nil {
		seen += stdout.seenBytes
	}
	if stderr != nil {
		seen += stderr.seenBytes
	}
	joined := joinCommandOutput(
		func() string {
			if stdout == nil {
				return ""
			}
			return stdout.String()
		}(),
		func() string {
			if stderr == nil {
				return ""
			}
			return stderr.String()
		}(),
	)
	if maxBytes > 0 && !truncated && len(joined) > maxBytes {
		truncated = true
		seen = len(joined)
	}
	path := ""
	if tee != nil {
		path = tee.Path()
	}
	if truncated && maxBytes > 0 {
		if path != "" {
			return tooloutput.FormatPersistedFromFile(path, seen, maxBytes)
		}
		if len(joined) > maxBytes {
			return truncateStringBytes(joined, maxBytes)
		}
		return joined
	}
	if path != "" {
		_ = os.Remove(path)
	}
	if maxBytes > 0 && len(joined) > maxBytes {
		return truncateStringBytes(joined, maxBytes)
	}
	return joined
}

func finalizeBoundedOutput(collector *boundedOutputCollector, maxBytes int, tee *tooloutput.Tee) string {
	if tee != nil {
		_ = tee.Close()
	}
	if collector == nil {
		return ""
	}
	path := ""
	if tee != nil {
		path = tee.Path()
	}
	if collector.truncated && maxBytes > 0 {
		if path != "" {
			return tooloutput.FormatPersistedFromFile(path, collector.seenBytes, maxBytes)
		}
		return truncateStringBytes(collector.String(), maxBytes)
	}
	if path != "" {
		_ = os.Remove(path)
	}
	out := collector.String()
	if maxBytes > 0 && len(out) > maxBytes {
		return tooloutput.BoundWithSpill(out, maxBytes, tooloutput.SpillOpts{})
	}
	return out
}

func limitOutputString(s string, maxBytes int, spill tooloutput.SpillOpts) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	return tooloutput.BoundWithSpill(s, maxBytes, spill)
}

func truncateStringBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	if cut <= 0 {
		return ""
	}
	return s[:cut]
}

// streamCommandOutput 以“边读边回调”的方式读取命令 stdout/stderr。
// 使用定长块读取，避免按行读取在无换行输出时永久阻塞；ctx 取消时终止进程树。
func streamCommandOutput(ctx context.Context, cmd *exec.Cmd, cb ToolOutputCallback, noOutputSec int, maxBytes int, spill tooloutput.SpillOpts) (string, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdoutPipe.Close()
		return "", err
	}
	session, err := StartShellSessionContext(ctx, cmd)
	if err != nil {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		return "", err
	}

	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			TerminateShellCmdSession(session)
		case <-stopWatch:
		}
	}()
	defer close(stopWatch)

	readStop := make(chan struct{})
	defer close(readStop)
	chunks := make(chan string, 64)
	var wg sync.WaitGroup
	readFn := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 8192)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				select {
				case chunks <- string(buf[:n]):
				case <-readStop:
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}

	wg.Add(2)
	go readFn(stdoutPipe)
	go readFn(stderrPipe)

	go func() {
		wg.Wait()
		close(chunks)
	}()

	tee := (*tooloutput.Tee)(nil)
	if maxBytes > 0 {
		tee = tooloutput.NewTee(spill)
		defer func() { _ = tee.Close() }()
	}
	outBuilder := newBoundedOutputCollector(maxBytes, tee)
	var deltaBuilder strings.Builder
	lastFlush := time.Now()

	flush := func() {
		if deltaBuilder.Len() == 0 {
			return
		}
		if cb != nil {
			cb(deltaBuilder.String())
		}
		deltaBuilder.Reset()
		lastFlush = time.Now()
	}

	idleWatch := NewShellInactivityWatch(noOutputSec)
	if idleWatch != nil {
		defer idleWatch.Stop()
	}

	fireInactivity := func() {
		TerminateShellCmdSession(session)
		msg := ShellNoOutputTimeoutMessage(idleWatch.Sec)
		msg = outBuilder.WriteStringLimited(msg)
		if cb != nil {
			cb(msg)
		}
		_ = session.Wait()
	}

chunksLoop:
	for {
		var idleCh <-chan struct{}
		if idleWatch != nil {
			idleCh = idleWatch.Expired
		}
		select {
		case <-ctx.Done():
			TerminateShellCmdSession(session)
			flush()
			_ = session.Wait()
			return outBuilder.String(), ctx.Err()
		case <-idleCh:
			fireInactivity()
			return finalizeBoundedOutput(outBuilder, maxBytes, tee), fmt.Errorf("shell inactivity timeout (%ds)", idleWatch.Sec)
		case chunk, ok := <-chunks:
			if !ok {
				break chunksLoop
			}
			if chunk != "" && idleWatch != nil {
				idleWatch.Bump()
			}
			keptChunk := outBuilder.WriteStringLimited(chunk)
			deltaBuilder.WriteString(keptChunk)
			if deltaBuilder.Len() >= 2048 || time.Since(lastFlush) >= 200*time.Millisecond {
				flush()
			}
		}
	}
	flush()

	// 等待命令结束，返回最终退出状态
	waitErr := session.Wait()
	return finalizeBoundedOutput(outBuilder, maxBytes, tee), waitErr
}

// applyDefaultTerminalEnv 为外部工具补齐常见的终端环境变量。
// 注意：这不会创建 TTY，只是减少某些工具在非交互环境下的“奇怪排版/检测失败”。
func applyDefaultTerminalEnv(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	// 仅在未显式设置 Env 时，继承当前进程环境
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = ApplyNonInteractivePagerEnv(cmd.Env)
	// 如果用户已设置 TERM/COLUMNS/LINES，则不覆盖
	has := func(k string) bool {
		prefix := k + "="
		for _, e := range cmd.Env {
			if strings.HasPrefix(e, prefix) {
				return true
			}
		}
		return false
	}
	if !has("TERM") {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	if !has("COLUMNS") {
		cmd.Env = append(cmd.Env, "COLUMNS=256")
	}
	if !has("LINES") {
		cmd.Env = append(cmd.Env, "LINES=40")
	}
}

func shouldRetryWithPTY(output string) bool {
	o := strings.ToLower(output)
	// autorecon / python termios 常见报错
	if strings.Contains(o, "inappropriate ioctl for device") {
		return true
	}
	if strings.Contains(o, "termios.error") {
		return true
	}
	// 兜底：stdin 不是 tty
	if strings.Contains(o, "not a tty") {
		return true
	}
	return false
}

// runCommandWithPTY 为子进程分配 PTY，适配需要交互式终端的工具（如 autorecon）。
// 若 cb != nil，将持续回调增量输出（用于 SSE）。
func runCommandWithPTY(ctx context.Context, cmd *exec.Cmd, cb ToolOutputCallback, maxBytes int, spill tooloutput.SpillOpts) (string, error) {
	if runtime.GOOS == "windows" {
		// PTY 方案为类 Unix；Windows 走原逻辑
		if cb != nil {
			return streamCommandOutput(ctx, cmd, cb, 0, maxBytes, spill)
		}
		_ = prepareShellCmdSession(cmd)
		return combinedOutputCancellableWithLimit(ctx, cmd, maxBytes, spill)
	}

	_ = prepareShellCmdSession(cmd)
	var ptmx *os.File
	session, err := startShellSessionContext(ctx, cmd, func() error {
		var startErr error
		ptmx, startErr = pty.Start(cmd)
		return startErr
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = ptmx.Close() }()

	// ctx 取消时尽快终止子进程
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = ptmx.Close() // 触发读退出
			session.Terminate()
		case <-done:
		}
	}()
	defer close(done)

	tee := (*tooloutput.Tee)(nil)
	if maxBytes > 0 {
		tee = tooloutput.NewTee(spill)
		defer func() { _ = tee.Close() }()
	}
	outBuilder := newBoundedOutputCollector(maxBytes, tee)
	var deltaBuilder strings.Builder
	lastFlush := time.Now()
	flush := func() {
		if cb == nil || deltaBuilder.Len() == 0 {
			deltaBuilder.Reset()
			lastFlush = time.Now()
			return
		}
		cb(deltaBuilder.String())
		deltaBuilder.Reset()
		lastFlush = time.Now()
	}

	buf := make([]byte, 4096)
	for {
		n, readErr := ptmx.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			// 统一换行为 \n，避免前端错位
			chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
			chunk = strings.ReplaceAll(chunk, "\r", "\n")
			keptChunk := outBuilder.WriteStringLimited(chunk)
			deltaBuilder.WriteString(keptChunk)
			if deltaBuilder.Len() >= 2048 || time.Since(lastFlush) >= 200*time.Millisecond {
				flush()
			}
		}
		if readErr != nil {
			break
		}
	}
	flush()

	waitErr := session.Wait()
	return finalizeBoundedOutput(outBuilder, maxBytes, tee), waitErr
}

// executeInternalTool 执行内部工具（不执行外部命令）
func (e *Executor) executeInternalTool(ctx context.Context, toolName string, command string, args map[string]interface{}) (*mcp.ToolResult, error) {
	internalToolType := strings.TrimPrefix(command, "internal:")
	e.logger.Warn("未知的内部工具",
		zap.String("toolName", toolName),
		zap.String("internalToolType", internalToolType),
	)
	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: fmt.Sprintf("错误: 未知的内部工具类型: %s", internalToolType),
			},
		},
		IsError: true,
	}, nil
}

// buildInputSchema 构建输入模式
func (e *Executor) buildInputSchema(toolConfig *config.ToolConfig) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}

	// 如果配置中定义了参数，优先使用配置中的参数定义
	if len(toolConfig.Parameters) > 0 {
		properties := make(map[string]interface{})
		required := []string{}

		for _, param := range toolConfig.Parameters {
			// 跳过 name 为空的参数（避免 YAML 中 name: null 或空导致非法 schema）
			if strings.TrimSpace(param.Name) == "" {
				e.logger.Debug("跳过无名称的参数",
					zap.String("tool", toolConfig.Name),
					zap.String("type", param.Type),
				)
				continue
			}
			// 转换类型为OpenAI/JSON Schema标准类型（空类型默认为 string）
			openAIType := e.convertToOpenAIType(param.Type)

			prop := map[string]interface{}{
				"type":        openAIType,
				"description": param.Description,
			}

			// JSON Schema/OpenAI 要求 array 类型必须包含 items，否则 API 报 invalid_function_parameters
			if openAIType == "array" {
				itemType := strings.TrimSpace(param.ItemType)
				if itemType == "" {
					itemType = "string"
				}
				prop["items"] = map[string]interface{}{
					"type": e.convertToOpenAIType(itemType),
				}
			}

			// 添加默认值
			if param.Default != nil {
				prop["default"] = param.Default
			}

			// 添加枚举选项
			if len(param.Options) > 0 {
				prop["enum"] = param.Options
			}

			properties[param.Name] = prop

			// 添加到必需参数列表
			if param.Required {
				required = append(required, param.Name)
			}
		}

		schema["properties"] = properties
		schema["required"] = required
		return schema
	}

	// 如果没有定义参数配置，返回空schema
	// 这种情况下工具可能只使用固定参数（args字段）
	// 或者需要通过YAML配置文件定义参数
	e.logger.Warn("工具未定义参数配置，返回空schema",
		zap.String("tool", toolConfig.Name),
	)
	return schema
}

// convertToOpenAIType 将配置中的类型转换为OpenAI/JSON Schema标准类型
func (e *Executor) convertToOpenAIType(configType string) string {
	// 空或 null 类型统一视为 string，避免非法 schema 导致工具调用失败
	if strings.TrimSpace(configType) == "" {
		return "string"
	}
	switch configType {
	case "bool":
		return "boolean"
	case "int", "integer":
		return "number"
	case "float", "double":
		return "number"
	case "string", "array", "object":
		return configType
	default:
		// 默认返回原类型，但记录警告
		e.logger.Warn("未知的参数类型，使用原类型",
			zap.String("type", configType),
		)
		return configType
	}
}

// getExitCode 从错误中提取退出码，如果不是ExitError则返回nil
func getExitCode(err error) *int {
	if err == nil {
		return nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		if exitError.ProcessState != nil {
			exitCode := exitError.ExitCode()
			return &exitCode
		}
	}
	return nil
}

// getExitCodeValue 从错误中提取退出码值，如果不是ExitError则返回-1
func getExitCodeValue(err error) int {
	if code := getExitCode(err); code != nil {
		return *code
	}
	return -1
}
