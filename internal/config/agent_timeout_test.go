package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoadTaskTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		want       time.Duration
		invalid    bool
	}{
		{"absent agent", "{}", 600 * time.Minute, false},
		{"absent field", "agent: {}", 600 * time.Minute, false},
		{"custom", "agent:\n  task_timeout_minutes: 1440", 24 * time.Hour, false},
		{"unlimited", "agent:\n  task_timeout_minutes: 0", 0, false},
		{"negative", "agent:\n  task_timeout_minutes: -1", 0, true},
		{"overflow", "agent:\n  task_timeout_minutes: 153722868", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), "agent.task_timeout_minutes") {
					t.Fatalf("expected field validation error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Agent.EffectiveTaskTimeout(); got != tc.want {
				t.Fatalf("timeout = %v, want %v", got, tc.want)
			}
			// YAML saves must preserve explicit zero as distinct from omission.
			data, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			again, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if again.Agent.EffectiveTaskTimeout() != tc.want {
				t.Fatal("timeout changed after save/load")
			}
		})
	}
}
