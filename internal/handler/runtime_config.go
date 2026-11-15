package handler

import (
	"cyberstrike-ai/internal/config"
	"gopkg.in/yaml.v3"
)

type AgentRuntimeUpdate struct {
	Engine     *string `json:"engine,omitempty"`
	NodePath   *string `json:"node_path,omitempty"`
	WorkerPath *string `json:"worker_path,omitempty"`
	SessionDir *string `json:"session_dir,omitempty"`
}

func applyAgentRuntimeUpdate(dst *config.AgentRuntimeConfig, src *AgentRuntimeUpdate) {
	if src == nil {
		return
	}
	if src.Engine != nil {
		dst.Engine = *src.Engine
	}
	if src.NodePath != nil {
		dst.NodePath = *src.NodePath
	}
	if src.WorkerPath != nil {
		dst.WorkerPath = *src.WorkerPath
	}
	if src.SessionDir != nil {
		dst.SessionDir = *src.SessionDir
	}
}

func updateAgentRuntimeConfig(doc *yaml.Node, cfg config.AgentRuntimeConfig) {
	node := ensureMap(doc.Content[0], "agent_runtime")
	setStringInMap(node, "engine", "pi")
	setStringInMap(node, "node_path", cfg.NodePath)
	setStringInMap(node, "worker_path", cfg.WorkerPath)
	setStringInMap(node, "session_dir", cfg.SessionDir)
}
func migrateConfigMapKey(node *yaml.Node, oldKey, newKey string) {
	hasNew := false
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == newKey {
			hasNew = true
			break
		}
	}
	if hasNew {
		removeKeyFromMap(node, oldKey)
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == oldKey {
			node.Content[i].Value = newKey
			return
		}
	}
}
