package config

import (
	"fmt"

	"cyberstrike-ai/internal/toolguard"

	"gopkg.in/yaml.v3"
)

// EffectiveToolGuard enables the default government-domain protection for old
// configurations as well as new installs. An explicit config may disable it.
func (c *Config) EffectiveToolGuard() toolguard.Config {
	if c.ToolGuard == nil {
		return toolguard.DefaultConfig()
	}
	return *c.ToolGuard
}

// validateToolGuardYAML requires an explicit decision for both protection and
// its rules whenever a non-null section is supplied. Otherwise a typo or partial
// section could silently turn the enabled-by-default protection off. Pointer
// fields distinguish false/[] from omitted or null values, and the YAML decoder
// continues to support aliases and merged configuration mappings.
func validateToolGuardYAML(data []byte) error {
	var document struct {
		ToolGuard *struct {
			Enabled *bool             `yaml:"enabled"`
			Rules   *[]toolguard.Rule `yaml:"rules"`
		} `yaml:"tool_guard"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if section := document.ToolGuard; section != nil && (section.Enabled == nil || section.Rules == nil) {
		return fmt.Errorf("tool_guard must explicitly provide enabled and rules; provide an empty array to clear the rules")
	}
	return nil
}
