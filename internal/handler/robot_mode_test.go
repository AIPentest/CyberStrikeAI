package handler

import (
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

func TestRobotModeSwitch(t *testing.T) {
	h := NewRobotHandler(&config.Config{MultiAgent: config.MultiAgentConfig{Enabled: true}}, nil, nil, zap.NewNop())

	if got := h.cmdSwitchMode("lark", "user-1", "plan-execute"); !strings.Contains(got, "Plan-Execute") {
		t.Fatalf("unexpected switch response: %s", got)
	}
	if got := h.getAgentMode("lark", "user-1"); got != "plan_execute" {
		t.Fatalf("mode = %q, want plan_execute", got)
	}
	if got := h.cmdModes("lark", "user-1"); !strings.Contains(got, "当前模式: Plan-Execute") {
		t.Fatalf("unexpected modes response: %s", got)
	}
}

func TestRobotModeRejectsUnavailableMultiAgent(t *testing.T) {
	h := NewRobotHandler(&config.Config{}, nil, nil, zap.NewNop())

	if got := h.cmdSwitchMode("lark", "user-1", "deep"); !strings.Contains(got, "启用 Agent 多代理") {
		t.Fatalf("unexpected rejection: %s", got)
	}
	if got := h.getAgentMode("lark", "user-1"); got != "single" {
		t.Fatalf("mode changed after rejection: %q", got)
	}
}

func TestParseRobotAgentModeRejectsUnknownMode(t *testing.T) {
	if mode, ok := parseRobotAgentMode("unknown"); ok || mode != "" {
		t.Fatalf("parseRobotAgentMode returned (%q, %v), want empty,false", mode, ok)
	}
}

func TestRobotStatusCommandPermission(t *testing.T) {
	for _, command := range []string{"状态", "status"} {
		permission, recognized := robotCommandPermission(command)
		if !recognized || permission != "chat:read" {
			t.Fatalf("command %q returned permission=%q recognized=%v", command, permission, recognized)
		}
	}
	for _, removed := range []string{"当前", "current"} {
		if _, recognized := robotCommandPermission(removed); recognized {
			t.Fatalf("removed command %q is still recognized", removed)
		}
	}
}

func TestRobotBestPracticeCommandPermissions(t *testing.T) {
	cases := map[string]string{
		"任务":       "chat:read",
		"task":     "chat:read",
		"重命名 新标题":  "chat:write",
		"rename x": "chat:write",
		"诊断":       "config:read",
		"doctor":   "config:read",
	}
	for command, want := range cases {
		permission, recognized := robotCommandPermission(command)
		if !recognized || permission != want {
			t.Fatalf("command %q returned permission=%q recognized=%v, want %q,true", command, permission, recognized, want)
		}
	}
}

func TestRobotConfirmationCanBeCancelled(t *testing.T) {
	h := NewRobotHandler(&config.Config{}, nil, nil, zap.NewNop())
	h.setPendingConfirmation("lark", "user-1", "delete_conversation", "conv-1")
	if got := h.cmdCancelConfirmation("lark", "user-1"); got != "已取消待确认操作。" {
		t.Fatalf("unexpected cancel response: %s", got)
	}
	if got := h.cmdConfirm("lark", "user-1"); !strings.Contains(got, "没有待确认操作") {
		t.Fatalf("confirmation survived cancellation: %s", got)
	}
}

func TestRobotDoctorSeparatesInternalToolsFromHTTPMCP(t *testing.T) {
	h := NewRobotHandler(&config.Config{
		Security: config.SecurityConfig{Tools: []config.ToolConfig{
			{Name: "enabled-tool", Enabled: true},
			{Name: "disabled-tool", Enabled: false},
		}},
		MCP: config.MCPConfig{Enabled: false},
	}, nil, nil, zap.NewNop())

	got := h.cmdDoctor()
	if !strings.Contains(got, "内置 MCP 工具: 1/2 个已启用") {
		t.Fatalf("internal tool status missing: %s", got)
	}
	if !strings.Contains(got, "HTTP MCP 服务: 已关闭") {
		t.Fatalf("HTTP MCP status missing: %s", got)
	}
}
