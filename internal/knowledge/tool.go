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
		Description:      "获取知识库中所有可用的风险类型（risk_type）列表。在搜索知识库之前，可以先调用此工具获取可用的风险类型，然后使用正确的风险类型进行精确搜索，这样可以大幅减少检索时间并提高检索准确性。",
		ShortDescription: "获取知识库中所有可用的风险类型列表",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	}

	listRiskTypesHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		categories, err := manager.GetCategories()
		if err != nil {
			logger.Error("获取风险类型列表失败", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("获取风险类型列表失败: %v", err),
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
						Text: "知识库中暂无风险类型。",
					},
				},
			}, nil
		}

		var resultText strings.Builder
		resultText.WriteString(fmt.Sprintf("知识库中共有 %d 个风险类型：\n\n", len(categories)))
		for i, category := range categories {
			resultText.WriteString(fmt.Sprintf("%d. %s\n", i+1, category))
		}
		resultText.WriteString("\n提示：在调用 " + builtin.ToolSearchKnowledgeBase + " 工具时，可以使用上述风险类型之一作为 risk_type 参数，以缩小搜索范围并提高检索效率。")

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
	logger.Debug("风险类型列表工具已注册", zap.String("toolName", listRiskTypesTool.Name))

	// 注册第二个工具：搜索知识库（保持原有功能）
	searchTool := mcp.Tool{
		Name:             builtin.ToolSearchKnowledgeBase,
		Description:      "在知识库中搜索相关的安全知识。当你需要了解特定漏洞类型、攻击技术、检测方法等安全知识时，可以使用此工具进行检索。工具基于向量嵌入与余弦相似度检索（与 Agent retriever 语义一致）。建议：在搜索前可以先调用 " + builtin.ToolListKnowledgeRiskTypes + " 工具获取可用的风险类型，然后使用正确的 risk_type 参数进行精确搜索，这样可以大幅减少检索时间。",
		ShortDescription: "搜索知识库中的安全知识（向量语义检索）",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "搜索查询内容，描述你想要了解的安全知识主题",
				},
				"risk_type": map[string]interface{}{
					"type":        "string",
					"description": "可选：指定风险类型（如：SQL注入、XSS、文件上传等）。建议先调用 " + builtin.ToolListKnowledgeRiskTypes + " 工具获取可用的风险类型列表，然后使用正确的风险类型进行精确搜索，这样可以大幅减少检索时间。如果不指定则搜索所有类型。",
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
						Text: "错误: 查询参数不能为空",
					},
				},
				IsError: true,
			}, nil
		}

		riskType := ""
		if rt, ok := args["risk_type"].(string); ok && rt != "" {
			riskType = rt
		}

		logger.Info("执行知识库检索",
			zap.String("query", query),
			zap.String("riskType", riskType),
		)

		// 检索统一走 Retriever.Search → VectorRetriever（Agent retriever 语义）。
		searchReq := &SearchRequest{
			Query:    query,
			RiskType: riskType,
			TopK:     5,
		}

		results, err := retriever.Search(ctx, searchReq)
		if err != nil {
			logger.Error("知识库检索失败", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("检索失败: %v", err),
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
						Text: fmt.Sprintf("未找到与查询 '%s' 相关的知识。建议：\n1. 尝试使用不同的关键词\n2. 检查风险类型是否正确\n3. 确认知识库中是否包含相关内容", query),
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

		resultText.WriteString(fmt.Sprintf("找到 %d 条相关知识片段：\n\n", len(results)))

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

			resultText.WriteString(fmt.Sprintf("--- 结果 %d (相似度: %.2f%%) ---\n",
				resultIndex, mainResult.Similarity*100))
			resultText.WriteString(fmt.Sprintf("来源: [%s] %s (ID: %s)\n", mainResult.Item.Category, mainResult.Item.Title, mainResult.Item.ID))

			// 按逻辑顺序显示所有chunk（包括主结果和扩展的chunk）
			if len(itemResults) == 1 {
				// 只有一个chunk，直接显示
				resultText.WriteString(fmt.Sprintf("内容片段:\n%s\n", mainResult.Chunk.ChunkText))
			} else {
				// 多个chunk，按逻辑顺序显示
				resultText.WriteString("内容片段（按文档顺序）:\n")
				for i, result := range itemResults {
					// 标记主结果
					marker := ""
					if result.Chunk.ID == mainResult.Chunk.ID {
						marker = " [主匹配]"
					}
					resultText.WriteString(fmt.Sprintf("  [片段 %d%s]\n%s\n", i+1, marker, result.Chunk.ChunkText))
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
	logger.Debug("知识检索工具已注册", zap.String("toolName", searchTool.Name))
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
		return "未找到相关结果"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("检索到 %d 条结果:\n", len(results)))

	itemIDs := make(map[string]bool)
	for i, result := range results {
		builder.WriteString(fmt.Sprintf("%d. [%s] %s (相似度: %.2f%%)\n",
			i+1, result.Item.Category, result.Item.Title, result.Similarity*100))
		itemIDs[result.Item.ID] = true
	}

	// 返回知识项ID列表（JSON格式）
	ids := make([]string, 0, len(itemIDs))
	for id := range itemIDs {
		ids = append(ids, id)
	}
	idsJSON, _ := json.Marshal(ids)
	builder.WriteString(fmt.Sprintf("\n检索到的知识项ID: %s", string(idsJSON)))

	return builder.String()
}
