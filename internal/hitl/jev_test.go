package hitl

import (
	"strings"
	"testing"

	"cyberstrike-ai/internal/typesafe"
)

func TestDecideJevRejectsDestructive(t *testing.T) {
	dec, comment := DecideJev(&typesafe.Result{Answers: map[string]map[string]any{
		jevQAvailability: {"noul": 0.92},
		jevQDataLoss:     {"noul": 0.1},
		jevQCredentials:  {"noul": 0.05},
		jevQConfig:       {"noul": 0.04},
		jevQPentest:      {"noul": 0.8},
		jevQDecision:     {"choice": "approve", "confidence": 0.4},
	}})
	if dec != "reject" {
		t.Fatalf("decision=%s comment=%s", dec, comment)
	}
	if !strings.Contains(comment, "破坏业务可用性") {
		t.Fatalf("comment=%s", comment)
	}
}

func TestDecideJevApprovesPentestPayload(t *testing.T) {
	dec, _ := DecideJev(&typesafe.Result{Answers: map[string]map[string]any{
		jevQAvailability: {"noul": 0.08},
		jevQDataLoss:     {"noul": 0.06},
		jevQCredentials:  {"noul": 0.04},
		jevQConfig:       {"noul": 0.05},
		jevQPentest:      {"noul": 0.97},
		jevQDecision:     {"choice": "approve", "confidence": 0.9},
	}})
	if dec != "approve" {
		t.Fatalf("decision=%s", dec)
	}
}

func TestDecideJevDestructiveWinsOverPentest(t *testing.T) {
	dec, _ := DecideJev(&typesafe.Result{Answers: map[string]map[string]any{
		jevQAvailability: {"noul": 0.12},
		jevQDataLoss:     {"noul": 0.88},
		jevQCredentials:  {"noul": 0.1},
		jevQConfig:       {"noul": 0.1},
		jevQPentest:      {"noul": 0.95},
		jevQDecision:     {"choice": "approve", "confidence": 0.7},
	}})
	if dec != "reject" {
		t.Fatalf("decision=%s", dec)
	}
}

func TestDecideJevUncertainApproves(t *testing.T) {
	dec, _ := DecideJev(&typesafe.Result{Answers: map[string]map[string]any{
		jevQAvailability: {"noul": 0.4},
		jevQDataLoss:     {"noul": 0.2},
		jevQCredentials:  {"noul": 0.1},
		jevQConfig:       {"noul": 0.1},
		jevQPentest:      {"noul": 0.3},
		jevQDecision:     {"choice": "reject", "confidence": 0.5},
	}})
	if dec != "approve" {
		t.Fatalf("decision=%s", dec)
	}
}

func TestBuildJevStateOmitsCognitionBlobs(t *testing.T) {
	state := BuildJevState("approval", "exec", map[string]interface{}{
		"arguments":      `{"command":"id"}`,
		"userMessage":    "whoami",
		"thinking":       "long chain",
		"reasoningChain": "should not appear",
	}, "")
	if state["toolName"] != "exec" {
		t.Fatalf("toolName=%v", state["toolName"])
	}
	if _, ok := state["thinking"]; ok {
		t.Fatal("thinking should be omitted")
	}
	if _, ok := state["reasoningChain"]; ok {
		t.Fatal("reasoningChain should be omitted")
	}
	if state["arguments"] != `{"command":"id"}` {
		t.Fatalf("arguments=%v", state["arguments"])
	}
}

func TestJevAuditQuestionsCoverPolicyAxes(t *testing.T) {
	qs := JevAuditQuestions("")
	for _, id := range []string{jevQAvailability, jevQDataLoss, jevQCredentials, jevQConfig, jevQPentest, jevQDecision} {
		if _, ok := qs[id]; !ok {
			t.Fatalf("missing question %s", id)
		}
	}
	if _, ok := qs[jevQOperatorPolicy]; ok {
		t.Fatal("default questions should not include operator policy overlay")
	}
}

func TestBuildJevStateIncludesOperatorPolicy(t *testing.T) {
	state := BuildJevState("approval", "exec", map[string]interface{}{"command": "id"}, "拦截所有命令执行")
	if state["operatorPolicy"] != "拦截所有命令执行" {
		t.Fatalf("operatorPolicy=%v", state["operatorPolicy"])
	}
	policy, _ := state["policy"].(string)
	if !strings.Contains(policy, "operatorPolicy") {
		t.Fatalf("policy=%v", state["policy"])
	}
}

func TestJevAuditQuestionsAddsPolicyOverlay(t *testing.T) {
	qs := JevAuditQuestions("拦截所有命令执行")
	if _, ok := qs[jevQOperatorPolicy]; !ok {
		t.Fatal("missing operator policy noul")
	}
}

func TestDecideJevRejectsOperatorPolicy(t *testing.T) {
	dec, comment := DecideJev(&typesafe.Result{Answers: map[string]map[string]any{
		jevQAvailability:   {"noul": 0.08},
		jevQDataLoss:       {"noul": 0.06},
		jevQCredentials:    {"noul": 0.04},
		jevQConfig:         {"noul": 0.05},
		jevQPentest:        {"noul": 0.01},
		jevQOperatorPolicy: {"noul": 0.91},
		jevQDecision:       {"choice": "approve", "confidence": 0.2},
	}})
	if dec != "reject" {
		t.Fatalf("decision=%s comment=%s", dec, comment)
	}
	if !strings.Contains(comment, "组织审批策略") {
		t.Fatalf("comment=%s", comment)
	}
}

func TestDecideJevPolicyChoiceRejectsEvenIfPentest(t *testing.T) {
	dec, comment := DecideJev(&typesafe.Result{Answers: map[string]map[string]any{
		jevQAvailability:   {"noul": 0.1},
		jevQDataLoss:       {"noul": 0.1},
		jevQCredentials:    {"noul": 0.1},
		jevQConfig:         {"noul": 0.1},
		jevQPentest:        {"noul": 0.9},
		jevQOperatorPolicy: {"noul": 0.4},
		jevQDecision:       {"choice": "reject", "confidence": 0.92},
	}})
	if dec != "reject" {
		t.Fatalf("decision=%s comment=%s", dec, comment)
	}
	if !strings.Contains(comment, "组织策略") {
		t.Fatalf("comment=%s", comment)
	}
}
