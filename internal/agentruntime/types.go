// Package agentruntime defines the host-owned, framework-independent Agent protocol.
package agentruntime

import (
	"context"
	"encoding/json"
)

const ProtocolVersion = 1

type Model struct {
	Provider      string            `json:"provider"`
	API           string            `json:"api,omitempty"`
	ID            string            `json:"id"`
	BaseURL       string            `json:"baseUrl"`
	APIKey        string            `json:"apiKey"`
	Headers       map[string]string `json:"headers,omitempty"`
	ContextWindow int               `json:"contextWindow"`
	MaxTokens     int               `json:"maxTokens"`
	Thinking      string            `json:"thinking,omitempty"`
	ExtraFields   map[string]any    `json:"extraFields,omitempty"`
	Compat        map[string]any    `json:"compat,omitempty"`
	InputBudget   int               `json:"inputBudget,omitempty"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Request struct {
	GenerateOnly       bool            `json:"generateOnly,omitempty"`
	RunID              string          `json:"runId"`
	ConversationID     string          `json:"conversationId"`
	WorkingDir         string          `json:"workingDir,omitempty"`
	SystemPrompt       string          `json:"systemPrompt"`
	Message            string          `json:"message"`
	History            json.RawMessage `json:"history,omitempty"`
	Model              Model           `json:"model"`
	FallbackModels     []Model         `json:"fallbackModels,omitempty"`
	RetryMax           int             `json:"retryMax,omitempty"`
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
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ToolResult struct {
	Content     string `json:"content"`
	IsError     bool   `json:"isError,omitempty"`
	ExecutionID string `json:"executionId,omitempty"`
	Terminate   bool   `json:"terminate,omitempty"`
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
