package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestLegacyRuntimeSettingsAreReadWithoutLegacyEngine(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("multi_agent:\n  eino_middleware:\n    model_retry_max_retries: 7\n  eino_skills:\n    disable: true\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.MultiAgent.Runtime.ModelRetryMaxRetries != 7 || !cfg.MultiAgent.Skills.Disable {
		t.Fatal("migration lost settings")
	}
	if err := yaml.Unmarshal([]byte("multi_agent:\n  runtime:\n    model_retry_max_retries: 2\n  eino_middleware:\n    model_retry_max_retries: 7\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.MultiAgent.Runtime.ModelRetryMaxRetries != 2 {
		t.Fatal("legacy settings overrode native settings")
	}
}
