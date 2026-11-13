package knowledge

import (
	"fmt"
	"strings"
)

// Document metadata keys for Agent Document flowing through the RAG pipeline.
const (
	metaKBCategory   = "kb_category"
	metaKBTitle      = "kb_title"
	metaKBItemID     = "kb_item_id"
	metaKBChunkIndex = "kb_chunk_index"
	metaSimilarity   = "similarity"
)

// DSL keys for [VectorRetriever.Retrieve] via [retriever.WithDSLInfo].
const (
	DSLRiskType            = "risk_type"
	DSLSimilarityThreshold = "similarity_threshold"
	DSLSubIndexFilter      = "sub_index_filter"
)

// FormatEmbeddingInput matches the historical indexing format so existing embeddings
// stay comparable if users skip reindex; new indexes use the same string shape.
func FormatEmbeddingInput(category, title, chunkText string) string {
	return fmt.Sprintf("[风险类型：%s] [标题：%s]\n%s", category, title, chunkText)
}

// FormatQueryEmbeddingText builds the string embedded at query time so it matches
// [FormatEmbeddingInput] for the same risk category (title left empty for queries).
func FormatQueryEmbeddingText(riskType, query string) string {
	q := strings.TrimSpace(query)
	rt := strings.TrimSpace(riskType)
	if rt != "" {
		return FormatEmbeddingInput(rt, "", q)
	}
	return q
}

// MetaLookupString returns metadata string value or "" if absent.
func MetaLookupString(md map[string]any, key string) string {
	if md == nil {
		return ""
	}
	v, ok := md[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

// MetaStringOK returns trimmed non-empty string and true if present and non-empty.
func MetaStringOK(md map[string]any, key string) (string, bool) {
	s := strings.TrimSpace(MetaLookupString(md, key))
	if s == "" {
		return "", false
	}
	return s, true
}

// RequireMetaString requires a non-empty string metadata field.
func RequireMetaString(md map[string]any, key string) (string, error) {
	s, ok := MetaStringOK(md, key)
	if !ok {
		return "", fmt.Errorf("missing or empty metadata %q", key)
	}
	return s, nil
}

// RequireMetaInt requires an integer metadata field.
func RequireMetaInt(md map[string]any, key string) (int, error) {
	if md == nil {
		return 0, fmt.Errorf("missing metadata key %q", key)
	}
	v, ok := md[key]
	if !ok {
		return 0, fmt.Errorf("missing metadata key %q", key)
	}
	switch t := v.(type) {
	case int:
		return t, nil
	case int32:
		return int(t), nil
	case int64:
		return int(t), nil
	case float64:
		return int(t), nil
	default:
		return 0, fmt.Errorf("metadata %q: unsupported type %T", key, v)
	}
}

// DSLNumeric coerces DSL map values (e.g. from JSON) to float64.
func DSLNumeric(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	default:
		return 0, false
	}
}

// MetaFloat64OK reads a float metadata value.
func MetaFloat64OK(md map[string]any, key string) (float64, bool) {
	if md == nil {
		return 0, false
	}
	v, ok := md[key]
	if !ok {
		return 0, false
	}
	return DSLNumeric(v)
}
