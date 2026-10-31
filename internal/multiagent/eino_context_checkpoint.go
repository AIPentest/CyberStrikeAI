package multiagent

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

var transcriptSnapshotWriteMu sync.Mutex

const transcriptSnapshotTimeLayout = "20060102-150405.000000000-0700"

// Time-stamped snapshots keep earlier evidence reachable after the mutable
// alias advances. Short MD5 prefixes are labels, never evidence of equality.
func writeSummarizationTranscriptSnapshot(path string, msgs []adk.Message) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	body := []byte(formatSummarizationTranscript(msgs))
	transcriptSnapshotWriteMu.Lock()
	defer transcriptSnapshotWriteMu.Unlock()
	dir := filepath.Join(filepath.Dir(path), "snapshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	snapshot, err := findOrCreateTranscriptSnapshot(dir, body, time.Now())
	if err != nil {
		return "", err
	}
	if err := writeContextArtifactAtomically(path, body); err != nil {
		// The immutable evidence already exists; an expendable alias failure
		// must not hide its successful retrieval path.
		return snapshot, err
	}
	return snapshot, nil
}

func findOrCreateTranscriptSnapshot(dir string, body []byte, timestamp time.Time) (string, error) {
	prefix := fmt.Sprintf("%x", md5.Sum(body))[:5]
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "transcript-"+prefix+"-") && strings.HasSuffix(entry.Name(), ".txt") {
			candidates = append(candidates, filepath.Join(dir, entry.Name()))
		}
	}
	// Existing SHA-256 names remain valid after upgrading; never rename evidence
	// referenced by older summaries or create a duplicate on an identical retry.
	legacy := filepath.Join(dir, fmt.Sprintf("transcript-%x.txt", sha256.Sum256(body)))
	if _, err := os.Stat(legacy); err == nil {
		candidates = append(candidates, legacy)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	for _, candidate := range candidates {
		info, err := os.Lstat(candidate)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("snapshot is not a regular file: %s", candidate)
		}
		stored, err := os.ReadFile(candidate)
		if err != nil {
			return "", err
		}
		if bytes.Equal(stored, body) {
			return candidate, nil
		}
	}
	for {
		name := "transcript-" + prefix + "-" + timestamp.Format(transcriptSnapshotTimeLayout) + ".txt"
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			if err := writeContextArtifactAtomically(path, body); err != nil {
				return "", err
			}
			return path, nil
		} else if err != nil {
			return "", err
		}
		// A colliding timestamp must not replace an existing file, even if the
		// short digest prefix happens to match different content.
		timestamp = timestamp.Add(time.Nanosecond)
	}
}

func writeContextArtifactAtomically(path string, body []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".context-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err = file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// Persist generated working memory separately from verified project findings.
// One key per conversation/agent scope is updated; retries do not grow the fact index.
func persistSummarizationCheckpoint(db *database.DB, projectID, conversationID, memoryScope, summary, snapshot string, cfg config.ProjectConfig) (string, error) {
	if db == nil || !cfg.Enabled || !cfg.ContextCheckpointsEnabled() || strings.TrimSpace(projectID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(summary) == "" {
		return "", nil
	}
	// Bind storage to the current conversation rather than trusting a stale run's project ID.
	bound, err := db.GetConversationProjectID(conversationID)
	if err != nil {
		return "", err
	}
	if bound != projectID {
		return "", fmt.Errorf("checkpoint project does not match conversation")
	}
	hash := sha256.Sum256([]byte(conversationID + "\x00" + memoryScope))
	key := fmt.Sprintf("note/context-%x", hash[:16])
	body := "## 会话续接记录（自动摘要，尚未经独立验证）\n来源对话: " + conversationID + "\n代理范围: " + memoryScope + "\n\n" + summary
	if snapshot != "" {
		body = appendSummarizationSection(body, summarizationTranscriptRetrievalSection(snapshot))
	}
	err = db.SaveContextCheckpoint(&database.ProjectFact{
		ProjectID: projectID, FactKey: key, Category: "note", Confidence: "tentative",
		SourceConversationID: conversationID,
		Summary:              checkpointSummaryLabel(memoryScope, summary, cfg.FactSummaryMaxRunesEffective()),
		Body:                 body,
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

func checkpointSummaryLabel(scope, summary string, cap int) string {
	// A generated note must not turn a permissive fact-summary setting into
	// automatic reinjection of the entire compaction output.
	if cap > 200 {
		cap = 200
	}
	if start := strings.Index(summary, summarizationSnapshotHeading); start >= 0 {
		summary = summary[:start]
	}
	if clues := checkpointRetrievalClues(summary); clues != "" {
		summary = clues
	}
	var lines []string
	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<") {
			continue
		}
		lines = append(lines, strings.TrimLeft(line, "- *"))
		if len(lines) == 4 {
			break
		}
	}
	label := "自动续接（" + strings.Join(strings.Fields(scope), " ") + "）：" + strings.Join(lines, "；")
	return truncateIndexLabel(label, cap)
}

// Reuse section 8 without changing the original summary, ledger or model calls.
// Older summaries and an empty retrieval section retain the existing fallback.
func checkpointRetrievalClues(summary string) string {
	inSection := false
	var clues []string
	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## 8.") && strings.Contains(line, "原始证据检索线索") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "</") {
			break
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "- *"))
		if line != "" && line != "无" && line != "无。" && !strings.HasPrefix(line, "<") {
			clues = append(clues, line)
		}
		if len(clues) == 4 {
			break
		}
	}
	return strings.Join(clues, "\n")
}

func truncateIndexLabel(text string, cap int) string {
	if cap < 1 {
		return ""
	}
	runes := []rune(text)
	if len(runes) > cap {
		return string(runes[:cap-1]) + "…"
	}
	return text
}

func finalizeSummarizationMemory(msg adk.Message, originals []adk.Message, transcriptPath string, db *database.DB, projectID, conversationID, memoryScope string, cfg config.ProjectConfig, logger *zap.Logger) adk.Message {
	msg = stripAnalysisFromSummarizationMessage(msg)
	if ledgerSec := buildAuditedFilesLedgerSection(originals); ledgerSec != "" && msg != nil {
		clone := *msg
		if clone.Content != "" {
			clone.Content = appendSummarizationSection(clone.Content, ledgerSec)
		}
		msg = &clone
	}
	snapshot, err := writeSummarizationTranscriptSnapshot(transcriptPath, originals)
	if err != nil && logger != nil {
		if snapshot != "" {
			logger.Warn("更新上下文兼容入口失败，原始快照仍可读取", zap.String("snapshot", snapshot), zap.Error(err))
		} else {
			logger.Warn("保存上下文证据快照失败", zap.Error(err))
		}
	}
	// Publish successful immutable evidence even if only its alias failed.
	if snapshot != "" {
		msg = appendTranscriptPathToSummarizationMessage(msg, snapshot)
	}
	text := ""
	if msg != nil {
		text = msg.Content
		if text == "" {
			for _, part := range msg.UserInputMultiContent {
				if part.Type == schema.ChatMessagePartTypeText {
					text = part.Text
					break
				}
			}
		}
	}
	if snapshot != "" {
		text = strings.TrimSuffix(text, "\n\n"+summarizationTranscriptRetrievalSection(snapshot))
	}
	if inner, ok := extractSummarizationSummaryBody(text); ok {
		text = inner
	}
	key, err := persistSummarizationCheckpoint(db, projectID, conversationID, memoryScope, text, snapshot, cfg)
	if err != nil {
		if logger != nil {
			logger.Warn("保存黑板续接记录失败", zap.String("conversationId", conversationID), zap.Error(err))
		}
		return msg
	}
	if key == "" || msg == nil {
		return msg
	}
	section := "本轮续接摘要已保存到项目黑板 get_project_fact(" + key + ")；这是未独立验证的工作记录，使用前核对证据与当前任务范围。"
	clone := *msg
	if clone.Content != "" {
		clone.Content = appendSummarizationSection(clone.Content, section)
	}
	if len(clone.UserInputMultiContent) > 0 {
		clone.UserInputMultiContent = append([]schema.MessageInputPart(nil), clone.UserInputMultiContent...)
		for i, part := range clone.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeText {
				clone.UserInputMultiContent[i].Text = appendSummarizationSection(part.Text, section)
				break
			}
		}
	}
	return &clone
}

func summarizationMemoryScope(scopes []string) string {
	if len(scopes) > 0 && strings.TrimSpace(scopes[0]) != "" {
		return strings.TrimSpace(scopes[0])
	}
	return "main"
}

func summarizationScopeID(scope string) string {
	hash := sha256.Sum256([]byte(scope))
	return fmt.Sprintf("%x", hash[:16])
}


const auditedFilesLedgerHeading = "## 9. 已读取文件台账（系统自动生成，严禁重复调用 read_file 重读）"

func buildAuditedFilesLedgerSection(originals []adk.Message) string {
	if len(originals) == 0 {
		return ""
	}
	var order []string
	seen := make(map[string][]string)
	addEntry := func(fp, rng string) {
		fp = strings.TrimSpace(fp)
		if fp == "" {
			return
		}
		if _, ok := seen[fp]; !ok {
			order = append(order, fp)
		}
		if rng != "" {
			for _, existing := range seen[fp] {
				if existing == rng {
					return
				}
			}
			seen[fp] = append(seen[fp], rng)
		}
	}

	for _, m := range originals {
		if m == nil {
			continue
		}
		// Preserve entries from any prior summary's ledger section
		if m.Content != "" && strings.Contains(m.Content, auditedFilesLedgerHeading) {
			inLedger := false
			for _, line := range strings.Split(m.Content, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, auditedFilesLedgerHeading) {
					inLedger = true
					continue
				}
				if !inLedger {
					continue
				}
				if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "</") {
					break
				}
				if strings.HasPrefix(line, "- `") {
					rest := strings.TrimPrefix(line, "- `")
					if idx := strings.Index(rest, "`"); idx > 0 {
						fp := rest[:idx]
						rng := strings.TrimSpace(rest[idx+1:])
						addEntry(fp, rng)
					}
				}
			}
		}
		for _, tc := range m.ToolCalls {
			if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), "read_file") {
				continue
			}
			raw := strings.TrimSpace(tc.Function.Arguments)
			if raw == "" {
				continue
			}
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				continue
			}
			fp, _ := args["file_path"].(string)
			if fp == "" {
				continue
			}
			offset := 1
			if v, ok := args["offset"].(float64); ok && int(v) >= 0 {
				offset = int(v)
			}
			limit := 200
			if v, ok := args["limit"].(float64); ok && int(v) > 0 {
				limit = int(v)
			}
			addEntry(fp, fmt.Sprintf("offset=%d,limit=%d", offset, limit))
		}
	}
	if len(order) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(auditedFilesLedgerHeading)
	sb.WriteString("\n以下文件在压缩前的历史轮次中**已经读取完毕**。除非确实需要读取尚未覆盖的行号区间，否则**严禁再次调用 `read_file` 重新读取下列已读文件区间**！请直接检查尚未读取的剩余文件，或基于已有发现直接输出最终报告：\n")
	for _, fp := range order {
		rngs := strings.Join(seen[fp], "; ")
		if rngs != "" {
			sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", fp, rngs))
		} else {
			sb.WriteString(fmt.Sprintf("- `%s`\n", fp))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
