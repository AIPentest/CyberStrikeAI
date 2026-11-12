package handler

import "strings"

// normalizeFofaFields accepts the legacy province return-field alias from saved
// forms and API clients. FOFA exports this value as region. Query syntax and
// other search providers do not pass through this conversion.
func normalizeFofaFields(raw string) string {
	fields := splitAndCleanCSV(raw)
	seen := make(map[string]bool, len(fields))
	normalized := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "province" {
			field = "region"
		}
		if !seen[field] {
			normalized = append(normalized, field)
			seen[field] = true
		}
	}
	return strings.Join(normalized, ",")
}
