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
		return fmt.Sprintf("## 项目测试范围（project: %s）\n（scope_json 非合法 JSON，请人工核对配置）\n```\n%s\n```\n"+
			"仅对明确授权目标执行测试；超出范围须停止并说明。\n", proj.Name, truncateRunes(raw, 800))
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## 项目测试范围（project: %s, id: %s）\n", proj.Name, proj.ID))
	b.WriteString("以下为授权边界，**必须遵守**：仅测试列出的 targets，避开 exclude，不得擅自扩大范围。\n")

	if len(payload.Targets) > 0 {
		b.WriteString("\n**允许测试（targets）**：\n")
		for _, t := range payload.Targets {
			t = strings.TrimSpace(t)
			if t != "" {
				b.WriteString("- " + t + "\n")
			}
		}
	}
	if len(payload.Exclude) > 0 {
		b.WriteString("\n**明确排除（exclude）**：\n")
		for _, t := range payload.Exclude {
			t = strings.TrimSpace(t)
			if t != "" {
				b.WriteString("- " + t + "\n")
			}
		}
	}
	if n := strings.TrimSpace(payload.Notes); n != "" {
		b.WriteString("\n**说明（notes）**：\n" + n + "\n")
	}
	if len(payload.Targets) == 0 && len(payload.Exclude) == 0 && strings.TrimSpace(payload.Notes) == "" {
		b.WriteString("\n（scope_json 已配置但未识别 targets/exclude/notes 字段，原始内容供参考）\n```json\n")
		b.WriteString(truncateRunes(raw, 1200))
		b.WriteString("\n```\n")
	}
	b.WriteString("\n若目标不在 targets 内或命中 exclude，不得主动扫描/利用；需用户明确扩大授权后再继续。\n")
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
