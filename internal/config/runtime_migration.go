package config

import "gopkg.in/yaml.v3"

// Read old configuration keys as data migration only; there is no legacy runner.
// Saving this configuration emits only skills/runtime, never the retired keys.
func (m *MultiAgentConfig) UnmarshalYAML(node *yaml.Node) error {
	type current MultiAgentConfig
	var decoded current
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	var legacy struct {
		Skills  *AgentSkillsConfig `yaml:"eino_skills"`
		Runtime *AgentTuningConfig `yaml:"eino_middleware"`
	}
	if err := node.Decode(&legacy); err != nil {
		return err
	}
	hasSkills, hasRuntime := false, false
	for i := 0; i+1 < len(node.Content); i += 2 {
		switch node.Content[i].Value {
		case "skills":
			hasSkills = true
		case "runtime":
			hasRuntime = true
		}
	}
	*m = MultiAgentConfig(decoded)
	if !hasSkills && legacy.Skills != nil {
		m.Skills = *legacy.Skills
	}
	if !hasRuntime && legacy.Runtime != nil {
		m.Runtime = *legacy.Runtime
	}
	return nil
}
