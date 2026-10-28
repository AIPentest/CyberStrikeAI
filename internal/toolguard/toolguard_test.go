package toolguard

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestDefaultGovernmentProtection(t *testing.T) {
	policy, err := Compile(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		input string
		match string
	}{
		{"https://agency.gov/login", "agency.gov"},
		{"https://www.agency.gov.cn:443/login", "www.agency.gov.cn"},
		{"curl https://EXAMPLE.GOV.UK/a", "EXAMPLE.GOV.UK"},
		{"*.gov", "*.gov"},
		{"*.gov.*", "*.gov.*"},
		{".gov", ".gov"},
		{".gov.*", ".gov.*"},
		{"https://政务.gov.cn/", "政务.gov.cn"},
		{"https://gov.cn/", "gov.cn"},
		{"https://agency.gov./", "agency.gov."},
		{"https://agency%2egov/a", "agency.gov"},
		{"https://agency%252Egov/a", "agency.gov"},
		{"echo 100% && curl https://agency%2egov/a", "agency.gov"},
	} {
		t.Run(test.input, func(t *testing.T) {
			match := policy.Check("http_request", map[string]interface{}{"target": test.input})
			if match == nil || match.MatchedText != test.match {
				t.Fatalf("Check = %+v, want match %q", match, test.match)
			}
			if !strings.Contains(match.Message, test.match) || !strings.Contains(match.Message, "attacking government sites is forbidden") {
				t.Fatalf("unexpected reminder: %q", match.Message)
			}
		})
	}
	for _, input := range []string{
		"https://example.com/", "https://government.example/", "https://agency.govt/",
		"https://agency.gov-example.com/", "https://agency.gov_cn/", "governance", "gov",
		".government", ".govx",
	} {
		t.Run("allowed "+input, func(t *testing.T) {
			if match := policy.Check("http_request", map[string]interface{}{"target": input}); match != nil {
				t.Fatalf("unexpected match: %+v", match)
			}
		})
	}
}

func TestNestedArgumentsAndJSONEscapes(t *testing.T) {
	policy, _ := Compile(DefaultConfig())
	for _, args := range []map[string]interface{}{
		{"targets": []interface{}{map[string]interface{}{"target": "https://agency.gov"}}},
		{"targets": []string{"https://agency.gov"}},
		{"targets": map[string]string{"target": "https://agency.gov"}},
		{"https://agency.gov": true},
		{"payload": json.RawMessage(`{"target":"https://agency\u002egov"}`)},
		{"payload": json.RawMessage(`{"https://agency\u002egov":true}`)},
		{"bad_value": make(chan string), "target": "https://agency.gov"},
	} {
		if match := policy.Check("request", args); match == nil || match.MatchedText != "agency.gov" {
			t.Fatalf("Check(%v) = %+v", args, match)
		}
	}
}

func TestDeepNestedJSONEscapes(t *testing.T) {
	policy, _ := Compile(DefaultConfig())
	// Deep nesting must not hide a domain represented with JSON Unicode escapes.
	payload := strings.Repeat("[", 200) + `"https://agency\u002egov"` + strings.Repeat("]", 200)
	match := policy.Check("request", map[string]interface{}{"payload": json.RawMessage(payload)})
	if match == nil || match.MatchedText != "agency.gov" {
		t.Fatalf("deep JSON value was not checked: %+v", match)
	}
}

func TestRuleOrderingAndInputCoverage(t *testing.T) {
	config := Config{Enabled: true, Rules: []Rule{
		{ID: "first", Name: "First", Enabled: true, Pattern: "payload-risk", Message: "{rule}/{tool}/{match}"},
		{ID: "second", Name: "Second", Enabled: true, Pattern: "tool-risk"},
	}}
	policy, _ := Compile(config)
	match := policy.Check("tool-risk", map[string]interface{}{"value": "payload-risk"})
	if match == nil || match.RuleID != "first" || match.Message != "First/tool-risk/payload-risk" {
		t.Fatalf("rule order or reminder incorrect: %+v", match)
	}
	if match = policy.Check("tool-risk", nil); match == nil || match.RuleID != "second" || match.Message == "" {
		t.Fatalf("tool name not checked: %+v", match)
	}
	config.Rules[0].Pattern = `"port":443`
	policy, _ = Compile(config)
	if match = policy.Check("request", map[string]interface{}{"port": 443}); match == nil || match.MatchedText != `"port":443` {
		t.Fatalf("serialized arguments not checked: %+v", match)
	}
	config.Rules[0].Pattern = `^risk.+$`
	policy, _ = Compile(config)
	if match = policy.Check("request", map[string]interface{}{"z": "risk-z", "a": "risk-a"}); match == nil || match.MatchedText != "risk-a" {
		t.Fatalf("field traversal is not deterministic: %+v", match)
	}
}

func TestTemplateReplacementDoesNotExpandMatchedText(t *testing.T) {
	config := Config{Enabled: true, Rules: []Rule{{
		ID: "template", Name: "Rule", Enabled: true, Pattern: `\{tool\}`, Message: "{match}; {tool}; {rule}",
	}}}
	policy, _ := Compile(config)
	match := policy.Check("request", map[string]interface{}{"value": "{tool}"})
	if match == nil || match.Message != "{tool}; request; Rule" {
		t.Fatalf("template expansion was recursive: %+v", match)
	}
}

func TestPercentDecodingBudgetAppliesToEachInput(t *testing.T) {
	config := Config{Enabled: true, Rules: []Rule{{
		ID: "domain", Name: "Domain", Enabled: true, Pattern: `^agency\.gov$`,
	}}}
	policy, _ := Compile(config)
	// A deeply encoded value may finish its decoding budget at an intermediate
	// string, but must not prevent an independent field from decoding further.
	match := policy.Check("request", map[string]interface{}{
		"a": "agency%2525252egov", "b": "agency%2egov",
	})
	if match == nil || match.MatchedText != "agency.gov" {
		t.Fatalf("an earlier value suppressed decoding of another field: %+v", match)
	}
}

func TestDisabledSettings(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false
	policy, _ := Compile(config)
	args := map[string]interface{}{"target": "agency.gov"}
	if policy.Check("request", args) != nil {
		t.Fatal("disabled policy blocked the call")
	}
	config.Enabled = true
	config.Rules[0].Enabled = false
	policy, _ = Compile(config)
	if policy.Check("request", args) != nil {
		t.Fatal("disabled rule blocked the call")
	}
	config.Rules = []Rule{}
	policy, _ = Compile(config)
	if policy.Check("request", args) != nil {
		t.Fatal("empty policy blocked the call")
	}
}

func TestCompileValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Config)
	}{
		{"invalid regex", func(c *Config) { c.Rules[0].Pattern = "[" }},
		{"disabled invalid regex", func(c *Config) { c.Enabled = false; c.Rules[0].Enabled = false; c.Rules[0].Pattern = "[" }},
		{"unsupported lookahead", func(c *Config) { c.Rules[0].Pattern = "x(?=y)" }},
		{"empty regex", func(c *Config) { c.Rules[0].Pattern = " " }},
		{"oversized regex", func(c *Config) { c.Rules[0].Pattern = strings.Repeat("a", MaxPatternLength+1) }},
		{"empty id", func(c *Config) { c.Rules[0].ID = " " }},
		{"padded id", func(c *Config) { c.Rules[0].ID = " id" }},
		{"oversized id", func(c *Config) { c.Rules[0].ID = strings.Repeat("a", MaxIDLength+1) }},
		{"duplicate id", func(c *Config) { c.Rules = append(c.Rules, c.Rules[0]) }},
		{"empty name", func(c *Config) { c.Rules[0].Name = " " }},
		{"oversized name", func(c *Config) { c.Rules[0].Name = strings.Repeat("a", MaxNameLength+1) }},
		{"oversized message", func(c *Config) { c.Rules[0].Message = strings.Repeat("a", MaxMessageLength+1) }},
		{"too many rules", func(c *Config) { c.Rules = make([]Rule, MaxRules+1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			test.change(&config)
			if _, err := Compile(config); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestManagerUsesImmutableValidatedSnapshots(t *testing.T) {
	config := DefaultConfig()
	manager, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]interface{}{"target": "agency.gov"}
	config.Rules[0].Pattern = "safe"
	snapshot := manager.Config()
	snapshot.Rules[0].Enabled = false
	if manager.Check("request", args) == nil {
		t.Fatal("external config mutation changed the active policy")
	}
	invalid := DefaultConfig()
	invalid.Rules[0].Pattern = "["
	if err := manager.Update(invalid); err == nil || manager.Check("request", args) == nil {
		t.Fatal("invalid update did not preserve protection")
	}
	disabled := DefaultConfig()
	disabled.Enabled = false
	if err := manager.Update(disabled); err != nil || manager.Check("request", args) != nil {
		t.Fatal("valid update did not take effect")
	}
}

func TestManagerConcurrentUpdatesAndChecks(t *testing.T) {
	manager, _ := NewManager(DefaultConfig())
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 100; i++ {
				if match := manager.Check("request", map[string]interface{}{"target": "agency.gov"}); match == nil {
					t.Error("an update created an unprotected interval")
					return
				}
				config := manager.Config()
				config.Rules[0].Message = "Block {match}"
				if err := manager.Update(config); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	workers.Wait()
}
