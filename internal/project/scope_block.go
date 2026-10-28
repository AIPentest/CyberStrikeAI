package project

import (
	"encoding/json"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
)

// projectScopePayload 解析 projects.scope_json（约定字段，可扩展）。
type projectScopePayload struct {
	Targets []string `json:"targets"`
	Exclude []string `json:"exclude"`
	Notes   string   `json:"notes"`
}

// BuildScopeBlock 将项目 scope_json 格式化为 Agent 可读的授权范围块。
func BuildScopeBlock(proj *database.Project) string {
	if proj == nil {
		return ""
	}
	raw := strings.TrimSpace(proj.ScopeJSON)
	if raw == "" {
		return ""
	}

	var payload projectScopePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return fmt.Sprintf("## Project testing scope (project: %s)\n(scope_json is not valid JSON, please review the configuration manually)\n```\n%s\n```\n"+
			"Only test explicitly authorized targets; if you exceed the scope you must stop and explain.\n", proj.Name, truncateRunes(raw, 800))
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Project testing scope (project: %s, id: %s)\n", proj.Name, proj.ID))
	b.WriteString("The following are the authorization boundaries, **you must obey them**: only test the listed targets, avoid exclude, and do not enlarge the scope on your own.\n")

	if len(payload.Targets) > 0 {
		b.WriteString("\n**Allowed to test (targets)**:\n")
		for _, t := range payload.Targets {
			t = strings.TrimSpace(t)
			if t != "" {
				b.WriteString("- " + t + "\n")
			}
		}
	}
	if len(payload.Exclude) > 0 {
		b.WriteString("\n**Explicitly excluded (exclude)**:\n")
		for _, t := range payload.Exclude {
			t = strings.TrimSpace(t)
			if t != "" {
				b.WriteString("- " + t + "\n")
			}
		}
	}
	if n := strings.TrimSpace(payload.Notes); n != "" {
		b.WriteString("\n**Notes (notes)**:\n" + n + "\n")
	}
	if len(payload.Targets) == 0 && len(payload.Exclude) == 0 && strings.TrimSpace(payload.Notes) == "" {
		b.WriteString("\n(scope_json is configured but no targets/exclude/notes fields were recognized; the raw content is for reference)\n```json\n")
		b.WriteString(truncateRunes(raw, 1200))
		b.WriteString("\n```\n")
	}
	b.WriteString("\nIf a target is not within targets or hits exclude, you must not actively scan/exploit; continue only after the user explicitly enlarges the authorization.\n")
	return b.String()
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// BuildProjectBlackboardBlock 组合测试范围 + 事实黑板索引。
func BuildProjectBlackboardBlock(db *database.DB, projectID string, cfg config.ProjectConfig, conversationIDs ...string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", nil
	}
	proj, err := db.GetProject(projectID)
	if err != nil {
		return "", err
	}
	parts := []string{}
	if scope := strings.TrimSpace(BuildScopeBlock(proj)); scope != "" {
		parts = append(parts, scope)
	}
	index, err := BuildFactIndexBlock(db, projectID, cfg, conversationIDs...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(index) != "" {
		parts = append(parts, index)
	}
	return strings.Join(parts, "\n\n"), nil
}
