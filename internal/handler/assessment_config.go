package handler

import (
	"encoding/json"
	"fmt"

	"cyberstrike-ai/internal/config"
)

// AssessmentConfigSnapshot shares the same lock as configuration writes. Deep
// copying prevents a model request from combining a key, endpoint or approval
// whitelist from different configuration revisions during hot reload.
func (h *ConfigHandler) AssessmentConfigSnapshot() (*config.Config, error) {
	h.mu.RLock()
	if h.config == nil {
		h.mu.RUnlock()
		return nil, fmt.Errorf("configuration unavailable")
	}
	data, err := json.Marshal(struct {
		OpenAI config.OpenAIConfig `json:"openai"`
		AI     config.AIConfig     `json:"ai"`
		Hitl   config.HitlConfig   `json:"hitl"`
	}{h.config.OpenAI, h.config.AI, h.config.Hitl})
	h.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	var snapshot config.Config
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
