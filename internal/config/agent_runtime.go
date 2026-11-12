package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AgentRuntimeConfig keeps runtime selection separate from model/provider selection.
type AgentRuntimeConfig struct {
	Engine     string `yaml:"engine,omitempty" json:"engine,omitempty"`
	NodePath   string `yaml:"node_path,omitempty" json:"node_path,omitempty"`
	WorkerPath string `yaml:"worker_path,omitempty" json:"worker_path,omitempty"`
	SessionDir string `yaml:"session_dir,omitempty" json:"session_dir,omitempty"`
}

// AgentSessionDir is shared by the runner and conversation cleanup, including
// configurations that omit database.path or restore the default session root.
func (c *Config) AgentSessionDir() string {
	if dir := strings.TrimSpace(c.AgentRuntime.SessionDir); dir != "" {
		return dir
	}
	dbPath := strings.TrimSpace(c.Database.Path)
	if dbPath == "" {
		dbPath = "data/conversations.db"
	}
	return filepath.Join(filepath.Dir(dbPath), "pi-sessions")
}

func (c AgentRuntimeConfig) Validate() error {
	engine := strings.ToLower(strings.TrimSpace(c.Engine))
	if engine != "" && engine != "pi" {
		return fmt.Errorf("unsupported agent runtime %q: this version only supports pi", c.Engine)
	}
	return nil
}
