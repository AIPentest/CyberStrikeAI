package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"cyberstrike-ai/internal/config"

	"github.com/pkoukk/tiktoken-go"
)

// postRetrieveMaxPrefetchCap 限制单次向量候选上限，避免误配置导致全表扫压力过大。
const postRetrieveMaxPrefetchCap = 200

// DocumentReranker 精排（HTTP dashscope / Cohere 兼容 API），由 [WireRetrieverPipeline] 注入。
type DocumentReranker interface {
	Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error)
}

// NopDocumentReranker 占位实现，便于测试或未启用重排时显式注入。
type NopDocumentReranker struct{}

// Rerank implements [DocumentReranker] as no-op.
func (NopDocumentReranker) Rerank(_ context.Context, _ string, docs []*Document) ([]*Document, error) {
	return docs, nil
}

var tiktokenEncMu sync.Mutex
var tiktokenEncCache = map[string]*tiktoken.Tiktoken{}

func encodingForTokenizerModel(model string) (*tiktoken.Tiktoken, error) {
	m := strings.TrimSpace(model)
	if m == "" {
		m = "gpt-4"
	}
	tiktokenEncMu.Lock()
	defer tiktokenEncMu.Unlock()
	if enc, ok := tiktokenEncCache[m]; ok {
		return enc, nil
	}
	enc, err := tiktoken.EncodingForModel(m)
	if err != nil {
		enc, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return nil, err
		}
	}
	tiktokenEncCache[m] = enc
	return enc, nil
}

func countDocTokens(text, model string) (int, error) {
	enc, err := encodingForTokenizerModel(model)
	if err != nil {
		return 0, err
	}
	toks := enc.Encode(text, nil, nil)
	return len(toks), nil
}

// normalizeContentFingerprintKey 去重键：trim + 空白折叠（不改动大小写，避免合并仅大小写不同的代码片段）。
func normalizeContentFingerprintKey(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

func contentNormKey(d *Document) string {
	if d == nil {
		return ""
	}
	n := normalizeContentFingerprintKey(d.Content)
	if n == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(n))
	return hex.EncodeToString(sum[:])
}

// dedupeByNormalizedContent 按规范化正文去重，保留向量检索顺序中首次出现的文档（同正文仅保留一条）。
func dedupeByNormalizedContent(docs []*Document) []*Document {
	if len(docs) < 2 {
		return docs
	}
	seen := make(map[string]struct{}, len(docs))
	out := make([]*Document, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			continue
		}
		k := contentNormKey(d)
		if k == "" {
			out = append(out, d)
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, d)
	}
	return out
}

// truncateDocumentsByBudget 按检索顺序整段保留文档，直至字符数或 token 数（任一启用）超限则停止。
func truncateDocumentsByBudget(docs []*Document, maxRunes, maxTokens int, tokenModel string) ([]*Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}
	unlimitedChars := maxRunes <= 0
	unlimitedTok := maxTokens <= 0
	if unlimitedChars && unlimitedTok {
		return docs, nil
	}

	remRunes := maxRunes
	remTok := maxTokens
	out := make([]*Document, 0, len(docs))

	for _, d := range docs {
		if d == nil || strings.TrimSpace(d.Content) == "" {
			continue
		}
		runes := utf8.RuneCountInString(d.Content)
		if !unlimitedChars && runes > remRunes {
			break
		}
		var tok int
		var err error
		if !unlimitedTok {
			tok, err = countDocTokens(d.Content, tokenModel)
			if err != nil {
				return nil, fmt.Errorf("token count: %w", err)
			}
			if tok > remTok {
				break
			}
		}
		out = append(out, d)
		if !unlimitedChars {
			remRunes -= runes
		}
		if !unlimitedTok {
			remTok -= tok
		}
	}
	return out, nil
}

// EffectivePrefetchTopK 计算每条 MultiQuery 变体在向量阶段的候选条数（供融合 / 重排 / 后处理）。
func EffectivePrefetchTopK(topK int, po *config.PostRetrieveConfig) int {
	if topK < 1 {
		topK = 5
	}
	fetch := topK * 4
	if fetch < 20 {
		fetch = 20
	}
	if po != nil && po.PrefetchTopK > 0 {
		fetch = po.PrefetchTopK
	}
	if fetch > postRetrieveMaxPrefetchCap {
		fetch = postRetrieveMaxPrefetchCap
	}
	return fetch
}

// ApplyPostRetrieve 检索后处理：规范化正文去重 → 预算截断 → 最终 TopK（精排已在流水线中完成）。
func ApplyPostRetrieve(docs []*Document, po *config.PostRetrieveConfig, tokenModel string, finalTopK int) ([]*Document, error) {
	if finalTopK < 1 {
		finalTopK = 5
	}
	if len(docs) == 0 {
		return docs, nil
	}

	maxChars := 0
	maxTok := 0
	if po != nil {
		maxChars = po.MaxContextChars
		maxTok = po.MaxContextTokens
	}

	out := dedupeByNormalizedContent(docs)

	var err error
	out, err = truncateDocumentsByBudget(out, maxChars, maxTok, tokenModel)
	if err != nil {
		return nil, err
	}

	if len(out) > finalTopK {
		out = out[:finalTopK]
	}
	return out, nil
}
