package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

// RegisterKnowledgeTool Register the knowledge-retrieval tool with the MCP server
func RegisterKnowledgeTool(
	mcpServer *mcp.Server,
	retriever *Retriever,
	manager *Manager,
	logger *zap.Logger,
) {
	// Register the first tool: get the list of all available risk types
	listRiskTypesTool := mcp.Tool{
		Name:             builtin.ToolListKnowledgeRiskTypes,
		Description:      "Get the list of all available risk types (risk_type) in the knowledge base. Before searching the knowledge base, you can call this tool first to get the available risk types, then use the correct risk type for precise search, which greatly reduces retrieval time and improves retrieval accuracy.",
		ShortDescription: "Get the list of all available risk types in the knowledge base",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	}

	listRiskTypesHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		categories, err := manager.GetCategories()
		if err != nil {
			logger.Error("Failed to get the risk-type list", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to get the risk-type list: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		if len(categories) == 0 {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "There are no risk types in the knowledge base yet.",
					},
				},
			}, nil
		}

		var resultText strings.Builder
		resultText.WriteString(fmt.Sprintf("There are %d risk types in the knowledge base: \n\n", len(categories)))
		for i, category := range categories {
			resultText.WriteString(fmt.Sprintf("%d. %s\n", i+1, category))
		}
		resultText.WriteString("\n Hint: when calling " + builtin.ToolSearchKnowledgeBase + " tool, you can use one of the above risk types as the risk_type parameter to narrow the search scope and improve retrieval efficiency.")

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: resultText.String(),
				},
			},
		}, nil
	}

	mcpServer.RegisterTool(listRiskTypesTool, listRiskTypesHandler)
	logger.Debug("The risk-type list tool is registered", zap.String("toolName", listRiskTypesTool.Name))

	// Register the second tool: search the knowledge base (keeping the original functionality)
	searchTool := mcp.Tool{
		Name:             builtin.ToolSearchKnowledgeBase,
		Description:      "Search the knowledge base for relevant security knowledge. When you need to understand a specific vulnerability type, attack technique, detection method, or other security knowledge, use this tool to retrieve it. The tool is based on vector embeddings and cosine-similarity retrieval (consistent with Eino retriever semantics). Suggestion: before searching you can first call " + builtin.ToolListKnowledgeRiskTypes + " tool to get the available risk types, then use the correct risk_type parameter for precise search, which greatly reduces retrieval time.",
		ShortDescription: "Search security knowledge in the knowledge base (vector semantic retrieval)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query content, describing the security-knowledge topic you want to learn about",
				},
				"risk_type": map[string]interface{}{
					"type":        "string",
					"description": "Optional: specify a risk type (such as: SQL injection, XSS, file upload, etc.). It is recommended to call first " + builtin.ToolListKnowledgeRiskTypes + " tool to get the list of available risk types, then use the correct risk type for precise search, which greatly reduces retrieval time. If not specified, all types are searched.",
				},
			},
			"required": []string{"query"},
		},
	}

	searchHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		query, ok := args["query"].(string)
		if !ok || query == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: the query parameter cannot be empty",
					},
				},
				IsError: true,
			}, nil
		}

		riskType := ""
		if rt, ok := args["risk_type"].(string); ok && rt != "" {
			riskType = rt
		}

		logger.Info("Perform knowledge-base retrieval",
			zap.String("query", query),
			zap.String("riskType", riskType),
		)

		// Retrieval goes uniformly through Retriever.Search → VectorEinoRetriever (Eino retriever Semantics).
		searchReq := &SearchRequest{
			Query:    query,
			RiskType: riskType,
			TopK:     5,
		}

		results, err := retriever.Search(ctx, searchReq)
		if err != nil {
			logger.Error("Knowledge-base retrieval failed", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Retrieval failed: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		if len(results) == 0 {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("No match found for the query '%s' related knowledge. Suggestion: \n1. Try different keywords\n2. Check whether the risk type is correct\n3. Confirm whether the knowledge base contains relevant content", query),
					},
				},
			}, nil
		}

		// Format the result
		var resultText strings.Builder

		// Descending by cosine similarity (Score)
		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})

		// Group results by document to present context better
		type itemGroup struct {
			itemID   string
			results  []*RetrievalResult
			maxScore float64 // The highest similarity of this document block
		}
		itemGroups := make([]*itemGroup, 0)
		itemMap := make(map[string]*itemGroup)

		for _, result := range results {
			itemID := result.Item.ID
			group, exists := itemMap[itemID]
			if !exists {
				group = &itemGroup{
					itemID:   itemID,
					results:  make([]*RetrievalResult, 0),
					maxScore: result.Score,
				}
				itemMap[itemID] = group
				itemGroups = append(itemGroups, group)
			}
			group.results = append(group.results, result)
			if result.Score > group.maxScore {
				group.maxScore = result.Score
			}
		}

		// Sort by the highest similarity within a document
		sort.Slice(itemGroups, func(i, j int) bool {
			return itemGroups[i].maxScore > itemGroups[j].maxScore
		})

		// Collect the retrieved knowledge-item IDs (for logging)
		retrievedItemIDs := make([]string, 0, len(itemGroups))

		resultText.WriteString(fmt.Sprintf("Found %d relevant knowledge snippets: \n\n", len(results)))

		resultIndex := 1
		for _, group := range itemGroups {
			itemResults := group.results
			mainResult := itemResults[0]
			maxScore := mainResult.Score
			for _, result := range itemResults {
				if result.Score > maxScore {
					maxScore = result.Score
					mainResult = result
				}
			}

			// Sort by chunk_index to ensure the logical reading order (the document's original order)
			sort.Slice(itemResults, func(i, j int) bool {
				return itemResults[i].Chunk.ChunkIndex < itemResults[j].Chunk.ChunkIndex
			})

			resultText.WriteString(fmt.Sprintf("--- Result %d (similarity: %.2f%%) ---\n",
				resultIndex, mainResult.Similarity*100))
			resultText.WriteString(fmt.Sprintf("Source: [%s] %s (ID: %s)\n", mainResult.Item.Category, mainResult.Item.Title, mainResult.Item.ID))

			// Display all chunks in logical order (including the main result and expanded oneschunk)
			if len(itemResults) == 1 {
				// Only one chunk, display it directly
				resultText.WriteString(fmt.Sprintf("Content snippet:\n%s\n", mainResult.Chunk.ChunkText))
			} else {
				// Multiple chunks, displayed in logical order
				resultText.WriteString("Content snippet (in document order):\n")
				for i, result := range itemResults {
					// Mark the main result
					marker := ""
					if result.Chunk.ID == mainResult.Chunk.ID {
						marker = " [Primary match]"
					}
					resultText.WriteString(fmt.Sprintf("  [Snippet %d%s]\n%s\n", i+1, marker, result.Chunk.ChunkText))
				}
			}
			resultText.WriteString("\n")

			if !contains(retrievedItemIDs, group.itemID) {
				retrievedItemIDs = append(retrievedItemIDs, group.itemID)
			}
			resultIndex++
		}

		// Append metadata at the end of the result (JSON format, used to extract knowledge itemsID)
		// Use a special marker to avoid affecting how the AI reads the result
		if len(retrievedItemIDs) > 0 {
			metadataJSON, _ := json.Marshal(map[string]interface{}{
				"_metadata": map[string]interface{}{
					"retrievedItemIDs": retrievedItemIDs,
				},
			})
			resultText.WriteString(fmt.Sprintf("\n<!-- METADATA: %s -->", string(metadataJSON)))
		}

		// Record the retrieval log (asynchronous, non-blocking)
		// Note: there is no conversationID or messageID here; it must be logged at the Agent level
		// Actual logging should be done in the Agent's progressCallback

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: resultText.String(),
				},
			},
		}, nil
	}

	mcpServer.RegisterTool(searchTool, searchHandler)
	logger.Debug("The knowledge-retrieval tool is registered", zap.String("toolName", searchTool.Name))
}

// contains Check whether the slice contains an element
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetRetrievalMetadata Extract retrieval metadata from the tool call (used for logging)
func GetRetrievalMetadata(args map[string]interface{}) (query string, riskType string) {
	if q, ok := args["query"].(string); ok {
		query = q
	}
	if rt, ok := args["risk_type"].(string); ok {
		riskType = rt
	}
	return
}

// FormatRetrievalResults Format the retrieval result as a string (for logging)
func FormatRetrievalResults(results []*RetrievalResult) string {
	if len(results) == 0 {
		return "No relevant results found"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Retrieved %d results:\n", len(results)))

	itemIDs := make(map[string]bool)
	for i, result := range results {
		builder.WriteString(fmt.Sprintf("%d. [%s] %s (Similarity: %.2f%%)\n",
			i+1, result.Item.Category, result.Item.Title, result.Similarity*100))
		itemIDs[result.Item.ID] = true
	}

	// Return the knowledge-item ID list (JSON format)
	ids := make([]string, 0, len(itemIDs))
	for id := range itemIDs {
		ids = append(ids, id)
	}
	idsJSON, _ := json.Marshal(ids)
	builder.WriteString(fmt.Sprintf("\n The retrieved knowledge itemID: %s", string(idsJSON)))

	return builder.String()
}
