package config

import "testing"

func TestAgentRuntimeIsPiOnly(t *testing.T) {
	if (AgentRuntimeConfig{}).Validate() != nil || (AgentRuntimeConfig{Engine: "pi"}).Validate() != nil {
		t.Fatal("Pi default rejected")
	}
	if (AgentRuntimeConfig{Engine: "eino"}).Validate() == nil {
		t.Fatal("retired engine accepted")
	}
}
