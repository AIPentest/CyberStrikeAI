package handler

import (
	"testing"

	"cyberstrike-ai/internal/modelmeta"

	"github.com/gin-gonic/gin"
)

func TestAddModelContextToResponseLocalProfilePreview(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{
		Backend:       modelmeta.BackendOllama,
		ContextWindow: 131072,
		Source:        "ollama_ps",
		Loaded:        true,
	}, 120000, "local")

	if got := resp["context_window"]; got != 131072 {
		t.Fatalf("context_window = %#v, want 131072", got)
	}
	if got := resp["effective_max_total_tokens"]; got != 78643 {
		t.Fatalf("effective_max_total_tokens = %#v, want 78643", got)
	}
	warnings, ok := resp["context_warnings"].([]string)
	if !ok {
		t.Fatalf("context_warnings type = %T", resp["context_warnings"])
	}
	if !containsWarning(warnings, "max_total_tokens_clamped") {
		t.Fatalf("missing clamp warning: %#v", warnings)
	}
	if countWarning(warnings, "max_total_tokens_clamped") != 1 {
		t.Fatalf("clamp warning duplicated: %#v", warnings)
	}
}

func TestAddModelContextToResponseLocalUnknownWindowAssumes32K(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{}, 120000, "local")
	if got := resp["effective_max_total_tokens"]; got != 16384 {
		t.Fatalf("effective_max_total_tokens = %#v, want 16384", got)
	}
	warnings := resp["context_warnings"].([]string)
	if !containsWarning(warnings, "local_context_window_unknown") {
		t.Fatalf("missing unknown-window warning: %#v", warnings)
	}
}

func TestAddModelContextToResponseAutoProfileKeepsNoPreviewWithoutWindow(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{}, 120000, "auto")
	if _, ok := resp["effective_max_total_tokens"]; ok {
		t.Fatalf("auto profile without window should not preview a budget: %#v", resp)
	}
}

func TestAddModelContextToResponseDedupesWarnings(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{
		ContextWindow: 32768,
		Warnings:      []string{modelmeta.WarningContextUnknown, modelmeta.WarningContextUnknown},
	}, 64000, "auto")
	warnings := resp["context_warnings"].([]string)
	if countWarning(warnings, modelmeta.WarningContextUnknown) != 1 {
		t.Fatalf("warnings not deduped: %#v", warnings)
	}
	if !containsWarning(warnings, modelmeta.WarningBudgetExceedsWindow) {
		t.Fatalf("missing budget-exceeds-window warning: %#v", warnings)
	}
}

func containsWarning(warnings []string, want string) bool {
	for _, code := range warnings {
		if code == want {
			return true
		}
	}
	return false
}

func countWarning(warnings []string, want string) int {
	count := 0
	for _, code := range warnings {
		if code == want {
			count++
		}
	}
	return count
}
