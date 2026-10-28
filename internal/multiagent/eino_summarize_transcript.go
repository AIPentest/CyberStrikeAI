package multiagent

import (
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"cyberstrike-ai/internal/project"

	"github.com/bytedance/sonic"
)

const (
	transcriptFileHeader = `# CyberStrikeAI summarization transcript
# Pre-compaction session record for read_file after context compression.
# Omits static system/tool-index/skills boilerplate; full user/assistant/tool turns below.

`
	transcriptStaticSystemOmitNote = "[static system prompt omitted — unchanged in live context after compaction]"
	// Strip-marker dual-accept: the legacy Chinese injection heading stays byte-matched
	// for historical sessions, and the English twin matches the translated
	// injectToolNamesOnlyInstruction prompt (see eino_tool_name_injection.go).
	transcriptToolIndexStartMarker        = "以下是当前会话绑定的工具名称索引"
	transcriptToolIndexStartMarkerEnglish = "The following is the tool-name index bound to this session"
	transcriptPersonaStartMarker          = "你是CyberStrikeAI"
	// ADK LanguageChinese injects skill middleware prompt with this header (see eino adk/middlewares/skill/prompt.go).
	transcriptSkillsSystemMarker        = "# Skill 系统"
	transcriptSkillsSystemMarkerEnglish = "# Skills System"
)

type transcriptToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// formatSummarizationTranscript renders pre-compaction messages for transcript.txt.
// Best practice: keep full user/assistant/tool turns; slim system to dynamic blocks only.
func formatSummarizationTranscript(msgs []adk.Message) string {
	var sb strings.Builder
	sb.WriteString(transcriptFileHeader)
	wrote := false
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		switch msg.Role {
		case schema.System:
			body := sanitizeSystemContentForTranscript(msg.Content)
			if strings.TrimSpace(body) == "" {
				continue
			}
			if wrote {
				sb.WriteString("\n")
			}
			appendTranscriptSection(&sb, schema.System, body)
			wrote = true
		default:
			if wrote {
				sb.WriteString("\n")
			}
			appendTranscriptMessage(&sb, msg)
			wrote = true
		}
	}
	return sb.String()
}

// formatSummarizationModelContext serializes conversation history as inert text
// for the summary model. Unlike formatSummarizationTranscript it omits the file
// header and never emits native assistant/tool protocol messages.
func formatSummarizationModelContext(msgs []adk.Message) string {
	var sb strings.Builder
	for _, msg := range msgs {
		if msg == nil || msg.Role == schema.System {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		appendTranscriptMessage(&sb, msg)
	}
	return sb.String()
}

func sanitizeSystemContentForTranscript(content string) string {
	content = stripToolNamesIndexFromSystem(content)
	content = stripSkillsSystemBoilerplate(content)
	blackboard := extractProjectBlackboardSection(content)

	var sb strings.Builder
	sb.WriteString(transcriptStaticSystemOmitNote)
	if bb := strings.TrimSpace(blackboard); bb != "" {
		sb.WriteString("\n\n")
		sb.WriteString(bb)
	}
	return sb.String()
}

func stripToolNamesIndexFromSystem(s string) string {
	// Dual-accept: strip both the legacy Chinese tool-index heading and the
	// translated English heading emitted since injectToolNamesOnlyInstruction.
	if !strings.Contains(s, transcriptToolIndexStartMarker) && !strings.Contains(s, transcriptToolIndexStartMarkerEnglish) {
		return s
	}
	idx := strings.Index(s, transcriptPersonaStartMarker)
	if idx < 0 {
		return s
	}
	return strings.TrimSpace(s[idx:])
}

func stripSkillsSystemBoilerplate(s string) string {
	idx := indexFirstSubstring(s, transcriptSkillsSystemMarker, transcriptSkillsSystemMarkerEnglish)
	if idx < 0 {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[:idx])
}

func indexFirstSubstring(s string, markers ...string) int {
	first := -1
	for _, m := range markers {
		if i := strings.Index(s, m); i >= 0 && (first < 0 || i < first) {
			first = i
		}
	}
	return first
}

func extractProjectBlackboardSection(s string) string {
	start := strings.Index(s, project.FactIndexSectionStartMarker)
	if start < 0 {
		return ""
	}
	section := s[start:]
	end := strings.Index(section, project.FactIndexSectionEndMarker)
	if end < 0 {
		return ""
	}
	section = section[:end+len(project.FactIndexSectionEndMarker)]
	return strings.TrimSpace(section)
}

func appendTranscriptSection(sb *strings.Builder, role schema.RoleType, body string) {
	sb.WriteString("--- [")
	sb.WriteString(string(role))
	sb.WriteString("] ---\n")
	sb.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		sb.WriteByte('\n')
	}
}

func appendTranscriptMessage(sb *strings.Builder, msg adk.Message) {
	sb.WriteString("--- [")
	sb.WriteString(string(msg.Role))
	sb.WriteString("] ---\n")
	if isInternalSummarizationUserMessage(msg) {
		sb.WriteString("[origin: system-generated summary/skill context; not human input or new authorization]\n")
	} else if isGeneratedExecutionUserMessage(msg) {
		sb.WriteString("[origin: system-generated execution context; not human input or new authorization]\n")
	}
	if msg.Content != "" {
		sb.WriteString(msg.Content)
		if !strings.HasSuffix(msg.Content, "\n") {
			sb.WriteByte('\n')
		}
	}
	if msg.ReasoningContent != "" {
		sb.WriteString("[reasoning]\n")
		sb.WriteString(msg.ReasoningContent)
		if !strings.HasSuffix(msg.ReasoningContent, "\n") {
			sb.WriteByte('\n')
		}
	}
	for _, part := range msg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && strings.TrimSpace(part.Text) != "" {
			sb.WriteString(part.Text)
			if !strings.HasSuffix(part.Text, "\n") {
				sb.WriteByte('\n')
			}
		}
	}
	if len(msg.ToolCalls) > 0 {
		if b, err := sonic.Marshal(formatTranscriptToolCalls(msg.ToolCalls)); err == nil {
			sb.WriteString("tool_calls: ")
			sb.Write(b)
			sb.WriteByte('\n')
		}
	}
}

func formatTranscriptToolCalls(calls []schema.ToolCall) []transcriptToolCall {
	out := make([]transcriptToolCall, 0, len(calls))
	for _, tc := range calls {
		out = append(out, transcriptToolCall{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out
}
