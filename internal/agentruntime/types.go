// Package agentruntime defines the host-owned, framework-independent Agent protocol.
package agentruntime

import (
	"context"
	"encoding/json"
)

const ProtocolVersion = 1

type Model struct {
	Provider        string            `json:"provider"`
	API             string            `json:"api,omitempty"`
	ID              string            `json:"id"`
	BaseURL         string            `json:"baseUrl"`
	APIKey          string            `json:"apiKey"`
	Headers         map[string]string `json:"headers,omitempty"`
	ContextWindow   int               `json:"contextWindow"`
	MaxTokens       int               `json:"maxTokens"`
	ImageInput      *bool             `json:"imageInput,omitempty"`
	Thinking        string            `json:"thinking,omitempty"`
	ExtraFields     map[string]any    `json:"extraFields,omitempty"`
	Compat          map[string]any    `json:"compat,omitempty"`
	InputBudget     int               `json:"inputBudget,omitempty"`
	CompactToolJSON bool              `json:"compactToolJSON,omitempty"`
}

type Tool struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Parameters       map[string]any  `json:"parameters"`
	OutputSchema     map[string]any  `json:"outputSchema,omitempty"`
	Annotations      map[string]bool `json:"annotations,omitempty"`
	Exposure         string          `json:"exposure,omitempty"`
	Namespace        string          `json:"namespace,omitempty"`
	PromptSnippet    string          `json:"promptSnippet,omitempty"`
	PromptGuidelines []string        `json:"promptGuidelines,omitempty"`
	ExecutionMode    string          `json:"executionMode,omitempty"`
}

// NativeWorkflow opts a background assessment into Pi's native lifecycle hooks.
// Other callers keep their existing execution policy.
type NativeWorkflow struct {
	Receipts       []NativeReceipt `json:"receipts,omitempty"`
	ResumeRequired bool            `json:"resumeRequired,omitempty"`
	Context        string          `json:"context,omitempty"`
	CompletionTool string          `json:"completionTool"`
	CheckpointTool string          `json:"checkpointTool,omitempty"`
	SegmentSeconds int             `json:"segmentSeconds,omitempty"`
	SkillPaths     []string        `json:"skillPaths,omitempty"`
}

type Image struct {
	MIMEType string `json:"mimeType"`
	Data     []byte `json:"data"`
}

type Request struct {
	NativeWorkflow     *NativeWorkflow `json:"nativeWorkflow,omitempty"`
	GenerateOnly       bool            `json:"generateOnly,omitempty"`
	RunID              string          `json:"runId"`
	ConversationID     string          `json:"conversationId"`
	WorkingDir         string          `json:"workingDir,omitempty"`
	SystemPrompt       string          `json:"systemPrompt"`
	Message            string          `json:"message"`
	Images             []Image         `json:"images,omitempty"`
	History            json.RawMessage `json:"history,omitempty"`
	Model              Model           `json:"model"`
	FallbackModels     []Model         `json:"fallbackModels,omitempty"`
	RetryMax           int             `json:"retryMax,omitempty"`
	RetryMaxDelayMS    int             `json:"retryMaxDelayMs,omitempty"`
	Tools              []Tool          `json:"tools"`
	ToolSearch         bool            `json:"toolSearch"`
	ToolSearchMinTools int             `json:"toolSearchMinTools,omitempty"`
	AlwaysVisibleTools []string        `json:"alwaysVisibleTools,omitempty"`
	SessionDir         string          `json:"sessionDir,omitempty"`
	MaxTurns           int             `json:"maxTurns"`
}

type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type ToolCall struct {
	SessionEntryID string         `json:"sessionEntryId,omitempty"`
	ID             string         `json:"id"`
	ProviderID     string         `json:"providerId,omitempty"`
	Name           string         `json:"name"`
	Arguments      map[string]any `json:"arguments"`
}

type ToolResult struct {
	Content           string         `json:"content"`
	IsError           bool           `json:"isError,omitempty"`
	ExecutionID       string         `json:"executionId,omitempty"`
	Terminate         bool           `json:"terminate,omitempty"`
	StructuredContent map[string]any `json:"structuredContent,omitempty"`
}

type Result struct {
	TotalTokens int             `json:"totalTokens,omitempty"`
	Response    string          `json:"response"`
	History     json.RawMessage `json:"history"`
	Status      string          `json:"status"`
	SessionFile string          `json:"sessionFile,omitempty"`
	Error       string          `json:"error,omitempty"`
}

type ToolExecutor func(context.Context, ToolCall) (ToolResult, error)

type Control struct {
	Steer  func(string) error
	Cancel func() error
}

// NativeReceipt associates a durable result with its exact Pi assistant entry.
// Provider tool-call IDs alone are not unique across a multi-day session.
type NativeReceipt struct {
	SessionEntryID string `json:"sessionEntryId"`
	ProviderID     string `json:"providerId"`
	Name           string `json:"name"`
	Content        string `json:"content"`
	ExecutionID    string `json:"executionId,omitempty"`
	IsError        bool   `json:"isError"`
}
