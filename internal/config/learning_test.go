package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestLearningOptInAndHardLimits(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("learning:\n  enabled: true\n  max_jobs_per_day: 999\n  timeout_seconds: 999\n  max_input_runes: 999999\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Learning.Enabled || cfg.Learning.DailyLimit() != 200 || cfg.Learning.Timeout() != 120 || cfg.Learning.InputLimit() != 32000 {
		t.Fatalf("invalid bounds: %+v", cfg.Learning)
	}
	var defaultCfg Config
	if err := yaml.Unmarshal([]byte("version: test\n"), &defaultCfg); err != nil {
		t.Fatal(err)
	}
	if defaultCfg.Learning.Enabled || defaultCfg.Learning.DailyLimit() != 20 || defaultCfg.Learning.Timeout() != 60 || defaultCfg.Learning.InputLimit() != 12000 {
		t.Fatal("learning not opt-in or defaults changed")
	}
}
