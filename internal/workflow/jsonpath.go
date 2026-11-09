package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func evalJSONPathValue(input any, path string) any {
	path = strings.TrimSpace(path)
	if path == "" || path == "$" || path == "." {
		return input
	}
	if strings.HasPrefix(path, "$.") {
		path = strings.TrimPrefix(path, "$.")
	} else if strings.HasPrefix(path, ".") {
		path = strings.TrimPrefix(path, ".")
	} else if strings.HasPrefix(path, "$") {
		path = strings.TrimPrefix(path, "$")
	}
	cur := normalizeJSONInput(input)
	for _, token := range parseJSONPathTokens(path) {
		if token == "" {
			continue
		}
		switch v := cur.(type) {
		case map[string]any:
			cur = v[token]
		case []any:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(v) {
				return ""
			}
			cur = v[idx]
		default:
			return ""
		}
	}
	if cur == nil {
		return ""
	}
	return cur
}

func normalizeJSONInput(input any) any {
	switch v := input.(type) {
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			return decoded
		}
		return v
	case []byte:
		var decoded any
		if err := json.Unmarshal(v, &decoded); err == nil {
			return decoded
		}
		return string(v)
	default:
		return input
	}
}

func parseJSONPathTokens(path string) []string {
	var tokens []string
	var buf strings.Builder
	for i := 0; i < len(path); i++ {
		ch := path[i]
		switch ch {
		case '.':
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
		case '[':
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
			j := i + 1
			for j < len(path) && path[j] != ']' {
				j++
			}
			if j <= len(path) {
				token := strings.Trim(path[i+1:j], `"' `)
				tokens = append(tokens, token)
				i = j
			}
		default:
			buf.WriteByte(ch)
		}
	}
	if buf.Len() > 0 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

func validateJSONPathSyntax(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("JSONPath cannot be empty")
	}
	if !strings.HasPrefix(path, "$") && !strings.HasPrefix(path, ".") {
		return fmt.Errorf("JSONPath/JQ path must start with $ or .")
	}
	if strings.Contains(path, "..") || strings.ContainsAny(path, "*?()|") {
		return fmt.Errorf("only a safe path subset is supported (no wildcards, recursion, or expressions)")
	}
	if strings.Count(path, "[") != strings.Count(path, "]") {
		return fmt.Errorf("JSONPath brackets do not match")
	}
	return nil
}
