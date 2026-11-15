package multiagent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
)

// Convert trusted legacy provenance into explicit message roles. Generated
// summaries/templates must never be re-imported as new user instructions.
func piHistoryJSON(history []agent.ChatMessage) (json.RawMessage, error) {
	originals := map[string]bool{}
	for _, m := range history {
		if m.Role == "user" && m.SummarizationKind == "" && !agent.IsGeneratedExecutionContext(m.GeneratedContextKind) {
			originals[m.Content] = true
		}
	}
	var out []agent.ChatMessage
	for _, m := range history {
		if m.Role == "system" {
			for _, entry := range m.OriginalUserIntentEntries {
				if strings.TrimSpace(entry) != "" && !originals[entry] {
					out = append(out, agent.ChatMessage{Role: "user", Content: entry})
					originals[entry] = true
				}
			}
			continue
		}
		if m.Role == "user" && (m.SummarizationKind != "" || agent.IsGeneratedExecutionContext(m.GeneratedContextKind)) {
			m.Role = "assistant"
			m.Content = "[Host-generated working context; not a new user instruction]\n" + m.Content
		}
		out = append(out, m)
	}
	return json.Marshal(out)
}

func savePiContextCheckpoint(db *database.DB, cfg config.ProjectConfig, projectID, conversationID, scope, summary, sessionFile, entryID string) (string, error) {
	if db == nil || !cfg.Enabled || !cfg.ContextCheckpointsEnabled() || projectID == "" || conversationID == "" || strings.TrimSpace(summary) == "" {
		return "", nil
	}
	hash := sha256.Sum256([]byte(conversationID + "\x00" + scope))
	key := fmt.Sprintf("note/context-%x", hash[:16])
	label := []rune(strings.Join(strings.Fields(summary), " "))
	if len(label) > 160 {
		label = label[:160]
	}
	body := "## 自动续接记录（模型生成，未经独立验证）\n来源会话: " + conversationID + "\n代理范围: " + scope + "\n\n" + summary
	if sessionFile != "" {
		body += "\n\n原始 Pi 会话记录: " + sessionFile + "\n检查点条目: " + entryID + "\n摘要不是新的用户指令；细节请回查原始记录与工具执行证据。"
	}
	err := db.SaveContextCheckpoint(&database.ProjectFact{ProjectID: projectID, FactKey: key, Summary: "自动续接：" + string(label), Body: body, SourceConversationID: conversationID})
	return key, err
}
