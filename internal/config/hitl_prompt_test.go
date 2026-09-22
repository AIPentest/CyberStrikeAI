package config

import (
	"strings"
	"testing"
)

func TestDefaultHitlAuditAgentPromptIncludesPrioritizedRules(t *testing.T) {
	prompt := DefaultHitlAuditAgentPrompt()
	for _, want := range []string{
		"如果同时命中 reject 和 approve，必须 reject",
		"修改/重置任意用户或管理员密码",
		"修改/创建/删除用户、角色、权限",
		"停止、禁用、重启业务服务",
		"命中规则：...",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default approval prompt missing %q", want)
		}
	}
}

func TestDefaultHitlAuditAgentPromptReviewEditKeepsEditedArguments(t *testing.T) {
	prompt := DefaultHitlAuditAgentPromptReviewEdit()
	if !strings.Contains(prompt, `"editedArguments":{...}`) {
		t.Fatal("review-edit prompt must preserve editedArguments output")
	}
	if !strings.Contains(prompt, "命中规则：...") {
		t.Fatal("review-edit prompt must require a matched rule")
	}
}

func TestJevOperatorPolicySkipsDefaultPrompt(t *testing.T) {
	if got := (HitlConfig{}).JevOperatorPolicy("approval"); got != "" {
		t.Fatalf("empty config should not send default prompt to Jev, got %q", got)
	}
	if got := (HitlConfig{AuditAgentPrompt: DefaultHitlAuditAgentPrompt()}).JevOperatorPolicy("approval"); got != "" {
		t.Fatalf("default prompt should not be sent to Jev, got %q", got)
	}
	if got := (HitlConfig{AuditAgentPrompt: "拦截所有命令执行"}).JevOperatorPolicy("approval"); got != "拦截所有命令执行" {
		t.Fatalf("custom prompt=%q", got)
	}
}
