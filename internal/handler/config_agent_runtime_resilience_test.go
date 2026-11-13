package handler

import (
	"testing"

	"cyberstrike-ai/internal/config"
	"gopkg.in/yaml.v3"
)

func TestUpdateMultiAgentConfigWritesAgentModelResilience(t *testing.T) {
	doc := &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}},
	}

	updateMultiAgentConfig(doc, config.MultiAgentConfig{
		Enabled:                      true,
		RobotDefaultAgentMode:        "deep",
		PlanExecuteLoopMaxIterations: 3,
		Runtime: config.AgentTuningConfig{
			ModelRetryMaxRetries:    5,
			ModelRetryMaxBackoffSec: 45,
			ModelFailoverChannels:   []string{"backup-openai", "backup-claude", "backup-openai"},
			ModelFailoverMaxRetries: 2,
		},
	})

	var got struct {
		MultiAgent struct {
			Runtime struct {
				ModelRetryMaxRetries    int      `yaml:"model_retry_max_retries"`
				ModelRetryMaxBackoffSec int      `yaml:"model_retry_max_backoff_sec"`
				ModelFailoverChannels   []string `yaml:"model_failover_channels"`
				ModelFailoverMaxRetries int      `yaml:"model_failover_max_retries"`
			} `yaml:"runtime"`
		} `yaml:"multi_agent"`
	}
	if err := doc.Decode(&got); err != nil {
		t.Fatalf("decode config yaml: %v", err)
	}

	mw := got.MultiAgent.Runtime
	if mw.ModelRetryMaxRetries != 5 {
		t.Fatalf("model_retry_max_retries = %d, want 5", mw.ModelRetryMaxRetries)
	}
	if mw.ModelRetryMaxBackoffSec != 45 {
		t.Fatalf("model_retry_max_backoff_sec = %d, want 45", mw.ModelRetryMaxBackoffSec)
	}
	if mw.ModelFailoverMaxRetries != 2 {
		t.Fatalf("model_failover_max_retries = %d, want 2", mw.ModelFailoverMaxRetries)
	}
	wantChannels := []string{"backup-openai", "backup-claude"}
	if len(mw.ModelFailoverChannels) != len(wantChannels) {
		t.Fatalf("model_failover_channels = %#v, want %#v", mw.ModelFailoverChannels, wantChannels)
	}
	for i, want := range wantChannels {
		if mw.ModelFailoverChannels[i] != want {
			t.Fatalf("model_failover_channels[%d] = %q, want %q", i, mw.ModelFailoverChannels[i], want)
		}
	}
}
