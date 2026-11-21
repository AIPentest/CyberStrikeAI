package handler

import (
	"cyberstrike-ai/internal/config"
	"encoding/json"
	"fmt"
)

// RuntimeConfigSnapshot freezes one coherent configuration for a background run.
// The returned copy is private to the runner and must never be sent to clients.
func (h *ConfigHandler) RuntimeConfigSnapshot() (*config.Config, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.config == nil {
		return nil, fmt.Errorf("configuration unavailable")
	}
	b, err := json.Marshal(h.config)
	if err != nil {
		return nil, err
	}
	var out config.Config
	err = json.Unmarshal(b, &out)
	return &out, err
}
