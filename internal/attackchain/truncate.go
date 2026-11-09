package attackchain

import (
	"strings"
	"unicode/utf8"

	"go.uber.org/zap"
)

const (
	attackChainTruncationMarker = "\n\n...[attack chain input truncated]...\n\n"
	attackChainSystemReserve    = 256
	attackChainSafetyReserve    = 2048
)

// attackChainMaxCompletionTokens 为攻击链 JSON 输出预留的 completion token 上限。
func attackChainMaxCompletionTokens(maxTotal int) int {
	const capTokens = 16384
	if maxTotal <= 0 {
		return 8192
	}
	v := maxTotal / 8
	if v < 4096 {
		v = 4096
	}
	if v > capTokens {
		v = capTokens
	}
	return v
}

func (b *Builder) modelName() string {
	if b.openAIConfig != nil && b.openAIConfig.Model != "" {
		return b.openAIConfig.Model
	}
	return "gpt-4"
}

func (b *Builder) countTokens(text string) int {
	if text == "" {
		return 0
	}
	n, err := b.tokenCounter.Count(b.modelName(), text)
	if err != nil {
		return utf8.RuneCountInString(text) / 4
	}
	return n
}

// attackChainPayloadTokenBudget 计算 reactInput + modelOutput 可用的 token 预算。
func (b *Builder) attackChainPayloadTokenBudget() int {
	maxTotal := b.maxTokens
	if maxTotal <= 0 {
		maxTotal = 100000
	}
	templateTok := b.countTokens(b.buildSimplePrompt("", ""))
	completion := attackChainMaxCompletionTokens(maxTotal)
	reserve := templateTok + attackChainSystemReserve + completion + attackChainSafetyReserve
	budget := maxTotal - reserve
	minBudget := maxTotal * 35 / 100
	if budget < minBudget {
		budget = minBudget
	}
	if budget < 4096 {
		budget = 4096
	}
	return budget
}

// fitAttackChainPayload 在构建最终 prompt 前压缩 ReAct 轨迹与模型输出，避免超出模型上下文。
func (b *Builder) fitAttackChainPayload(reactInput, modelOutput string) (string, string, bool) {
	budget := b.attackChainPayloadTokenBudget()
	modelBudget := budget * 15 / 100
	if modelBudget < 512 {
		modelBudget = 512
	}
	reactBudget := budget - modelBudget

	origReactTok := b.countTokens(reactInput)
	origModelTok := b.countTokens(modelOutput)
	truncated := false

	outModel := modelOutput
	if origModelTok > modelBudget {
		outModel = truncateTextByTokens(b, modelOutput, modelBudget)
		truncated = true
	}

	outReact := reactInput
	perToolLimits := []int{12000, 6000, 3000, 1500, 800}
	for _, lim := range perToolLimits {
		compact := compactFormattedToolBodies(outReact, lim)
		if compact != outReact {
			outReact = compact
			truncated = true
		}
		if b.countTokens(outReact) <= reactBudget {
			break
		}
	}

	if b.countTokens(outReact) > reactBudget {
		outReact = truncateTextByTokens(b, outReact, reactBudget)
		truncated = true
	}

	if truncated {
		b.logger.Info("Attack chain input truncated to fit the token budget",
			zap.Int("maxTotalTokens", b.maxTokens),
			zap.Int("payloadBudget", budget),
			zap.Int("reactBudget", reactBudget),
			zap.Int("modelBudget", modelBudget),
			zap.Int("reactInputTokensBefore", origReactTok),
			zap.Int("reactInputTokensAfter", b.countTokens(outReact)),
			zap.Int("modelOutputTokensBefore", origModelTok),
			zap.Int("modelOutputTokensAfter", b.countTokens(outModel)),
			zap.Int("maxCompletionTokens", attackChainMaxCompletionTokens(b.maxTokens)),
		)
	}

	return outReact, outModel, truncated
}

// compactFormattedToolBodies 缩短格式化 trace 中 [tool] 消息的正文，保留工具头与调用 ID。
func compactFormattedToolBodies(s string, maxRunesPerBody int) string {
	if maxRunesPerBody <= 0 || s == "" {
		return s
	}
	const marker = "[tool]"
	var out strings.Builder
	remaining := s
	changed := false
	for {
		idx := strings.Index(remaining, marker)
		if idx < 0 {
			out.WriteString(remaining)
			break
		}
		out.WriteString(remaining[:idx])
		remaining = remaining[idx:]
		nl := strings.IndexByte(remaining, '\n')
		if nl < 0 {
			out.WriteString(remaining)
			break
		}
		header := remaining[:nl+1]
		remaining = remaining[nl+1:]
		bodyEnd := strings.Index(remaining, "\n\n[")
		var body, rest string
		if bodyEnd < 0 {
			body = remaining
			rest = ""
		} else {
			body = remaining[:bodyEnd]
			rest = remaining[bodyEnd:]
		}
		if runeLen(body) > maxRunesPerBody {
			body = truncateRunesWithNotice(body, maxRunesPerBody)
			changed = true
		}
		out.WriteString(header)
		out.WriteString(body)
		remaining = rest
		if rest == "" {
			break
		}
	}
	if !changed {
		return s
	}
	return out.String()
}

func truncateTextByTokens(b *Builder, text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return ""
	}
	if b.countTokens(text) <= maxTokens {
		return text
	}
	markerTok := b.countTokens(attackChainTruncationMarker)
	usable := maxTokens - markerTok
	if usable < 256 {
		usable = maxTokens / 2
	}
	headBudget := usable * 60 / 100
	tailBudget := usable - headBudget
	head := takeTokensFromStart(b, text, headBudget)
	tail := takeTokensFromEnd(b, text, tailBudget)
	return head + attackChainTruncationMarker + tail
}

func takeTokensFromStart(b *Builder, text string, maxTokens int) string {
	rs := []rune(text)
	if len(rs) == 0 || maxTokens <= 0 {
		return ""
	}
	lo, hi := 0, len(rs)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if b.countTokens(string(rs[:mid])) <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(rs[:lo])
}

func takeTokensFromEnd(b *Builder, text string, maxTokens int) string {
	rs := []rune(text)
	if len(rs) == 0 || maxTokens <= 0 {
		return ""
	}
	lo, hi := 0, len(rs)
	for lo < hi {
		mid := (lo + hi) / 2
		if b.countTokens(string(rs[mid:])) <= maxTokens {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return string(rs[lo:])
}

func truncateRunesWithNotice(s string, maxRunes int) string {
	rs := []rune(s)
	if len(rs) <= maxRunes {
		return s
	}
	const notice = "\n...[tool output truncated]...\n"
	noticeRunes := []rune(notice)
	keep := maxRunes - len(noticeRunes)
	if keep < 200 {
		keep = maxRunes * 2 / 3
	}
	if keep < 1 {
		return notice
	}
	head := keep * 70 / 100
	tail := keep - head
	return string(rs[:head]) + notice + string(rs[len(rs)-tail:])
}

func runeLen(s string) int {
	return len([]rune(s))
}
