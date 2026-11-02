package config

import "strings"

// auditModelHeaders treats an explicit audit identity as a separate credential
// boundary, even when its provider or URL happens to equal the main model's.
// A model-name-only override can safely continue using the main identity.
func auditModelHeaders(main, audit OpenAIConfig) map[string]string {
	if len(audit.Headers) > 0 {
		return ExpandHeaderMap(audit.Headers)
	}
	if strings.TrimSpace(audit.Provider) != "" || strings.TrimSpace(audit.BaseURL) != "" || strings.TrimSpace(audit.APIKey) != "" {
		return nil
	}
	return ExpandHeaderMap(main.Headers)
}
