package llm

import "strings"

func IsClaudeProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "claude" || provider == "anthropic"
}
