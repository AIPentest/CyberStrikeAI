package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/c2"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/openai"

	"go.uber.org/zap"
)

// Agent: the AI agent
type Agent struct {
	openAIClient        *openai.Client
	config              *config.OpenAIConfig
	agentConfig         *config.AgentConfig
	mcpServer           *mcp.Server
	externalMCPMgr      *mcp.ExternalMCPManager // external MCP manager
	logger              *zap.Logger
	maxIterations       int
	mu                  sync.RWMutex      // mutex added to support concurrent updates
	toolNameMapping     map[string]string // tool name mapping: OpenAI format -> original format (for external MCP tools)
	promptBaseDir       string            // base directory for relative paths when resolving system_prompt_path (usually the directory containing config.yaml)
	toolDescriptionMode string            // tool description mode: "short" | "full", default short
}

type agentConversationIDKey struct{}

func withAgentConversationID(ctx context.Context, id string) context.Context {
	id = strings.TrimSpace(id)
	if id == "" || ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, agentConversationIDKey{}, id)
}

func agentConversationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(agentConversationIDKey{}).(string)
	return v
}

// ConversationIDFromContext returns the conversation ID injected into the current Agent request context (used, e.g., by C2 MCP enqueue and human-in-the-loop gating).
func ConversationIDFromContext(ctx context.Context) string {
	return agentConversationIDFromContext(ctx)
}

// NewAgent creates a new Agent
func NewAgent(cfg *config.OpenAIConfig, agentCfg *config.AgentConfig, mcpServer *mcp.Server, externalMCPMgr *mcp.ExternalMCPManager, logger *zap.Logger, maxIterations int) *Agent {
	// If maxIterations is 0 or negative, use the default value 30
	if maxIterations <= 0 {
		maxIterations = 30
	}

	// Configure the HTTP Transport, optimizing connection management and timeout settings
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   300 * time.Second,
			KeepAlive: 300 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 60 * time.Minute, // response-header timeout: increased to 15 minutes to handle large responses
		DisableKeepAlives:     false,            // enable connection reuse
	}

	// Increase the timeout to 30 minutes to support long-running AI inference
	// Especially when using streaming responses or handling complex tasks
	httpClient := &http.Client{
		Timeout:   30 * time.Minute, // increased from 5 minutes to 30 minutes
		Transport: transport,
	}
	llmClient := openai.NewClient(cfg, httpClient, logger)

	return &Agent{
		openAIClient:        llmClient,
		config:              cfg,
		agentConfig:         agentCfg,
		mcpServer:           mcpServer,
		externalMCPMgr:      externalMCPMgr,
		logger:              logger,
		maxIterations:       maxIterations,
		toolNameMapping:     make(map[string]string), // initialize the tool name mapping
		toolDescriptionMode: "short",
	}
}

// SetPromptBaseDir sets the base directory for the single-agent system_prompt_path relative path (usually the directory containing config.yaml).
func (a *Agent) SetPromptBaseDir(dir string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.promptBaseDir = strings.TrimSpace(dir)
}

// ChatMessage: a chat message
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// ToolName: tool role only: recovered from the name or tool_name of the Eino/trace JSON, used to construct a ToolMessage on resume.
	ToolName string `json:"tool_name,omitempty"`
	// ReasoningContent corresponds to OpenAI/DeepSeek's reasoning_content; in thinking mode + tool calls it must be sent back on resume (see the DeepSeek docs).
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// ModelFacingTrace is runtime-only metadata: true means Content was already the exact
	// payload seen at the model boundary and must be restored byte-for-byte.
	ModelFacingTrace bool `json:"-"`
	// Internal provenance is restored from trusted trace metadata, never from
	// message text. Keep it out of provider request JSON.
	SummarizationKind         string   `json:"-"`
	GeneratedContextKind      string   `json:"-"`
	OriginalUserIntentEntries []string `json:"-"`
}

// MarshalJSON: custom JSON serialization, converting the arguments in tool_calls to a JSON string
func (cm ChatMessage) MarshalJSON() ([]byte, error) {
	// Build the serialization struct
	aux := map[string]interface{}{
		"role": cm.Role,
	}

	// Add content (if present)
	if cm.Content != "" {
		aux["content"] = cm.Content
	}
	if cm.ReasoningContent != "" {
		aux["reasoning_content"] = cm.ReasoningContent
	}

	// Add tool_call_id (if present)
	if cm.ToolCallID != "" {
		aux["tool_call_id"] = cm.ToolCallID
	}
	if cm.ToolName != "" {
		aux["tool_name"] = cm.ToolName
	}

	// Convert tool_calls, converting arguments to a JSON string
	if len(cm.ToolCalls) > 0 {
		toolCallsJSON := make([]map[string]interface{}, len(cm.ToolCalls))
		for i, tc := range cm.ToolCalls {
			// Convert arguments to a JSON string
			argsJSON := ""
			if tc.Function.Arguments != nil {
				argsBytes, err := json.Marshal(tc.Function.Arguments)
				if err != nil {
					return nil, err
				}
				argsJSON = string(argsBytes)
			}

			toolCallsJSON[i] = map[string]interface{}{
				"id":   tc.ID,
				"type": tc.Type,
				"function": map[string]interface{}{
					"name":      tc.Function.Name,
					"arguments": argsJSON,
				},
			}
		}
		aux["tool_calls"] = toolCallsJSON
	}

	return json.Marshal(aux)
}

// OpenAIRequest: an OpenAI API request
type OpenAIRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []Tool        `json:"tools,omitempty"`
	Stream   bool          `json:"stream,omitempty"`
}

// OpenAIResponse: an OpenAI API response
type OpenAIResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Error   *Error   `json:"error,omitempty"`
}

// Choice: a choice
type Choice struct {
	Message      MessageWithTools `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

// MessageWithTools: a message with tool calls
type MessageWithTools struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Tool: an OpenAI tool definition
type Tool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition: a function definition
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Error: an OpenAI error
type Error struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// ToolCall: a tool call
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall: a function call
type FunctionCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// UnmarshalJSON: custom JSON parsing, handling the case where arguments may be a string or an object
func (fc *FunctionCall) UnmarshalJSON(data []byte) error {
	type Alias FunctionCall
	aux := &struct {
		Name      string      `json:"name"`
		Arguments interface{} `json:"arguments"`
		*Alias
	}{
		Alias: (*Alias)(fc),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	fc.Name = aux.Name

	// Handle the case where arguments may be a string or an object
	switch v := aux.Arguments.(type) {
	case map[string]interface{}:
		fc.Arguments = v
	case string:
		// If it is a string, try to parse it as JSON
		if err := json.Unmarshal([]byte(v), &fc.Arguments); err != nil {
			// If parsing fails, create a map containing the original string
			fc.Arguments = map[string]interface{}{
				"raw": v,
			}
		}
	case nil:
		fc.Arguments = make(map[string]interface{})
	default:
		// Other types, try to convert to a map
		fc.Arguments = map[string]interface{}{
			"value": v,
		}
	}

	return nil
}

// ProgressCallback: the progress-callback function type
type ProgressCallback func(eventType, message string, data interface{})

// EinoSingleAgentSystemInstruction is used by Eino adk.ChatModelAgent.Instruction (includes system_prompt_path).
func (a *Agent) EinoSingleAgentSystemInstruction() string {
	systemPrompt := DefaultSingleAgentSystemPrompt()
	if a.agentConfig != nil {
		if p := strings.TrimSpace(a.agentConfig.SystemPromptPath); p != "" {
			path := p
			a.mu.RLock()
			base := a.promptBaseDir
			a.mu.RUnlock()
			if !filepath.IsAbs(path) && base != "" {
				path = filepath.Join(base, path)
			}
			if b, err := os.ReadFile(path); err != nil {
				a.logger.Warn("Failed to read single-agent system_prompt_path, using the built-in prompt", zap.String("path", path), zap.Error(err))
			} else if s := strings.TrimSpace(string(b)); s != "" {
				systemPrompt = s
			}
		}
	}
	return systemPrompt
}

// getAvailableTools gets the available tools
// Dynamically get the tool list from the MCP server; the description mode is controlled by tool_description_mode
// roleTools: the tool list configured for the role (toolKey format); if empty or nil, use all tools (the default role)
func (a *Agent) getAvailableTools(roleTools []string) []Tool {
	// Build the role tool set (for fast lookup)
	roleToolSet := make(map[string]bool)
	if len(roleTools) > 0 {
		for _, toolKey := range roleTools {
			roleToolSet[toolKey] = true
		}
	}

	// Get all registered internal tools from the MCP server
	mcpTools := a.mcpServer.GetAllTools()

	// Convert to OpenAI-format tool definitions
	tools := make([]Tool, 0, len(mcpTools))
	for _, mcpTool := range mcpTools {
		// If a role tool list is specified, only add the tools in the list
		if len(roleToolSet) > 0 {
			toolKey := mcpTool.Name // built-in tools use the tool name as the key
			if !roleToolSet[toolKey] {
				continue // not in the role tool list, skip
			}
		}
		description := a.pickToolDescription(mcpTool.ShortDescription, mcpTool.Description)

		// Convert the types in the schema to standard OpenAI types
		convertedSchema := a.convertSchemaTypes(mcpTool.InputSchema)

		tools = append(tools, Tool{
			Type: "function",
			Function: FunctionDefinition{
				Name:        mcpTool.Name,
				Description: description, // use the short description to reduce token consumption
				Parameters:  convertedSchema,
			},
		})
	}

	// Get external MCP tools
	if a.externalMCPMgr != nil {
		// Increase the timeout to 30 seconds, since connecting to a remote server through a proxy may take longer
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		externalTools, err := a.externalMCPMgr.GetAllTools(ctx)
		extMap := make(map[string]string)
		if err != nil {
			a.logger.Warn("Failed to get external MCP tools", zap.Error(err))
		} else {
			// Get the external MCP configuration, used to check tool enablement status
			externalMCPConfigs := a.externalMCPMgr.GetConfigs()

			// Add external MCP tools to the tool list (only add enabled tools)
			for _, externalTool := range externalTools {
				// External tools use "mcpName::toolName" as the toolKey
				externalToolKey := externalTool.Name

				// If a role tool list is specified, only add the tools in the list
				if len(roleToolSet) > 0 {
					if !roleToolSet[externalToolKey] {
						continue // not in the role tool list, skip
					}
				}

				// Parse the tool name: mcpName::toolName
				var mcpName, actualToolName string
				if idx := strings.Index(externalTool.Name, "::"); idx > 0 {
					mcpName = externalTool.Name[:idx]
					actualToolName = externalTool.Name[idx+2:]
				} else {
					continue // skip tools with an incorrect format
				}

				// Check whether the tool is enabled
				enabled := false
				if cfg, exists := externalMCPConfigs[mcpName]; exists {
					// First check whether the external MCP is enabled
					if !cfg.ExternalMCPEnable {
						enabled = false // MCP not enabled, all tools are disabled
					} else {
						// MCP is enabled, check the enablement status of each tool
						// If ToolEnabled is empty or the tool is not set, default to enabled (backward compatible)
						if cfg.ToolEnabled == nil {
							enabled = true // tool status not set, default to enabled
						} else if toolEnabled, exists := cfg.ToolEnabled[actualToolName]; exists {
							enabled = toolEnabled // use the configured tool status
						} else {
						enabled = true // tool not in config, default to enabled
						}
					}
				}

				// Only add enabled tools
				if !enabled {
					continue
				}

				description := a.pickToolDescription(externalTool.ShortDescription, externalTool.Description)

				// Convert the types in the schema to standard OpenAI types
				convertedSchema := a.convertSchemaTypes(externalTool.InputSchema)

				// Replace "::" in the tool name with "__" to conform to the OpenAI naming convention
				// OpenAI requires tool names to contain only [a-zA-Z0-9_-]
				openAIName := strings.ReplaceAll(externalTool.Name, "::", "__")

				// Save the name mapping (OpenAI format -> original format)
				extMap[openAIName] = externalTool.Name

				tools = append(tools, Tool{
					Type: "function",
					Function: FunctionDefinition{
						Name:        openAIName, // use a name that conforms to the OpenAI convention
						Description: description,
						Parameters:  convertedSchema,
					},
				})
			}
		}
		a.mu.Lock()
		a.toolNameMapping = extMap
		a.mu.Unlock()
	}

	a.logger.Debug("Getting the available tool list",
		zap.Int("internalTools", len(mcpTools)),
		zap.Int("totalTools", len(tools)),
	)

	return tools
}

func (a *Agent) pickToolDescription(shortDesc, fullDesc string) string {
	a.mu.RLock()
	mode := strings.TrimSpace(strings.ToLower(a.toolDescriptionMode))
	a.mu.RUnlock()
	if mode == "full" {
		return fullDesc
	}
	if shortDesc != "" {
		return shortDesc
	}
	return fullDesc
}

// convertSchemaTypes recursively converts the types in the schema to standard OpenAI types
func (a *Agent) convertSchemaTypes(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	// Create a new copy of the schema
	converted := make(map[string]interface{})
	for k, v := range schema {
		converted[k] = v
	}

	// Convert the types in properties
	if properties, ok := converted["properties"].(map[string]interface{}); ok {
		convertedProperties := make(map[string]interface{})
		for propName, propValue := range properties {
			if prop, ok := propValue.(map[string]interface{}); ok {
				convertedProp := make(map[string]interface{})
				for pk, pv := range prop {
					if pk == "type" {
						// Convert the type
						if typeStr, ok := pv.(string); ok {
							convertedProp[pk] = a.convertToOpenAIType(typeStr)
						} else {
							convertedProp[pk] = pv
						}
					} else {
						convertedProp[pk] = pv
					}
				}
				convertedProperties[propName] = convertedProp
			} else {
				convertedProperties[propName] = propValue
			}
		}
		converted["properties"] = convertedProperties
	}

	return converted
}

// convertToOpenAIType converts the type in the config to a standard OpenAI/JSON Schema type
func (a *Agent) convertToOpenAIType(configType string) string {
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
		// By default, return the original type
		return configType
	}
}

// ToolExecutionResult: an MCP tool execution result (used by the Eino bridge and monitoring persistence).
type ToolExecutionResult struct {
	Result      string
	ExecutionID string
	IsError     bool
	Blocked     bool
}

func buildToolFailureMessage(toolName, detail string, err error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tool call failed\n\n")
	fmt.Fprintf(&b, "Tool name: %s\n", toolName)
	fmt.Fprintf(&b, "Error details: %s", detail)
	return strings.TrimRight(b.String(), "\n")
}

// executeToolViaMCP executes a tool via MCP
// Even if the tool execution fails, return a result rather than an error, so the AI can handle the error case
func (a *Agent) executeToolViaMCP(ctx context.Context, toolName string, args map[string]interface{}) (*ToolExecutionResult, error) {
	a.logger.Info("Executing tool via MCP",
		zap.String("tool", toolName),
		zap.Any("args", args),
	)

	// If it is the record_vulnerability tool, automatically add conversation_id
	if toolName == builtin.ToolRecordVulnerability {
		conversationID := agentConversationIDFromContext(ctx)
		if conversationID != "" {
			args["conversation_id"] = conversationID
			a.logger.Debug("Automatically adding conversation_id to the record_vulnerability tool",
				zap.String("conversation_id", conversationID),
			)
		} else {
			a.logger.Warn("conversation_id is empty when calling the record_vulnerability tool")
		}
	}

	var result *mcp.ToolResult
	var executionID string
	var err error

	// Single tool-execution timeout: prevents a single tool from hanging for a long time (e.g., still showing "executing" after 30 minutes)
	toolCtx := ctx
	var toolCancel context.CancelFunc
	if a.agentConfig != nil && a.agentConfig.ToolTimeoutMinutes > 0 {
		toolCtx, toolCancel = context.WithTimeout(ctx, time.Duration(a.agentConfig.ToolTimeoutMinutes)*time.Minute)
		defer func() {
			if toolCancel != nil {
				toolCancel()
			}
		}()
	}
	// C2 dangerous-task HITL async wait: must bind to the entire Agent run ctx, not the single tool sub-ctx (which is canceled on return)
	toolCtx = c2.WithHITLRunContext(toolCtx, ctx)

	// Check whether it is an external MCP tool (via the tool name mapping)
	a.mu.RLock()
	originalToolName, isExternalTool := a.toolNameMapping[toolName]
	a.mu.RUnlock()

	if isExternalTool && a.externalMCPMgr != nil {
		// Call the external MCP tool using the original tool name
		a.logger.Debug("Calling the external MCP tool",
			zap.String("openAIName", toolName),
			zap.String("originalName", originalToolName),
		)
		result, executionID, err = a.externalMCPMgr.CallTool(toolCtx, originalToolName, args)
	} else {
		// Call the internal MCP tool
		result, executionID, err = a.mcpServer.CallTool(toolCtx, toolName, args)
	}

	// If the call fails (e.g., tool does not exist, timeout), return a friendly error message instead of throwing an exception
	if err != nil {
		detail := err.Error()
		timeoutMinutes := 10
		if a.agentConfig != nil && a.agentConfig.ToolTimeoutMinutes > 0 {
			timeoutMinutes = a.agentConfig.ToolTimeoutMinutes
		}
		if errors.Is(err, context.Canceled) {
			detail = "The tool call was manually terminated (MCP monitoring page). The agent will carry this result into subsequent steps, and the entire task will not be stopped because of this."
		} else if errors.Is(err, context.DeadlineExceeded) {
			detail = fmt.Sprintf("Tool execution exceeded %d minutes and was automatically terminated (adjustable in agent.tool_timeout_minutes in config.yaml)", timeoutMinutes)
		}
		errorMsg := buildToolFailureMessage(toolName, detail, err)

		return &ToolExecutionResult{
			Result:      errorMsg,
			ExecutionID: executionID,
			IsError:     true,
		}, nil // return a nil error, letting the caller handle the result
	}

	// Format the result
	var resultText strings.Builder
	for _, content := range result.Content {
		resultText.WriteString(content.Text)
		resultText.WriteString("\n")
	}

	resultStr := resultText.String()

	return &ToolExecutionResult{
		Result:      resultStr,
		ExecutionID: executionID,
		IsError:     result != nil && result.IsError,
		Blocked:     result != nil && result.Blocked,
	}, nil
}

// UpdateConfig updates the OpenAI configuration
func (a *Agent) UpdateConfig(cfg *config.OpenAIConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = cfg

	a.logger.Info("Agent configuration updated",
		zap.String("base_url", cfg.BaseURL),
		zap.String("model", cfg.Model),
	)
}

// UpdateMaxIterations updates the maximum number of iterations
func (a *Agent) UpdateMaxIterations(maxIterations int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if maxIterations > 0 {
		a.maxIterations = maxIterations
		a.logger.Info("Agent maximum iterations updated", zap.Int("max_iterations", maxIterations))
	}
}

// UpdateToolDescriptionMode updates the tool description mode (short/full)
func (a *Agent) UpdateToolDescriptionMode(mode string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode != "full" {
		mode = "short"
	}
	a.toolDescriptionMode = mode
	a.logger.Debug("Agent tool description mode updated", zap.String("tool_description_mode", mode))
}

// RepairOrphanToolMessages cleans up unpaired tool messages and incomplete tool_calls to avoid OpenAI errors
// It also ensures that tool_calls in historical messages serve only as context memory and do not trigger re-execution
// This is a public method that can be called when restoring historical messages
func (a *Agent) RepairOrphanToolMessages(messages *[]ChatMessage) bool {
	return a.repairOrphanToolMessages(messages)
}

// repairOrphanToolMessages cleans up unpaired tool messages and incomplete tool_calls to avoid OpenAI errors
// It also ensures that tool_calls in historical messages serve only as context memory and do not trigger re-execution
func (a *Agent) repairOrphanToolMessages(messages *[]ChatMessage) bool {
	if messages == nil {
		return false
	}

	msgs := *messages
	if len(msgs) == 0 {
		return false
	}

	pending := make(map[string]int)
	cleaned := make([]ChatMessage, 0, len(msgs))
	removed := false

	for _, msg := range msgs {
		switch strings.ToLower(msg.Role) {
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				// Record all tool_call IDs
				for _, tc := range msg.ToolCalls {
					if tc.ID != "" {
						pending[tc.ID]++
					}
				}
			}
			cleaned = append(cleaned, msg)
		case "tool":
			callID := msg.ToolCallID
			if callID == "" {
				removed = true
				continue
			}
			if count, exists := pending[callID]; exists && count > 0 {
				if count == 1 {
					delete(pending, callID)
				} else {
					pending[callID] = count - 1
				}
				cleaned = append(cleaned, msg)
			} else {
				removed = true
				continue
			}
		default:
			cleaned = append(cleaned, msg)
		}
	}

	// If there are still unmatched tool_calls (i.e., an assistant message has tool_calls but no corresponding tool response)
	// they need to be removed from the last assistant message to avoid the AI re-executing them
	if len(pending) > 0 {
		// Find the last assistant message from the end backward
		for i := len(cleaned) - 1; i >= 0; i-- {
			if strings.ToLower(cleaned[i].Role) == "assistant" && len(cleaned[i].ToolCalls) > 0 {
				// Remove unmatched tool_calls
				originalCount := len(cleaned[i].ToolCalls)
				validToolCalls := make([]ToolCall, 0)
				for _, tc := range cleaned[i].ToolCalls {
					if tc.ID != "" && pending[tc.ID] > 0 {
						// This tool_call has no corresponding tool response, remove it
						removed = true
						delete(pending, tc.ID)
					} else {
						validToolCalls = append(validToolCalls, tc)
					}
				}
				// Update the message's ToolCalls
				if len(validToolCalls) != originalCount {
					cleaned[i].ToolCalls = validToolCalls
					a.logger.Info("Removed incomplete tool_calls to avoid re-execution",
						zap.Int("removed_count", originalCount-len(validToolCalls)),
					)
				}
				break
			}
		}
	}

	if removed {
		a.logger.Warn("Repaired tool messages and tool_calls in the conversation history",
			zap.Int("original_messages", len(msgs)),
			zap.Int("cleaned_messages", len(cleaned)),
		)
		*messages = cleaned
	}

	return removed
}

// ToolsForRole returns tool definitions consistent with the single-Agent loop (OpenAI function format), for orchestration layers such as the Eino DeepAgent to bind MCP tools.
func (a *Agent) ToolsForRole(roleTools []string) []Tool {
	return a.getAvailableTools(roleTools)
}

// ExecuteMCPToolForConversation executes an MCP tool in the specified session context (behavior is consistent with the tool call in the main Agent loop, such as automatically injecting conversation_id).
func (a *Agent) ExecuteMCPToolForConversation(ctx context.Context, conversationID, toolName string, args map[string]interface{}) (*ToolExecutionResult, error) {
	ctx = withAgentConversationID(ctx, conversationID)
	ctx = mcp.WithMCPConversationID(ctx, conversationID)
	return a.executeToolViaMCP(ctx, toolName, args)
}

// BeginLocalToolExecution writes a running status at the start of a tool on a non-CallTool path, for the MCP monitoring page to show "executing".
func (a *Agent) BeginLocalToolExecution(ctx context.Context, toolName string, args map[string]interface{}) string {
	if a == nil || a.mcpServer == nil {
		return ""
	}
	return a.mcpServer.BeginToolExecution(ctx, toolName, args)
}

// FinishLocalToolExecution completes the record created by BeginLocalToolExecution; when executionID is empty, it writes a completed record in one shot.
func (a *Agent) FinishLocalToolExecution(ctx context.Context, executionID, toolName string, args map[string]interface{}, resultText string, invokeErr error) string {
	if a == nil || a.mcpServer == nil {
		return ""
	}
	return a.mcpServer.FinishToolExecution(ctx, executionID, toolName, args, resultText, invokeErr)
}

// AppendLocalToolExecutionPartialOutput records a bounded live-output preview for a running local tool.
func (a *Agent) AppendLocalToolExecutionPartialOutput(executionID, chunk string) {
	if a == nil || a.mcpServer == nil {
		return
	}
	a.mcpServer.AppendToolExecutionPartialOutput(executionID, chunk)
}

func (a *Agent) RegisterLocalToolExecutionCancel(executionID string, cancel context.CancelFunc) {
	if a == nil || a.mcpServer == nil {
		return
	}
	a.mcpServer.RegisterToolExecutionCancel(executionID, cancel)
}

func (a *Agent) UnregisterLocalToolExecutionCancel(executionID string) {
	if a == nil || a.mcpServer == nil {
		return
	}
	a.mcpServer.UnregisterToolExecutionCancel(executionID)
}

// RecordLocalToolExecution writes a tool call completed on a non-CallTool path to the MCP monitoring store (consistent with CallTool persistence) and returns the executionId.
// Used for scenarios such as Eino filesystem execute, so the assistant bubble "penetration testing details" is clickable into monitoring, consistent with regular MCP.
func (a *Agent) RecordLocalToolExecution(ctx context.Context, toolName string, args map[string]interface{}, resultText string, invokeErr error) string {
	return a.FinishLocalToolExecution(ctx, "", toolName, args, resultText, invokeErr)
}

// UpdateMCPExecutionDisplayResult updates the tool result in the monitoring store to the display body sent to the model (after reduction).
func (a *Agent) UpdateMCPExecutionDisplayResult(executionID, resultText string) {
	if a == nil || strings.TrimSpace(executionID) == "" {
		return
	}
	text := resultText
	if strings.TrimSpace(text) == "" {
		text = "(no output)"
	}
	tr := &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: text}},
	}
	if exec := a.mcpExecution(executionID); exec != nil && exec.Result != nil {
		tr.IsError = exec.Result.IsError
		tr.Blocked = exec.Result.Blocked
	}
	if a.mcpServer != nil {
		_ = a.mcpServer.UpdateToolExecutionResult(executionID, tr)
	}
}

// MCPExecutionResultText returns the monitor-facing result text after storage
// guards such as large-output spilling have been applied.
func (a *Agent) MCPExecutionResultText(executionID string) string {
	exec := a.mcpExecution(executionID)
	if exec == nil || exec.Result == nil {
		return ""
	}
	return mcp.ToolResultPlainText(exec.Result)
}

// MCPExecutionStatus returns the recorded outcome independently of model-facing
// text reduction, which can remove the original refusal wording.
func (a *Agent) MCPExecutionStatus(executionID string) string {
	if exec := a.mcpExecution(executionID); exec != nil {
		return exec.Status
	}
	return ""
}

func (a *Agent) mcpExecution(executionID string) *mcp.ToolExecution {
	if a == nil || strings.TrimSpace(executionID) == "" {
		return nil
	}
	if a.mcpServer != nil {
		if exec, ok := a.mcpServer.GetExecution(executionID); ok && exec != nil {
			return exec
		}
	}
	if a.externalMCPMgr != nil {
		if exec, ok := a.externalMCPMgr.GetExecution(executionID); ok {
			return exec
		}
	}
	return nil
}

// CancelMCPToolExecutionWithNote cancels an in-progress MCP tool (internal first, then external), consistent with the monitoring page's "terminate tool"; when note is non-empty, it is merged into the text returned to the model.
func (a *Agent) CancelMCPToolExecutionWithNote(executionID, note string) bool {
	executionID = strings.TrimSpace(executionID)
	note = strings.TrimSpace(note)
	if executionID == "" {
		return false
	}
	if a.mcpServer != nil && a.mcpServer.CancelToolExecutionWithNote(executionID, note) {
		return true
	}
	if a.externalMCPMgr != nil && a.externalMCPMgr.CancelToolExecutionWithNote(executionID, note) {
		return true
	}
	return false
}

// CancelRunningMCPToolsForConversation cancels all currently running internal/external MCP executions
// owned by the conversation. It is used when a session ends or the user stops a task.
func (a *Agent) CancelRunningMCPToolsForConversation(conversationID, note string) int {
	conversationID = strings.TrimSpace(conversationID)
	if a == nil || conversationID == "" {
		return 0
	}
	note = strings.TrimSpace(note)
	seen := make(map[string]struct{})
	cancelled := 0
	cancelIfConversationMatches := func(execID string, get func(string) (*mcp.ToolExecution, bool), cancel func(string, string) bool) {
		execID = strings.TrimSpace(execID)
		if execID == "" {
			return
		}
		if _, ok := seen[execID]; ok {
			return
		}
		seen[execID] = struct{}{}
		exec, ok := get(execID)
		if !ok || exec == nil || strings.TrimSpace(exec.ConversationID) != conversationID {
			return
		}
		if cancel(execID, note) {
			cancelled++
		}
	}
	if a.mcpServer != nil {
		for execID := range a.mcpServer.ActiveRunningExecutionIDs() {
			cancelIfConversationMatches(execID, a.mcpServer.GetExecution, a.mcpServer.CancelToolExecutionWithNote)
		}
	}
	if a.externalMCPMgr != nil {
		for execID := range a.externalMCPMgr.ActiveRunningExecutionIDs() {
			cancelIfConversationMatches(execID, a.externalMCPMgr.GetExecution, a.externalMCPMgr.CancelToolExecutionWithNote)
		}
	}
	return cancelled
}

// extractQuotedToolName tries to extract the referenced tool name from the error message
func extractQuotedToolName(errMsg string) string {
	start := strings.Index(errMsg, "\"")
	if start == -1 {
		return ""
	}
	rest := errMsg[start+1:]
	end := strings.Index(rest, "\"")
	if end == -1 {
		return ""
	}
	return rest[:end]
}
