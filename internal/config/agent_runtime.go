package config

import (
	"fmt"
	"strings"
)

// AgentRuntimeConfig keeps runtime selection separate from model/provider selection.
type AgentRuntimeConfig struct {
	Engine     string `yaml:"engine,omitempty" json:"engine,omitempty"`
	NodePath   string `yaml:"node_path,omitempty" json:"node_path,omitempty"`
	WorkerPath string `yaml:"worker_path,omitempty" json:"worker_path,omitempty"`
	SessionDir string `yaml:"session_dir,omitempty" json:"session_dir,omitempty"`
}

func (c AgentRuntimeConfig) UsesPi() bool {
	return strings.TrimSpace(c.Engine) == "" || strings.EqualFold(strings.TrimSpace(c.Engine), "pi")
}
func (c AgentRuntimeConfig) Validate() error {
	engine := strings.ToLower(strings.TrimSpace(c.Engine))
	if engine != "" && engine != "pi" && engine != "eino" {
		return fmt.Errorf("unknown agent runtime %q (expected pi or eino)", c.Engine)
	}
	return nil
}
