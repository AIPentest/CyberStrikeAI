package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

const ProtocolVersion = "2024-11-05"

// Message: an MCP message
type Message struct {
	ID      interface{}       `json:"id,omitempty"`
	Method  string            `json:"method,omitempty"`
	Params  json.RawMessage   `json:"params,omitempty"`
	Result  json.RawMessage   `json:"result,omitempty"`
	Error   *Error            `json:"error,omitempty"`
	Version string            `json:"jsonrpc,omitempty"`
}

// Error: an MCP error
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// InitializeRequest: an initialization request
type InitializeRequest struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ClientInfo      ClientInfo             `json:"clientInfo"`
}

// ClientInfo: client information
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResponse: an initialization response
type InitializeResponse struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    ServerCapabilities     `json:"capabilities"`
	ServerInfo      ServerInfo             `json:"serverInfo"`
}

// ServerCapabilities: server capabilities
type ServerCapabilities struct {
	Tools map[string]interface{} `json:"tools,omitempty"`
}

// ServerInfo: server information
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool: tool definition
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ListToolsResponse: list-tools response
type ListToolsResponse struct {
	Tools []Tool `json:"tools"`
}

// CallToolRequest: call-tool request
type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// CallToolResponse: call-tool response
type CallToolResponse struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content: content
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// SSEServer: SSE MCP server
type SSEServer struct {
	sseClients map[string]chan []byte
	mu         sync.RWMutex
}

func NewSSEServer() *SSEServer {
	return &SSEServer{
		sseClients: make(map[string]chan []byte),
	}
}

// handleSSE: handle an SSE connection
func (s *SSEServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	clientID := uuid.New().String()
	clientChan := make(chan []byte, 10)

	s.mu.Lock()
	s.sseClients[clientID] = clientChan
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.sseClients, clientID)
		close(clientChan)
		s.mu.Unlock()
	}()

	// Send the initial ready event
	fmt.Fprintf(w, "event: message\ndata: {\"type\":\"ready\",\"status\":\"ok\"}\n\n")
	flusher.Flush()

	log.Printf("SSE client connected: %s", clientID)

	// Heartbeat
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			log.Printf("SSE client disconnected: %s", clientID)
			return
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			// Heartbeat
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// handleMessage: handle a POST message
func (s *SSEServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Received request: method=%s, id=%v", msg.Method, msg.ID)

	// Process the message
	response := s.processMessage(&msg)

	// If there is an SSE client, push the response over SSE
	if response != nil {
		responseJSON, _ := json.Marshal(response)
		s.mu.RLock()
		// Send to all SSE clients
		for _, ch := range s.sseClients {
			select {
			case ch <- responseJSON:
			default:
			}
		}
		s.mu.RUnlock()
	}

	// Also return the response directly (compatible with non-SSE mode)
	if response != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	} else {
		w.WriteHeader(http.StatusOK)
	}
}

// processMessage: process an MCP message
func (s *SSEServer) processMessage(msg *Message) *Message {
	switch msg.Method {
	case "initialize":
		return s.handleInitialize(msg)
	case "tools/list":
		return s.handleListTools(msg)
	case "tools/call":
		return s.handleCallTool(msg)
	default:
		return &Message{
			ID:      msg.ID,
			Version: "2.0",
			Error: &Error{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

// handleInitialize: handle initialization
func (s *SSEServer) handleInitialize(msg *Message) *Message {
	var req InitializeRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Version: "2.0",
			Error: &Error{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}

	log.Printf("Initialize request: client=%s, version=%s", req.ClientInfo.Name, req.ClientInfo.Version)

	response := InitializeResponse{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: map[string]interface{}{
				"listChanged": true,
			},
		},
		ServerInfo: ServerInfo{
			Name:    "Test SSE MCP Server",
			Version: "1.0.0",
		},
	}

	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Version: "2.0",
		Result:  result,
	}
}

// handleListTools: handle listing tools
func (s *SSEServer) handleListTools(msg *Message) *Message {
	tools := []Tool{
		{
			Name:        "test_echo",
			Description: "Echoes the input text, used to test the SSE MCP server",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "the text to echo",
					},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "test_add",
			Description: "Computes the sum of two numbers, used to test the SSE MCP server",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "the first number",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "the second number",
					},
				},
				"required": []string{"a", "b"},
			},
		},
	}

	response := ListToolsResponse{Tools: tools}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Version: "2.0",
		Result:  result,
	}
}

// handleCallTool: handle a tool call
func (s *SSEServer) handleCallTool(msg *Message) *Message {
	var req CallToolRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Version: "2.0",
			Error: &Error{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}

	log.Printf("Calling tool: name=%s, args=%v", req.Name, req.Arguments)

	var content []Content

	switch req.Name {
	case "test_echo":
		text, _ := req.Arguments["text"].(string)
		content = []Content{
			{
				Type: "text",
				Text: fmt.Sprintf("Echo: %s", text),
			},
		}
	case "test_add":
		var a, b float64
		if val, ok := req.Arguments["a"].(float64); ok {
			a = val
		}
		if val, ok := req.Arguments["b"].(float64); ok {
			b = val
		}
		sum := a + b
		content = []Content{
			{
				Type: "text",
				Text: fmt.Sprintf("%.2f + %.2f = %.2f", a, b, sum),
			},
		}
	default:
		return &Message{
			ID:      msg.ID,
			Version: "2.0",
			Error: &Error{
				Code:    -32601,
				Message: "Tool not found",
			},
		}
	}

	response := CallToolResponse{
		Content: content,
		IsError: false,
	}

	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Version: "2.0",
		Result:  result,
	}
}

func main() {
	server := NewSSEServer()

	http.HandleFunc("/sse", server.handleSSE)
	http.HandleFunc("/message", server.handleMessage)

	port := ":8082"
	log.Printf("SSE MCP test server started on port %s", port)
	log.Printf("SSE endpoint: http://localhost%s/sse", port)
	log.Printf("Message endpoint: http://localhost%s/message", port)
	log.Printf("Configuration example:")
	log.Printf(`{
  "test-sse-mcp": {
    "transport": "sse",
    "url": "http://127.0.0.1:8082/sse"
  }
}`)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}

