package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

func TestParseAuditAgentLLMContentApprove(t *testing.T) {
	d, err := parseAuditAgentLLMContent(`{"decision":"approve","comment":"与任务一致"}`)
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != "approve" || d.Comment != "与任务一致" {
		t.Fatalf("unexpected %+v", d)
	}
}

func TestParseAuditAgentLLMContentReject(t *testing.T) {
	d, err := parseAuditAgentLLMContent("```json\n{\"decision\":\"reject\",\"comment\":\"风险过高\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != "reject" {
		t.Fatalf("expected reject, got %s", d.Decision)
	}
}

func TestParseAuditAgentLLMContentInvalid(t *testing.T) {
	_, err := parseAuditAgentLLMContent(`{"decision":"maybe"}`)
	if err == nil {
		t.Fatal("expected error for invalid decision")
	}
}

func TestParseAuditAgentLLMContentProseWrapped(t *testing.T) {
	d, err := parseAuditAgentLLMContent("好的，裁决如下：\n```json\n{\"decision\":\"approve\",\"comment\":\"只读 ls\"}\n```\n以上。")
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != "approve" {
		t.Fatalf("expected approve, got %s", d.Decision)
	}
}

func TestParseAuditAgentLLMContentChineseDecision(t *testing.T) {
	d, err := parseAuditAgentLLMContent(`{"decision":"通过","comment":"风险低"}`)
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != "approve" {
		t.Fatalf("expected approve, got %s", d.Decision)
	}
}

func TestParseAuditAgentLLMContentWithEditedArguments(t *testing.T) {
	d, err := parseAuditAgentLLMContent(`{"decision":"approve","comment":"收窄路径","editedArguments":{"path":"/safe"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != "approve" {
		t.Fatalf("expected approve, got %s", d.Decision)
	}
	if d.EditedArguments == nil || d.EditedArguments["path"] != "/safe" {
		t.Fatalf("unexpected edited args: %+v", d.EditedArguments)
	}
}

func TestAuditAgentReviewTypeSafeMissingAPIKey(t *testing.T) {
	h := &AgentHandler{config: &config.Config{Hitl: config.HitlConfig{AuditBackend: "typesafe"}}}
	d := h.auditAgentReview(context.Background(), "approval", "exec", nil)
	if d.Decision != "reject" {
		t.Fatalf("decision=%s", d.Decision)
	}
	if !strings.Contains(d.Comment, "TypeSafe API Key") {
		t.Fatalf("comment=%s", d.Comment)
	}
}

func TestBuildAuditAgentReviewInputIncludesMode(t *testing.T) {
	s := buildAuditAgentReviewInput("review_edit", "execute", map[string]interface{}{
		"arguments": `{"command":"pwd"}`,
	})
	if !strings.Contains(s, "review_edit") || !strings.Contains(s, "execute") {
		t.Fatalf("unexpected input: %s", s)
	}
}

func TestBuildAuditAgentReviewInput(t *testing.T) {
	s := buildAuditAgentReviewInput("approval", "nmap", map[string]interface{}{
		"arguments":   `{"target":"10.0.0.1"}`,
		"userMessage": "扫描内网",
	})
	if s == "" {
		t.Fatal("expected non-empty input")
	}
	if !strings.Contains(s, "nmap") || !strings.Contains(s, "10.0.0.1") || !strings.Contains(s, "扫描内网") {
		t.Fatalf("unexpected input: %s", s)
	}
}

func TestAuditAgentReviewTypeSafeIncompleteAnswersReject(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "empty answers",
			body: `{"model":"jev-latest","answers":{}}`,
			want: "reject",
		},
		{
			name: "partial answers",
			body: `{"answers":{"destroys_availability":{"noul":0.02}}}`,
			want: "reject",
		},
		{
			name: "valid low risk answers",
			body: `{"answers":{"destroys_availability":{"noul":0.02},"irreversible_data_loss":{"noul":0.02},"credential_or_acl_change":{"noul":0.02},"config_tamper":{"noul":0.02},"authorized_pentest_payload":{"noul":0.9},"decision":{"choice":"approve","confidence":0.95}}}`,
			want: "approve",
		},
		{
			name: "destructive answers",
			body: `{"answers":{"destroys_availability":{"noul":0.97},"irreversible_data_loss":{"noul":0.97},"credential_or_acl_change":{"noul":0.97},"config_tamper":{"noul":0.97},"authorized_pentest_payload":{"noul":0.2},"decision":{"choice":"reject","confidence":0.97}}}`,
			want: "reject",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			h := &AgentHandler{
				config: &config.Config{Hitl: config.HitlConfig{
					AuditBackend: "typesafe",
					AuditModel:   config.OpenAIConfig{BaseURL: srv.URL, APIKey: "ts-key", Model: "jev-latest"},
				}},
				logger: zap.NewNop(),
			}
			d := h.auditAgentReview(context.Background(), "approval", "exec", map[string]interface{}{"command": "id"})
			if d.Decision != tc.want {
				t.Fatalf("decision=%s comment=%s, want %s", d.Decision, d.Comment, tc.want)
			}
		})
	}
}
