package handler

import (
	"testing"

	"cyberstrike-ai/internal/config"
	"gopkg.in/yaml.v3"
)

func TestPiRuntimeSettingsSurviveSaveAndPartialUpdate(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(`
agent_runtime:
  node_path: /custom/node
  worker_path: /custom/worker.js
  session_dir: /private/sessions
multi_agent:
  eino_middleware:
    tool_search_enable: true
  eino_skills:
    disable: false
  eino_callbacks:
    enabled: true
`), &doc); err != nil {
		t.Fatal(err)
	}
	var original config.Config
	if err := doc.Decode(&original); err != nil {
		t.Fatal(err)
	}
	cfg := original.AgentRuntime
	nodePath := "/updated/node"
	applyAgentRuntimeUpdate(&cfg, &AgentRuntimeUpdate{NodePath: &nodePath})
	updateAgentRuntimeConfig(&doc, cfg)
	updateMultiAgentConfig(&doc, config.MultiAgentConfig{})
	var restored config.Config
	if err := doc.Decode(&restored); err != nil {
		t.Fatal(err)
	}
	if restored.AgentRuntime.Engine != "pi" || restored.AgentRuntime.NodePath != nodePath || restored.AgentRuntime.WorkerPath != "/custom/worker.js" || restored.AgentRuntime.SessionDir != "/private/sessions" {
		t.Fatalf("lost runtime settings: %+v", restored.AgentRuntime)
	}
	if !restored.MultiAgent.Runtime.ToolSearchEnable {
		t.Fatal("legacy runtime settings were discarded")
	}
	var raw map[string]any
	if err := doc.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	ma := raw["multi_agent"].(map[string]any)
	for _, key := range []string{"eino_middleware", "eino_skills", "eino_callbacks"} {
		if _, ok := ma[key]; ok {
			t.Fatalf("retired key emitted: %s", key)
		}
	}
}
