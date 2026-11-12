package config

import "testing"

func TestAgentRuntimeDefaultAndExplicitFallback(t *testing.T) {
	if !(AgentRuntimeConfig{}).UsesPi() {
		t.Fatal("Pi must be the default")
	}
	if (AgentRuntimeConfig{Engine: "eino"}).UsesPi() {
		t.Fatal("explicit fallback ignored")
	}
	if (AgentRuntimeConfig{Engine: "typo"}).Validate() == nil {
		t.Fatal("invalid runtime accepted")
	}
}
