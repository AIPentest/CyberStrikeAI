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

// RegisterKnowledgeTool 注册知识检索工具到MCP服务器
func RegisterKnowledgeTool(
	mcpServer *mcp.Server,
	retriever *Retriever,
	manager *Manager,
	logger *zap.Logger,
) {
	// 注册第一个工具：获取所有可用的风险类型列表
	listRiskTypesTool := mcp.Tool{
		Name:             builtin.ToolListKnowledgeRiskTypes,
		Description:      "Get the list of all available risk types (risk_type) in the knowledge base. Before searching the knowledge base, you can first call this tool to get the available risk types, then use the correct risk type for a precise search, which greatly reduces retrieval time and improves retrieval accuracy.",
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
			logger.Error("failed to get risk type list", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to get risk type list: %v", err),
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
						Text: "The knowledge base has no risk types yet.",
					},
				},
			}, nil
		}

		var resultText strings.Builder
		resultText.WriteString(fmt.Sprintf("The knowledge base has %d risk types:\n\n", len(categories)))
		for i, category := range categories {
			resultText.WriteString(fmt.Sprintf("%d. %s\n", i+1, category))
		}
		resultText.WriteString("\nTip: when calling the " + builtin.ToolSearchKnowledgeBase + " tool, you can use one of the above risk types as the risk_type parameter to narrow the search and improve retrieval efficiency.")

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
	logger.Debug("risk type list tool registered", zap.String("toolName", listRiskTypesTool.Name))

	// 注册第二个工具：搜索知识库（保持原有功能）
	searchTool := mcp.Tool{
		Name:             builtin.ToolSearchKnowledgeBase,
		Description:      "Search the knowledge base for relevant security knowledge. When you need to understand a specific vulnerability type, attack technique, detection method, or similar security knowledge, you can use this tool for retrieval. The tool is based on vector embedding and cosine similarity retrieval (semantically consistent with the Eino retriever). Recommendation: before searching, first call the " + builtin.ToolListKnowledgeRiskTypes + " tool to get the available risk types, then use the correct risk_type parameter for a precise search, which greatly reduces retrieval time.",
		ShortDescription: "Search the knowledge base for security knowledge (vector semantic retrieval)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query text, describing the security knowledge topic you want to learn about",
				},
				"risk_type": map[string]interface{}{
					"type":        "string",
					"description": "Optional: specify the risk type (e.g. SQL injection, XSS, file upload, etc.). Recommend calling the " + builtin.ToolListKnowledgeRiskTypes + " tool first to get the available risk types, then use the correct risk type for a precise search, which greatly reduces retrieval time. If not specified, all types are searched.",
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

		logger.Info("running knowledge base search",
			zap.String("query", query),
			zap.String("riskType", riskType),
		)

		// 检索统一走 Retriever.Search → VectorEinoRetriever（Eino retriever 语义）。
		searchReq := &SearchRequest{
			Query:    query,
			RiskType: riskType,
			TopK:     5,
		}

		results, err := retriever.Search(ctx, searchReq)
		if err != nil {
			logger.Error("knowledge base search failed", zap.Error(err))
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
						Text: fmt.Sprintf("No knowledge found related to the query '%s'. Suggestions:\n1. Try different keywords\n2. Check whether the risk type is correct\n3. Confirm whether the knowledge base contains relevant content", query),
					},
				},
			}, nil
		}

		// 格式化结果
		var resultText strings.Builder

		// 按余弦相似度（Score）降序
		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})

		// 按文档分组结果，以便更好地展示上下文
		type itemGroup struct {
			itemID   string
			results  []*RetrievalResult
			maxScore float64 // 该文档块的最高相似度
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

		// 按文档内最高相似度排序
		sort.Slice(itemGroups, func(i, j int) bool {
			return itemGroups[i].maxScore > itemGroups[j].maxScore
		})

		// 收集检索到的知识项ID（用于日志）
		retrievedItemIDs := make([]string, 0, len(itemGroups))

		resultText.WriteString(fmt.Sprintf("Found %d relevant knowledge chunks:\n\n", len(results)))

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

			// 按chunk_index排序，保证阅读的逻辑顺序（文档的原始顺序）
			sort.Slice(itemResults, func(i, j int) bool {
				return itemResults[i].Chunk.ChunkIndex < itemResults[j].Chunk.ChunkIndex
			})

			resultText.WriteString(fmt.Sprintf("--- Result %d (similarity: %.2f%%) ---\n",
				resultIndex, mainResult.Similarity*100))
			resultText.WriteString(fmt.Sprintf("Source: [%s] %s (ID: %s)\n", mainResult.Item.Category, mainResult.Item.Title, mainResult.Item.ID))

			// 按逻辑顺序显示所有chunk（包括主结果和扩展的chunk）
			if len(itemResults) == 1 {
				// 只有一个chunk，直接显示
				resultText.WriteString(fmt.Sprintf("Content chunk:\n%s\n", mainResult.Chunk.ChunkText))
			} else {
				// 多个chunk，按逻辑顺序显示
				resultText.WriteString("Content chunks (in document order):\n")
				for i, result := range itemResults {
					// 标记主结果
					marker := ""
					if result.Chunk.ID == mainResult.Chunk.ID {
						marker = " [main match]"
					}
					resultText.WriteString(fmt.Sprintf("  [Chunk %d%s]\n%s\n", i+1, marker, result.Chunk.ChunkText))
				}
			}
			resultText.WriteString("\n")

			if !contains(retrievedItemIDs, group.itemID) {
				retrievedItemIDs = append(retrievedItemIDs, group.itemID)
			}
			resultIndex++
		}

		// 在结果末尾添加元数据（JSON格式，用于提取知识项ID）
		// 使用特殊标记，避免影响AI阅读结果
		if len(retrievedItemIDs) > 0 {
			metadataJSON, _ := json.Marshal(map[string]interface{}{
				"_metadata": map[string]interface{}{
					"retrievedItemIDs": retrievedItemIDs,
				},
			})
			resultText.WriteString(fmt.Sprintf("\n<!-- METADATA: %s -->", string(metadataJSON)))
		}

		// 记录检索日志（异步，不阻塞）
		// 注意：这里没有conversationID和messageID，需要在Agent层面记录
		// 实际的日志记录应该在Agent的progressCallback中完成

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
	logger.Debug("knowledge search tool registered", zap.String("toolName", searchTool.Name))
}

// contains 检查切片是否包含元素
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetRetrievalMetadata 从工具调用中提取检索元数据（用于日志记录）
func GetRetrievalMetadata(args map[string]interface{}) (query string, riskType string) {
	if q, ok := args["query"].(string); ok {
		query = q
	}
	if rt, ok := args["risk_type"].(string); ok {
		riskType = rt
	}
	return
}

// FormatRetrievalResults 格式化检索结果为字符串（用于日志）
func FormatRetrievalResults(results []*RetrievalResult) string {
	if len(results) == 0 {
		return "No related results found"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Retrieved %d results:\n", len(results)))

	itemIDs := make(map[string]bool)
	for i, result := range results {
		builder.WriteString(fmt.Sprintf("%d. [%s] %s (similarity: %.2f%%)\n",
			i+1, result.Item.Category, result.Item.Title, result.Similarity*100))
		itemIDs[result.Item.ID] = true
	}

	// 返回知识项ID列表（JSON格式）
	ids := make([]string, 0, len(itemIDs))
	for id := range itemIDs {
		ids = append(ids, id)
	}
	idsJSON, _ := json.Marshal(ids)
	builder.WriteString(fmt.Sprintf("\nRetrieved knowledge item IDs: %s", string(idsJSON)))

	return builder.String()
}
