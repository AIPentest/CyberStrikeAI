package project

import (
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
)

// AppendSystemPromptBlock 将附加块追加到 system prompt。
func AppendSystemPromptBlock(base, block string) string {
	base = strings.TrimSpace(base)
	block = strings.TrimSpace(block)
	if block == "" {
		return base
	}
	if base == "" {
		return block
	}
	return base + "\n\n" + block
}

const (
	factIndexFooterGetDetail = "When full content is needed (attack chain, POC, request/response etc.) you must call get_project_fact(fact_key); do not fabricate details from the summary."
	factIndexFooterWriteHint = "When writing fact links use from (source fact_key → current fact), e.g. on a finding {from:target/*, type:discovered_on}; the body records the reproducible end-to-end flow (for discovery/exploit-type fact_keys prefer the finding|chain|exploit|poc/ prefixes)."
	factIndexFooterEmpty     = "To write, use upsert_project_fact; for detail, call get_project_fact(fact_key)."
)

// BuildFactIndexBlock 为 Agent 系统提示生成项目黑板索引（key + summary + 关系边 + 攻击路径，不含 body）。
func BuildFactIndexBlock(db *database.DB, projectID string, cfg config.ProjectConfig, conversationIDs ...string) (string, error) {
	if db == nil || !cfg.Enabled {
		return "", nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", nil
	}

	proj, err := db.GetProject(projectID)
	if err != nil {
		return "", err
	}

	conversationID := ""
	if len(conversationIDs) > 0 {
		conversationID = strings.TrimSpace(conversationIDs[0])
	}
	facts, err := db.ListProjectFactsForIndex(projectID, cfg.DefaultInjectDeprecated, conversationID)
	if err != nil {
		return "", err
	}
	allEdges, _ := db.ListProjectFactEdgesByProject(projectID)
	allEdges = FilterCheckpointEdges(allEdges, facts)
	_, incomingByTarget := indexEdgeGroupMaps(allEdges)
	resume := buildResumeSection(db, projectID, facts)

	maxRunes := cfg.FactIndexMaxRunesEffective()
	header := fmt.Sprintf("## Project blackboard index (project: %s, id: %s)\n", indexField(proj.Name), proj.ID)
	if len(facts) == 0 {
		content := header + "(no facts yet)\n" + factIndexFooterEmpty
		if len([]rune(wrapFactIndexBlock(content+"\n\n"+resume))) <= maxRunes {
			return wrapFactIndexBlock(content + "\n\n" + resume), nil
		}
		block := wrapFactIndexBlock(content)
		if len([]rune(block)) <= maxRunes {
			return block, nil
		}
		return minimalFactIndexBlock(maxRunes), nil
	}

	sortFactsForIndex(facts)

	pathMaxRunes := cfg.FactIndexPathMaxRunesEffective()
	footer := factIndexFooterGetDetail + "\n" + factIndexFooterWriteHint
	// Continuation data shares the host index cap. Reserve space for fact rows;
	// tiny budgets retain tool retrieval guidance.
	if len([]rune(resume)) <= maxRunes-len([]rune(wrapFactIndexBlock(header+footer)))-800 {
		footer += "\n\n" + resume
	}
	// Include markers, separators and the worst-case omission notice in the cap.
	omissionReserve := len([]rune(factIndexOmissionNotice(len(facts))))
	framingRunes := len([]rune(wrapFactIndexBlock(header+footer))) + omissionReserve
	available := maxRunes - framingRunes
	if available <= 0 {
		return minimalFactIndexBlock(maxRunes), nil
	}
	if pathMaxRunes > available {
		pathMaxRunes = available
	}
	factsBudget := available - pathMaxRunes
	if factsBudget < 800 {
		factsBudget = available
		pathMaxRunes = 0
	}

	indexedKeys := make(map[string]struct{}, len(facts))
	var b strings.Builder
	b.WriteString(header)
	used := 0
	omitted := 0

	for _, f := range facts {
		// Category and confidence are only defaulted, never validated, so either can
		// carry a newline that would inject a fake index line into the system prompt.
		line := fmt.Sprintf("- [%s] %s — %s (%s)", f.FactKey,
			indexField(f.Category),
			truncateIndexSummary(indexField(f.Summary), cfg.FactSummaryMaxRunesEffective()),
			indexField(f.Confidence))
		if database.IsContextCheckpointKey(f.FactKey) {
			line += " [current-session working record]"
		}
		line += FormatFactIndexLinksHint(f.FactKey, incomingByTarget[f.FactKey])
		line += "\n"
		lineRunes := len([]rune(line))
		if used+lineRunes > factsBudget {
			omitted++
			continue
		}
		indexedKeys[f.FactKey] = struct{}{}
		b.WriteString(line)
		used += lineRunes
	}

	if omitted > 0 {
		b.WriteString(factIndexOmissionNotice(omitted))
	}

	if pathSection := BuildFactPathOverviewSection(allEdges, indexedKeys, pathMaxRunes-1); pathSection != "" {
		b.WriteString("\n")
		b.WriteString(pathSection)
	}

	b.WriteString(footer)
	return wrapFactIndexBlock(b.String()), nil
}

// A hidden checkpoint must not reappear via a visible fact's relationship hint.
func FilterCheckpointEdges(edges []*database.ProjectFactEdge, visibleFacts []*database.ProjectFact) []*database.ProjectFactEdge {
	visible := make(map[string]bool)
	for _, f := range visibleFacts {
		visible[f.FactKey] = true
	}
	var out []*database.ProjectFactEdge
	for _, e := range edges {
		if database.IsContextCheckpointKey(e.SourceFactKey) && !visible[e.SourceFactKey] ||
			database.IsContextCheckpointKey(e.TargetFactKey) && !visible[e.TargetFactKey] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// indexField 压平自由文本事实字段：换行能伪造索引行，宿主定界符能提前闭合索引块。
func indexField(s string) string {
	return factIndexSafeText(strings.Join(strings.Fields(s), " "))
}

func truncateIndexSummary(s string, maxRunes int) string {
	runes := []rune(s)
	if maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes-1]) + "…"
	}
	return s
}

func factIndexOmissionNotice(count int) string {
	return fmt.Sprintf("\n(There are %d more facts not listed in the index; query them with list_project_facts or search_project_facts.)\n", count)
}

func minimalFactIndexBlock(maxRunes int) string {
	block := wrapFactIndexBlock("## Project blackboard index\nIndex budget too small; query list_project_facts or search_project_facts as needed.")
	if len([]rune(block)) > maxRunes {
		return ""
	}
	return block
}
