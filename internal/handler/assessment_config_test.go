package handler

import (
	"fmt"
	"sync"
	"testing"

	"cyberstrike-ai/internal/config"
)

func TestAssessmentConfigSnapshotIsConsistentAndDetached(t *testing.T) {
	h := &ConfigHandler{config: &config.Config{}}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			h.mu.Lock()
			tag := fmt.Sprint(i)
			h.config.OpenAI = config.OpenAIConfig{Model: tag, APIKey: "fixture-" + tag, Headers: map[string]string{"revision": tag}}
			h.config.Hitl.ToolWhitelist = []string{tag}
			h.mu.Unlock()
		}
	}()
	for i := 0; i < 500; i++ {
		s, err := h.AssessmentConfigSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		if s.OpenAI.Model != "" && (s.OpenAI.APIKey != "fixture-"+s.OpenAI.Model || s.Hitl.ToolWhitelist[0] != s.OpenAI.Model) {
			t.Fatal("mixed configuration revisions")
		}
		if s.OpenAI.Headers != nil {
			s.OpenAI.Headers["revision"] = "modified snapshot"
		}
	}
	wg.Wait()
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.config.OpenAI.Headers["revision"] == "modified snapshot" {
		t.Fatal("snapshot shares mutable headers")
	}
}
